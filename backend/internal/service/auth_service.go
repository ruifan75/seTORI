package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/auth"
)

// SessionTTL はセッションの有効期間。
const SessionTTL = 30 * 24 * time.Hour

var (
	ErrInvalidCredentials = errors.New("ユーザー名またはパスワードが正しくありません")
	ErrUserInactive       = errors.New("このアカウントは無効化されています")
	ErrUsernameTaken      = errors.New("そのユーザー名は既に使われています")
	ErrRoleNameTaken      = errors.New("そのロール名は既に使われています")
	ErrUserNotFound       = errors.New("ユーザーが見つかりません")
	ErrRoleNotFound       = errors.New("ロールが見つかりません")
	ErrRoleInUse          = errors.New("このロールを使用しているユーザーがいるため削除できません")
	ErrSystemRole         = errors.New("組み込みロールは削除できません")
	ErrWeakPassword       = errors.New("パスワードは4文字以上にしてください")
)

// AuthService は認証・ユーザー/ロール管理を担う。
type AuthService struct {
	repo *repository.AuthRepository
}

func NewAuthService(repo *repository.AuthRepository) *AuthService {
	return &AuthService{repo: repo}
}

// ========== 認証 ==========

// Login は資格情報を検証し、新しいセッショントークン（生トークン）と user を返す。
func (s *AuthService) Login(username, password string) (string, *models.User, error) {
	user, err := s.repo.FindUserByUsername(strings.TrimSpace(username))
	if err != nil {
		return "", nil, err
	}
	if user == nil {
		return "", nil, ErrInvalidCredentials
	}
	// 外部アカウントのみで登録した利用者はパスワードを持たない。
	// ハッシュ不正として 500 にせず、通常の認証失敗として扱う。
	if user.PasswordHash == "" {
		return "", nil, ErrInvalidCredentials
	}
	ok, err := auth.VerifyPassword(password, user.PasswordHash)
	if err != nil {
		return "", nil, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return "", nil, ErrInvalidCredentials
	}
	if !user.IsActive {
		return "", nil, ErrUserInactive
	}

	token, err := auth.GenerateToken()
	if err != nil {
		return "", nil, err
	}
	if err := s.repo.CreateSession(auth.HashToken(token), user.ID, time.Now().Add(SessionTTL)); err != nil {
		return "", nil, err
	}
	if err := s.repo.TouchLastLogin(user.ID); err != nil {
		logger.Warnf("failed to update last_login for %s: %v", user.Username, err)
	}
	user.PasswordHash = ""
	return token, user, nil
}

// Logout は生トークンに対応するセッションを削除する。
func (s *AuthService) Logout(token string) error {
	if token == "" {
		return nil
	}
	return s.repo.DeleteSession(auth.HashToken(token))
}

// Authenticate は生トークンから現在のユーザーを解決する。無効・期限切れなら nil を返す。
func (s *AuthService) Authenticate(token string) (*models.User, error) {
	if token == "" {
		return nil, nil
	}
	user, err := s.repo.FindUserBySessionToken(auth.HashToken(token))
	if err != nil {
		return nil, err
	}
	if user != nil {
		user.PasswordHash = ""
	}
	return user, nil
}

// ========== bootstrap ==========

// EnsureBootstrapAdmin はユーザーが 0 件のとき、admin ロールで初期管理者を作成する。
func (s *AuthService) EnsureBootstrapAdmin(username, password string) error {
	n, err := s.repo.CountUsers()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	adminRole, err := s.repo.FindRoleByName("admin")
	if err != nil {
		return err
	}
	if adminRole == nil {
		return errors.New("admin ロールが見つかりません（マイグレーション未適用の可能性）")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := s.repo.CreateUser(username, "管理者", hash, adminRole.ID, true); err != nil {
		return err
	}
	logger.Warnf("初期管理者アカウントを作成しました: username=%q（早めにパスワードを変更してください）", username)
	return nil
}

// ========== ユーザー管理 ==========

func (s *AuthService) ListUsers() ([]models.User, error) {
	return s.repo.ListUsers()
}

func (s *AuthService) CreateUser(username, displayName, password string, roleID uuid.UUID, isActive bool) (*models.User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, errors.New("ユーザー名は必須です")
	}
	if len(password) < 4 {
		return nil, ErrWeakPassword
	}
	role, err := s.repo.FindRoleByID(roleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, ErrRoleNotFound
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	user, err := s.repo.CreateUser(username, displayName, hash, roleID, isActive)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	user.PasswordHash = ""
	return user, nil
}

// UpdateUser は表示名・ロール・有効状態を更新する。無効化時はセッションを失効させる。
func (s *AuthService) UpdateUser(id uuid.UUID, displayName string, roleID uuid.UUID, isActive bool) (*models.User, error) {
	existing, err := s.repo.FindUserByID(id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrUserNotFound
	}
	role, err := s.repo.FindRoleByID(roleID)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, ErrRoleNotFound
	}
	if err := s.repo.UpdateUser(id, displayName, roleID, isActive); err != nil {
		return nil, err
	}
	// 無効化・ロール変更時は既存セッションを失効させ、新しい権限を強制する。
	if !isActive || existing.RoleID != roleID {
		if err := s.repo.DeleteSessionsByUser(id); err != nil {
			logger.Warnf("failed to revoke sessions for user %s: %v", id, err)
		}
	}
	updated, err := s.repo.FindUserByID(id)
	if err != nil {
		return nil, err
	}
	if updated != nil {
		updated.PasswordHash = ""
	}
	return updated, nil
}

// ChangePassword はパスワードを変更し、そのユーザーの全セッションを失効させる。
func (s *AuthService) ChangePassword(id uuid.UUID, newPassword string) error {
	if len(newPassword) < 4 {
		return ErrWeakPassword
	}
	existing, err := s.repo.FindUserByID(id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrUserNotFound
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateUserPassword(id, hash); err != nil {
		return err
	}
	if err := s.repo.DeleteSessionsByUser(id); err != nil {
		logger.Warnf("failed to revoke sessions after password change for %s: %v", id, err)
	}
	return nil
}

func (s *AuthService) DeleteUser(id uuid.UUID) error {
	existing, err := s.repo.FindUserByID(id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrUserNotFound
	}
	return s.repo.DeleteUser(id)
}

// ========== ロール管理 ==========

func (s *AuthService) ListRoles() ([]models.Role, error) {
	return s.repo.ListRoles()
}

func (s *AuthService) CreateRole(name, description string, permissions []string) (*models.Role, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("ロール名は必須です")
	}
	role, err := s.repo.CreateRole(name, description, sanitizePermissions(permissions))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrRoleNameTaken
		}
		return nil, err
	}
	return role, nil
}

func (s *AuthService) UpdateRole(id uuid.UUID, description string, permissions []string) (*models.Role, error) {
	existing, err := s.repo.FindRoleByID(id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrRoleNotFound
	}
	if err := s.repo.UpdateRole(id, description, sanitizePermissions(permissions)); err != nil {
		return nil, err
	}
	return s.repo.FindRoleByID(id)
}

func (s *AuthService) DeleteRole(id uuid.UUID) error {
	existing, err := s.repo.FindRoleByID(id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrRoleNotFound
	}
	if existing.IsSystem {
		return ErrSystemRole
	}
	count, err := s.repo.CountUsersByRole(id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrRoleInUse
	}
	return s.repo.DeleteRole(id)
}

// PurgeExpiredSessions は期限切れセッションを掃除する（定期実行用）。
func (s *AuthService) PurgeExpiredSessions() {
	if n, err := s.repo.DeleteExpiredSessions(); err != nil {
		logger.Warnf("failed to purge expired sessions: %v", err)
	} else if n > 0 {
		logger.Infof("purged %d expired sessions", n)
	}
}

// ========== helpers ==========

// sanitizePermissions は空白除去・重複排除する。既知キー以外も許容（将来の拡張のため）。
func sanitizePermissions(perms []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func isUniqueViolation(err error) bool {
	var pgErr *pq.Error
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	// ラップされた文字列にコードが含まれる場合のフォールバック
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key")
}
