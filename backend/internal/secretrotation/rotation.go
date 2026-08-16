// Package secretrotation provides the explicit, transactional operation used
// to re-encrypt database secrets with a new SETTINGS_ENCRYPTION_KEY.
package secretrotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ruifan75/setori/pkg/secrets"
)

const secretRotationLockID int64 = 0x7365544f5249

var integrationSecretFields = []string{
	"holodex_api_key",
	"holodex_editor_token",
	"youtube_api_key",
	"groq_api_key",
	"google_drive_secret",
	"google_signin_secret",
	"ytdlp_cookies",
}

// Result reports only counts. Secret values are never returned or logged.
type Result struct {
	IntegrationSecrets int
	GDriveTokens       int
	AIProviderKeys     int
}

// Rotate validates every stored secret with oldCipher and re-encrypts it with
// newCipher. All writes happen in one transaction; any error rolls everything
// back. When apply is false, the same validation runs but no data is changed.
func Rotate(ctx context.Context, db *sql.DB, oldCipher, newCipher *secrets.Cipher, apply bool) (Result, error) {
	if oldCipher == nil || !oldCipher.Enabled() {
		return Result{}, errors.New("old SETTINGS_ENCRYPTION_KEY is missing")
	}
	if newCipher == nil || !newCipher.Enabled() {
		return Result{}, errors.New("new SETTINGS_ENCRYPTION_KEY is missing")
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Result{}, fmt.Errorf("begin secret rotation: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, secretRotationLockID); err != nil {
		return Result{}, fmt.Errorf("lock secret rotation: %w", err)
	}
	if apply {
		if _, err := tx.ExecContext(ctx, `LOCK TABLE app_settings, ai_providers IN ACCESS EXCLUSIVE MODE`); err != nil {
			return Result{}, fmt.Errorf("lock secret tables: %w", err)
		}
	}

	var result Result
	result.IntegrationSecrets, err = rotateAppSetting(ctx, tx, "integrations", integrationSecretFields, oldCipher, newCipher, apply)
	if err != nil {
		return Result{}, err
	}
	result.GDriveTokens, err = rotateAppSetting(ctx, tx, "gdrive_token", []string{"refresh_token"}, oldCipher, newCipher, apply)
	if err != nil {
		return Result{}, err
	}
	result.AIProviderKeys, err = rotateAIProviderKeys(ctx, tx, oldCipher, newCipher, apply)
	if err != nil {
		return Result{}, err
	}

	if !apply {
		return result, nil
	}
	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit secret rotation: %w", err)
	}
	return result, nil
}

func rotateAppSetting(
	ctx context.Context,
	tx *sql.Tx,
	key string,
	fields []string,
	oldCipher, newCipher *secrets.Cipher,
	apply bool,
) (int, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = $1 FOR UPDATE`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read app_settings %s: %w", key, err)
	}

	rotated, count, err := rotateJSONSecretFields(raw, fields, oldCipher, newCipher)
	if err != nil {
		return 0, fmt.Errorf("rotate app_settings %s: %w", key, err)
	}
	if apply && count > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE app_settings SET value = $2, updated_at = NOW() WHERE key = $1`, key, rotated); err != nil {
			return 0, fmt.Errorf("store app_settings %s: %w", key, err)
		}
	}
	return count, nil
}

func rotateJSONSecretFields(raw []byte, fields []string, oldCipher, newCipher *secrets.Cipher) ([]byte, int, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, 0, fmt.Errorf("decode JSON: %w", err)
	}

	count := 0
	for _, field := range fields {
		encoded, ok := value[field]
		if !ok || string(encoded) == "null" {
			continue
		}
		var stored string
		if err := json.Unmarshal(encoded, &stored); err != nil {
			return nil, 0, fmt.Errorf("decode field %s: %w", field, err)
		}
		if stored == "" {
			continue
		}
		rotated, err := rotateStoredValue(stored, oldCipher, newCipher)
		if err != nil {
			return nil, 0, fmt.Errorf("field %s: %w", field, err)
		}
		value[field], err = json.Marshal(rotated)
		if err != nil {
			return nil, 0, fmt.Errorf("encode field %s: %w", field, err)
		}
		count++
	}

	rotated, err := json.Marshal(value)
	if err != nil {
		return nil, 0, fmt.Errorf("encode JSON: %w", err)
	}
	return rotated, count, nil
}

func rotateAIProviderKeys(ctx context.Context, tx *sql.Tx, oldCipher, newCipher *secrets.Cipher, apply bool) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, api_key FROM ai_providers WHERE api_key <> '' ORDER BY id FOR UPDATE`)
	if err != nil {
		return 0, fmt.Errorf("read AI provider keys: %w", err)
	}
	type pendingUpdate struct {
		id  int
		key string
	}
	var pending []pendingUpdate
	for rows.Next() {
		var id int
		var stored string
		if err := rows.Scan(&id, &stored); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan AI provider key: %w", err)
		}
		rotated, err := rotateStoredValue(stored, oldCipher, newCipher)
		if err != nil {
			rows.Close()
			return 0, fmt.Errorf("AI provider %d: %w", id, err)
		}
		pending = append(pending, pendingUpdate{id: id, key: rotated})
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close AI provider rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate AI provider keys: %w", err)
	}

	if apply {
		for _, item := range pending {
			if _, err := tx.ExecContext(ctx, `UPDATE ai_providers SET api_key = $2, updated_at = NOW() WHERE id = $1`, item.id, item.key); err != nil {
				return 0, fmt.Errorf("store AI provider %d: %w", item.id, err)
			}
		}
	}
	return len(pending), nil
}

func rotateStoredValue(stored string, oldCipher, newCipher *secrets.Cipher) (string, error) {
	plain, err := oldCipher.Decrypt(stored)
	if err != nil {
		return "", fmt.Errorf("decrypt with old key: %w", err)
	}
	rotated, err := newCipher.Encrypt(plain)
	if err != nil {
		return "", fmt.Errorf("encrypt with new key: %w", err)
	}
	return rotated, nil
}
