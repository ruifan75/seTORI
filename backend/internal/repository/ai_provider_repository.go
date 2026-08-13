package repository

import (
	"database/sql"
	"fmt"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/pkg/secrets"
)

// AIProviderRepository は AI プロバイダーの設定を扱う。
//
// **api_key は暗号化して保存する。** 理由は pkg/secrets が書いている通りで、
// バックアップは pg_dump で DB 全体を吸い出して Google Drive へ送るので、
// 平文で置くとバックアップ 1 つの流出が全 API キーの流出になる。
// app_settings 側は最初からそうしていたが、こちらは接続を忘れていて、
// 2026-08-13 に誤って commit した dump から 3 社分のキーが平文で読めた。
//
// 鍵（SETTINGS_ENCRYPTION_KEY）が無い環境では平文のまま保存する。
// 手元での開発を止めないため ── 目印（enc:v1:）で区別できるので混在してよい。
type AIProviderRepository struct {
	db     *sql.DB
	cipher *secrets.Cipher
}

func NewAIProviderRepository(db *sql.DB, cipher *secrets.Cipher) *AIProviderRepository {
	return &AIProviderRepository{db: db, cipher: cipher}
}

// encryptKey は保存前に暗号化する。鍵が無ければ平文のまま返す。
func (r *AIProviderRepository) encryptKey(plain string) string {
	if r.cipher == nil || !r.cipher.Enabled() || plain == "" {
		return plain
	}
	enc, err := r.cipher.Encrypt(plain)
	if err != nil {
		logger.Warnf("encrypt ai provider key failed: %v", err)
		return plain
	}
	return enc
}

// decryptKey は読み出し後に復号する。目印の無い値は平文（移行前）としてそのまま返る。
func (r *AIProviderRepository) decryptKey(stored string) string {
	if r.cipher == nil {
		return stored
	}
	plain, err := r.cipher.Decrypt(stored)
	if err != nil {
		// 鍵を失った / 変えた場合。空を返して「使えない」ことをはっきりさせる
		// （平文として送ると認証エラーの山になり、原因が分かりにくい）。
		logger.Warnf("decrypt ai provider key failed: %v", err)
		return ""
	}
	return plain
}

const aiProviderColumns = "id, name, base_url, model, api_key, enabled, priority, timeout_seconds, created_at, updated_at"

func scanAIProvider(rows *sql.Rows) (models.AIProvider, error) {
	var p models.AIProvider
	err := rows.Scan(&p.ID, &p.Name, &p.BaseURL, &p.Model, &p.APIKey, &p.Enabled, &p.Priority, &p.TimeoutSeconds, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// FindAll 取得所有 provider（依優先序）
func (r *AIProviderRepository) FindAll() ([]models.AIProvider, error) {
	rows, err := r.db.Query("SELECT " + aiProviderColumns + " FROM ai_providers ORDER BY priority ASC, id ASC")
	if err != nil {
		return nil, fmt.Errorf("query ai providers: %w", err)
	}
	defer rows.Close()

	var providers []models.AIProvider
	for rows.Next() {
		p, err := scanAIProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ai provider: %w", err)
		}
		p.APIKey = r.decryptKey(p.APIKey)
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// FindEnabled 取得啟用中的 provider（依優先序）
func (r *AIProviderRepository) FindEnabled() ([]models.AIProvider, error) {
	rows, err := r.db.Query("SELECT " + aiProviderColumns + " FROM ai_providers WHERE enabled = TRUE ORDER BY priority ASC, id ASC")
	if err != nil {
		return nil, fmt.Errorf("query enabled ai providers: %w", err)
	}
	defer rows.Close()

	var providers []models.AIProvider
	for rows.Next() {
		p, err := scanAIProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ai provider: %w", err)
		}
		p.APIKey = r.decryptKey(p.APIKey)
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// FindByID 取得單一 provider
func (r *AIProviderRepository) FindByID(id int) (*models.AIProvider, error) {
	var p models.AIProvider
	err := r.db.QueryRow("SELECT "+aiProviderColumns+" FROM ai_providers WHERE id = $1", id).
		Scan(&p.ID, &p.Name, &p.BaseURL, &p.Model, &p.APIKey, &p.Enabled, &p.Priority, &p.TimeoutSeconds, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find ai provider: %w", err)
	}
	p.APIKey = r.decryptKey(p.APIKey)
	return &p, nil
}

// Create 建立 provider
func (r *AIProviderRepository) Create(p *models.AIProvider) error {
	query := `
		INSERT INTO ai_providers (name, base_url, model, api_key, enabled, priority, timeout_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`
	if p.TimeoutSeconds <= 0 {
		p.TimeoutSeconds = 60
	}
	// 保存するのは暗号文。呼び出し側が持つ p.APIKey は平文のままにしておく
	// （保存後に画面へ返す値が暗号文になると、次の保存で二重に暗号化される）。
	err := r.db.QueryRow(query, p.Name, p.BaseURL, p.Model, r.encryptKey(p.APIKey), p.Enabled, p.Priority, p.TimeoutSeconds).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create ai provider: %w", err)
	}
	return nil
}

// Update 更新 provider
func (r *AIProviderRepository) Update(p *models.AIProvider) error {
	query := `
		UPDATE ai_providers
		SET name = $2, base_url = $3, model = $4, api_key = $5, enabled = $6, priority = $7, timeout_seconds = $8, updated_at = NOW()
		WHERE id = $1`
	if p.TimeoutSeconds <= 0 {
		p.TimeoutSeconds = 60
	}
	_, err := r.db.Exec(query, p.ID, p.Name, p.BaseURL, p.Model, r.encryptKey(p.APIKey), p.Enabled, p.Priority, p.TimeoutSeconds)
	if err != nil {
		return fmt.Errorf("update ai provider: %w", err)
	}
	return nil
}

// Delete 刪除 provider
func (r *AIProviderRepository) Delete(id int) error {
	_, err := r.db.Exec("DELETE FROM ai_providers WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete ai provider: %w", err)
	}
	return nil
}

// EncryptPlaintextKeys は平文で保存されている api_key を暗号化し直す。
// 起動時に一度だけ呼ぶ。目印（enc:v1:）の無い行だけが対象なので、何度実行してもよい。
//
// migration ではなく Go 側でやるのは、暗号化に鍵（環境変数）が要るため
// ── SQL だけでは書き換えられない。
func (r *AIProviderRepository) EncryptPlaintextKeys() (int, error) {
	if r.cipher == nil || !r.cipher.Enabled() {
		return 0, nil
	}
	rows, err := r.db.Query(`SELECT id, api_key FROM ai_providers WHERE api_key <> '' AND api_key NOT LIKE 'enc:v1:%'`)
	if err != nil {
		return 0, fmt.Errorf("list plaintext ai keys: %w", err)
	}
	type row struct {
		id  int
		key string
	}
	var pending []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.key); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan plaintext ai key: %w", err)
		}
		pending = append(pending, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	n := 0
	for _, x := range pending {
		enc, err := r.cipher.Encrypt(x.key)
		if err != nil {
			return n, fmt.Errorf("encrypt ai key (%d): %w", x.id, err)
		}
		if _, err := r.db.Exec(`UPDATE ai_providers SET api_key = $2 WHERE id = $1`, x.id, enc); err != nil {
			return n, fmt.Errorf("store encrypted ai key (%d): %w", x.id, err)
		}
		n++
	}
	return n, nil
}
