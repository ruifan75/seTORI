package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
	"github.com/lib/pq"
)

// AuthRepository は users / roles / sessions を扱う。
type AuthRepository struct {
	db *sql.DB
}

func NewAuthRepository(db *sql.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

// ========== users ==========

// userSelectWithRole は role を JOIN して権限も取得する SELECT。
// password_hash は外部アカウントのみの利用者では NULL になるため空文字へ寄せる
// （空文字は VerifyPassword で必ず失敗するので、パスワード認証は自然に拒否される）。
const userSelectWithRole = `
	SELECT u.id, u.username, u.display_name, u.email, u.email_verified,
	       COALESCE(u.password_hash, ''), u.role_id, u.is_active,
	       u.last_login, u.created_at, u.updated_at, r.name, r.permissions
	FROM users u
	JOIN roles r ON r.id = u.role_id`

func scanUserWithRole(row interface{ Scan(...any) error }) (*models.User, error) {
	var u models.User
	var perms pq.StringArray
	var lastLogin sql.NullTime
	var email sql.NullString
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &email, &u.EmailVerified,
		&u.PasswordHash, &u.RoleID,
		&u.IsActive, &lastLogin, &u.CreatedAt, &u.UpdatedAt, &u.RoleName, &perms)
	if err != nil {
		return nil, err
	}
	if email.Valid {
		u.Email = &email.String
	}
	if lastLogin.Valid {
		u.LastLogin = &lastLogin.Time
	}
	u.Permissions = []string(perms)
	return &u, nil
}

// FindUserByUsername はユーザー名で（有効・無効を問わず）取得する。見つからなければ nil。
func (r *AuthRepository) FindUserByUsername(username string) (*models.User, error) {
	u, err := scanUserWithRole(r.db.QueryRow(userSelectWithRole+" WHERE u.username = $1", username))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user by username: %w", err)
	}
	return u, nil
}

// FindUserByID は ID で取得する。見つからなければ nil。
func (r *AuthRepository) FindUserByID(id uuid.UUID) (*models.User, error) {
	u, err := scanUserWithRole(r.db.QueryRow(userSelectWithRole+" WHERE u.id = $1", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user by id: %w", err)
	}
	return u, nil
}

// FindUserBySessionToken は有効なセッション（未期限切れ）から user を引く。role/permissions 込み。
// セッションが無い・期限切れ・ユーザー無効の場合は nil を返す。
func (r *AuthRepository) FindUserBySessionToken(tokenHash string) (*models.User, error) {
	query := userSelectWithRole + `
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token_hash = $1 AND s.expires_at > NOW() AND u.is_active = TRUE`
	u, err := scanUserWithRole(r.db.QueryRow(query, tokenHash))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user by session: %w", err)
	}
	return u, nil
}

// ListUsers は全ユーザーを role 名込みで返す（password_hash は空にして返す）。
func (r *AuthRepository) ListUsers() ([]models.User, error) {
	rows, err := r.db.Query(userSelectWithRole + " ORDER BY u.created_at ASC")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		u, err := scanUserWithRole(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		u.PasswordHash = ""
		users = append(users, *u)
	}
	return users, rows.Err()
}

// CountUsers はユーザー総数（bootstrap 判定用）。
func (r *AuthRepository) CountUsers() (int, error) {
	var n int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CreateUser はユーザーを作成する。ID/タイムスタンプは DB 側で採番。
func (r *AuthRepository) CreateUser(username, displayName, passwordHash string, roleID uuid.UUID, isActive bool) (*models.User, error) {
	var id uuid.UUID
	err := r.db.QueryRow(
		`INSERT INTO users (username, display_name, password_hash, role_id, is_active)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		username, displayName, passwordHash, roleID, isActive,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return r.FindUserByID(id)
}

// FindUserByEmail はメールアドレス（大文字小文字を無視）でユーザーを引く。見つからなければ nil。
func (r *AuthRepository) FindUserByEmail(email string) (*models.User, error) {
	u, err := scanUserWithRole(r.db.QueryRow(userSelectWithRole+" WHERE LOWER(u.email) = LOWER($1)", email))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user by email: %w", err)
	}
	return u, nil
}

// CreateOAuthUser は外部アカウント連携で作るユーザー。パスワードは持たない（NULL）。
// email_verified はプロバイダー側の確認状態をそのまま引き継ぐ。
func (r *AuthRepository) CreateOAuthUser(username, displayName, email string, emailVerified bool, roleID uuid.UUID) (*models.User, error) {
	var id uuid.UUID
	var emailArg any
	if email != "" {
		emailArg = email
	}
	err := r.db.QueryRow(
		`INSERT INTO users (username, display_name, password_hash, email, email_verified, role_id, is_active)
		 VALUES ($1,$2,NULL,$3,$4,$5,TRUE) RETURNING id`,
		username, displayName, emailArg, emailVerified, roleID,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create oauth user: %w", err)
	}
	return r.FindUserByID(id)
}

// UsernameExists は（大文字小文字を無視して）ユーザー名の重複を確認する。
// 外部アカウント由来の名前から一意なユーザー名を作るのに使う。
func (r *AuthRepository) UsernameExists(username string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM users WHERE LOWER(username) = LOWER($1))", username).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check username exists: %w", err)
	}
	return exists, nil
}

// UpdateUser は表示名・ロール・有効状態を更新する。
func (r *AuthRepository) UpdateUser(id uuid.UUID, displayName string, roleID uuid.UUID, isActive bool) error {
	_, err := r.db.Exec(
		`UPDATE users SET display_name=$2, role_id=$3, is_active=$4, updated_at=NOW() WHERE id=$1`,
		id, displayName, roleID, isActive,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

// UpdateUserPassword はパスワードハッシュを更新する。
func (r *AuthRepository) UpdateUserPassword(id uuid.UUID, passwordHash string) error {
	_, err := r.db.Exec(`UPDATE users SET password_hash=$2, updated_at=NOW() WHERE id=$1`, id, passwordHash)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	return nil
}

// TouchLastLogin は最終ログイン時刻を更新する。
func (r *AuthRepository) TouchLastLogin(id uuid.UUID) error {
	_, err := r.db.Exec(`UPDATE users SET last_login=NOW() WHERE id=$1`, id)
	return err
}

// DeleteUser はユーザーを削除する（セッションは ON DELETE CASCADE）。
func (r *AuthRepository) DeleteUser(id uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM users WHERE id=$1", id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// ========== roles ==========

const roleColumns = "id, name, description, permissions, is_system, created_at, updated_at"

func scanRole(row interface{ Scan(...any) error }) (*models.Role, error) {
	var role models.Role
	err := row.Scan(&role.ID, &role.Name, &role.Description, &role.Permissions, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &role, nil
}

// ListRoles は全ロールを返す。
func (r *AuthRepository) ListRoles() ([]models.Role, error) {
	rows, err := r.db.Query("SELECT " + roleColumns + " FROM roles ORDER BY is_system DESC, name ASC")
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var roles []models.Role
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, *role)
	}
	return roles, rows.Err()
}

// FindRoleByID はロールを取得する。見つからなければ nil。
func (r *AuthRepository) FindRoleByID(id uuid.UUID) (*models.Role, error) {
	role, err := scanRole(r.db.QueryRow("SELECT "+roleColumns+" FROM roles WHERE id=$1", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find role: %w", err)
	}
	return role, nil
}

// FindRoleByName は名前でロールを取得する。見つからなければ nil。
func (r *AuthRepository) FindRoleByName(name string) (*models.Role, error) {
	role, err := scanRole(r.db.QueryRow("SELECT "+roleColumns+" FROM roles WHERE name=$1", name))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find role by name: %w", err)
	}
	return role, nil
}

// CreateRole はロールを作成する。
func (r *AuthRepository) CreateRole(name, description string, permissions []string) (*models.Role, error) {
	var id uuid.UUID
	err := r.db.QueryRow(
		`INSERT INTO roles (name, description, permissions, is_system)
		 VALUES ($1,$2,$3,FALSE) RETURNING id`,
		name, description, pq.Array(permissions),
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}
	return r.FindRoleByID(id)
}

// UpdateRole は説明と権限を更新する（is_system ロールは名前変更不可のため name は更新しない）。
func (r *AuthRepository) UpdateRole(id uuid.UUID, description string, permissions []string) error {
	_, err := r.db.Exec(
		`UPDATE roles SET description=$2, permissions=$3, updated_at=NOW() WHERE id=$1`,
		id, description, pq.Array(permissions),
	)
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	return nil
}

// DeleteRole はロールを削除する。使用中（FK）や is_system の場合はエラー。
func (r *AuthRepository) DeleteRole(id uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM roles WHERE id=$1 AND is_system=FALSE", id)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	return nil
}

// CountUsersByRole は指定ロールを使うユーザー数を返す（削除可否判定用）。
func (r *AuthRepository) CountUsersByRole(roleID uuid.UUID) (int, error) {
	var n int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM users WHERE role_id=$1", roleID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users by role: %w", err)
	}
	return n, nil
}

// ========== sessions ==========

// CreateSession はセッションを作成する。
func (r *AuthRepository) CreateSession(tokenHash string, userID uuid.UUID, expiresAt time.Time) error {
	_, err := r.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
		tokenHash, userID, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// DeleteSession は単一セッション（ログアウト）を削除する。
func (r *AuthRepository) DeleteSession(tokenHash string) error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE token_hash=$1", tokenHash)
	return err
}

// DeleteSessionsByUser はユーザーの全セッションを失効させる（パスワード変更・無効化時）。
func (r *AuthRepository) DeleteSessionsByUser(userID uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE user_id=$1", userID)
	return err
}

// DeleteExpiredSessions は期限切れセッションを掃除する。
func (r *AuthRepository) DeleteExpiredSessions() (int64, error) {
	res, err := r.db.Exec("DELETE FROM sessions WHERE expires_at <= NOW()")
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
