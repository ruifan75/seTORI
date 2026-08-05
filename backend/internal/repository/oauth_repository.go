package repository

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
)

// OAuthRepository は外部アカウント連携（oauth_identities）を扱う。
// provider は値として持つため、Google 以外を足してもこの層は変更不要。
type OAuthRepository struct {
	db *sql.DB
}

func NewOAuthRepository(db *sql.DB) *OAuthRepository {
	return &OAuthRepository{db: db}
}

const oauthIdentitySelect = `
	SELECT id, user_id, provider, provider_user_id, email, display_name, avatar_url, created_at, updated_at
	FROM oauth_identities`

func scanOAuthIdentity(row interface{ Scan(...any) error }) (*models.OAuthIdentity, error) {
	var oi models.OAuthIdentity
	var email, displayName, avatarURL sql.NullString
	err := row.Scan(&oi.ID, &oi.UserID, &oi.Provider, &oi.ProviderUserID,
		&email, &displayName, &avatarURL, &oi.CreatedAt, &oi.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if email.Valid {
		oi.Email = &email.String
	}
	if displayName.Valid {
		oi.DisplayName = &displayName.String
	}
	if avatarURL.Valid {
		oi.AvatarURL = &avatarURL.String
	}
	return &oi, nil
}

// FindByProviderUserID は外部アカウントの一意 ID から連携を引く。無ければ nil。
// ログイン時に「この Google アカウントは既に誰かに紐付いているか」を判定するのに使う。
func (r *OAuthRepository) FindByProviderUserID(provider, providerUserID string) (*models.OAuthIdentity, error) {
	oi, err := scanOAuthIdentity(r.db.QueryRow(
		oauthIdentitySelect+" WHERE provider = $1 AND provider_user_id = $2", provider, providerUserID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find oauth identity: %w", err)
	}
	return oi, nil
}

// FindByUserID は利用者に紐付く全ての外部アカウントを返す（設定画面の表示用）。
func (r *OAuthRepository) FindByUserID(userID uuid.UUID) ([]models.OAuthIdentity, error) {
	rows, err := r.db.Query(oauthIdentitySelect+" WHERE user_id = $1 ORDER BY created_at", userID)
	if err != nil {
		return nil, fmt.Errorf("find oauth identities by user: %w", err)
	}
	defer rows.Close()

	var result []models.OAuthIdentity
	for rows.Next() {
		oi, err := scanOAuthIdentity(rows)
		if err != nil {
			return nil, fmt.Errorf("scan oauth identity: %w", err)
		}
		result = append(result, *oi)
	}
	return result, rows.Err()
}

// Link は外部アカウントを利用者へ紐付ける。同じ (provider, provider_user_id) が
// 既にある場合はプロフィール情報だけ更新する（改名・アバター変更に追随）。
func (r *OAuthRepository) Link(oi *models.OAuthIdentity) error {
	query := `
		INSERT INTO oauth_identities (user_id, provider, provider_user_id, email, display_name, avatar_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (provider, provider_user_id)
		DO UPDATE SET email = EXCLUDED.email, display_name = EXCLUDED.display_name,
		              avatar_url = EXCLUDED.avatar_url, updated_at = NOW()
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(query, oi.UserID, oi.Provider, oi.ProviderUserID,
		oi.Email, oi.DisplayName, oi.AvatarURL).Scan(&oi.ID, &oi.CreatedAt, &oi.UpdatedAt)
	if err != nil {
		return fmt.Errorf("link oauth identity: %w", err)
	}
	return nil
}

// Unlink は連携を解除する。解除後にログイン手段が無くなる利用者を作らないよう、
// 呼び出し側（service）でパスワードや他プロバイダーの有無を必ず確認すること。
func (r *OAuthRepository) Unlink(userID uuid.UUID, provider string) error {
	res, err := r.db.Exec("DELETE FROM oauth_identities WHERE user_id = $1 AND provider = $2", userID, provider)
	if err != nil {
		return fmt.Errorf("unlink oauth identity: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("unlink oauth identity: not linked")
	}
	return nil
}

// CountByUserID は利用者に紐付く外部アカウント数を返す（解除可否の判定用）。
func (r *OAuthRepository) CountByUserID(userID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow("SELECT count(*) FROM oauth_identities WHERE user_id = $1", userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count oauth identities: %w", err)
	}
	return n, nil
}
