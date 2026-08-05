package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/auth"
	"github.com/ruifan75/setori/pkg/oauth"
)

var (
	ErrProviderNotFound   = fmt.Errorf("対応していない連携先です")
	ErrProviderNotEnabled = fmt.Errorf("この連携先は設定されていません")
	ErrOAuthState         = fmt.Errorf("認証の状態が確認できませんでした。もう一度お試しください")
	ErrOAuthCodeExpired   = fmt.Errorf("ログインの引き換えコードが無効です")
	ErrIdentityTaken      = fmt.Errorf("この外部アカウントは既に別の利用者に紐付いています")
	ErrEmailTaken         = fmt.Errorf("このメールアドレスの利用者が既にいます。先にログインしてから設定画面で連携してください")
	ErrLastLoginMethod    = fmt.Errorf("最後のログイン手段は解除できません")
	ErrAlreadyLinked      = fmt.Errorf("この連携先は既に紐付け済みです")
)

// state / 引き換えコードの有効期間。どちらも1往復のためだけの短命な値。
const (
	oauthStateTTL    = 10 * time.Minute
	exchangeCodeTTL  = 60 * time.Second
	defaultOAuthRole = "viewer" // 外部アカウントで登録した利用者の既定ロール
)

// oauthFlow は認可開始時に控えておく情報。
// LinkUserID が入っていれば「ログイン中の利用者へ連携を追加する」フロー、
// 空なら「ログイン（初回なら登録）」フロー。
type oauthFlow struct {
	Provider   string
	LinkUserID *uuid.UUID
	ExpiresAt  time.Time
}

// OAuthService は外部アカウントでのログインと連携の管理を担う。
//
// state と引き換えコードはプロセス内メモリに持つ。どちらも数分で失効する一時値で、
// 再起動時に失われても利用者はログインをやり直すだけで済む。
type OAuthService struct {
	authRepo     *repository.AuthRepository
	oauthRepo    *repository.OAuthRepository
	providers    map[string]oauth.Provider
	redirectBase string

	mu            sync.Mutex
	flows         map[string]oauthFlow     // state -> フロー情報
	exchangeCodes map[string]exchangeEntry // 引き換えコード -> 発行済みセッション
}

type exchangeEntry struct {
	Token     string
	ExpiresAt time.Time
}

func NewOAuthService(
	authRepo *repository.AuthRepository,
	oauthRepo *repository.OAuthRepository,
	redirectBase string,
	providers ...oauth.Provider,
) *OAuthService {
	m := make(map[string]oauth.Provider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	return &OAuthService{
		authRepo:      authRepo,
		oauthRepo:     oauthRepo,
		providers:     m,
		redirectBase:  strings.TrimSuffix(redirectBase, "/"),
		flows:         make(map[string]oauthFlow),
		exchangeCodes: make(map[string]exchangeEntry),
	}
}

// EnabledProviders は資格情報が設定済みの連携先名を返す（ログイン画面の出し分け用）。
func (s *OAuthService) EnabledProviders() []string {
	var names []string
	for name, p := range s.providers {
		if p.Configured() {
			names = append(names, name)
		}
	}
	return names
}

func (s *OAuthService) provider(name string) (oauth.Provider, error) {
	p, ok := s.providers[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	if !p.Configured() {
		return nil, ErrProviderNotEnabled
	}
	return p, nil
}

// redirectURI は Google 側に登録する「承認済みのリダイレクト URI」と完全一致させる必要がある。
func (s *OAuthService) redirectURI(provider string) string {
	return fmt.Sprintf("%s/api/auth/oauth/%s/callback", s.redirectBase, provider)
}

// Start は認可 URL を組み立て、state を控える。
// linkUserID が非 nil なら既存アカウントへの連携追加として扱う。
func (s *OAuthService) Start(providerName string, linkUserID *uuid.UUID) (string, error) {
	p, err := s.provider(providerName)
	if err != nil {
		return "", err
	}
	state, err := oauth.GenerateState()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.pruneLocked()
	s.flows[state] = oauthFlow{
		Provider:   providerName,
		LinkUserID: linkUserID,
		ExpiresAt:  time.Now().Add(oauthStateTTL),
	}
	s.mu.Unlock()

	return p.AuthCodeURL(state, s.redirectURI(providerName)), nil
}

// Callback は認可コードを処理し、フロントエンドへ渡す引き換えコードを返す。
// セッショントークンそのものをリダイレクト URL に載せないための一手間で、
// 引き換えコードは1回限り・60秒で失効する。
func (s *OAuthService) Callback(ctx context.Context, providerName, code, state string) (string, error) {
	p, err := s.provider(providerName)
	if err != nil {
		return "", err
	}

	// state を消費（再利用させない）
	s.mu.Lock()
	flow, ok := s.flows[state]
	delete(s.flows, state)
	s.mu.Unlock()
	if !ok || flow.Provider != providerName || time.Now().After(flow.ExpiresAt) {
		return "", ErrOAuthState
	}

	info, err := p.Exchange(ctx, code, s.redirectURI(providerName))
	if err != nil {
		return "", err
	}

	var user *models.User
	if flow.LinkUserID != nil {
		user, err = s.linkToExistingUser(*flow.LinkUserID, providerName, info)
	} else {
		user, err = s.loginOrRegister(providerName, info)
	}
	if err != nil {
		return "", err
	}

	token, err := s.issueSession(user)
	if err != nil {
		return "", err
	}
	return s.storeExchangeCode(token)
}

// linkToExistingUser はログイン中の利用者へ連携を追加する。
func (s *OAuthService) linkToExistingUser(userID uuid.UUID, providerName string, info *oauth.UserInfo) (*models.User, error) {
	existing, err := s.oauthRepo.FindByProviderUserID(providerName, info.ProviderUserID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.UserID != userID {
		return nil, ErrIdentityTaken
	}

	user, err := s.authRepo.FindUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	if err := s.link(userID, providerName, info); err != nil {
		return nil, err
	}
	return user, nil
}

// loginOrRegister は連携済みならそのままログイン、未連携なら新規登録する。
//
// メール一致による自動紐付けは、相手側で確認済みのメールで、かつ こちら側の
// アカウントもメール確認済みの場合に限る。こちらが未確認（管理者が作った
// アカウントなど）だと、そのメールの Google アカウントを持つ第三者に
// 乗っ取られうるため、明示的な連携（ログイン後に設定画面から）を求める。
func (s *OAuthService) loginOrRegister(providerName string, info *oauth.UserInfo) (*models.User, error) {
	identity, err := s.oauthRepo.FindByProviderUserID(providerName, info.ProviderUserID)
	if err != nil {
		return nil, err
	}
	if identity != nil {
		user, err := s.authRepo.FindUserByID(identity.UserID)
		if err != nil {
			return nil, err
		}
		if user == nil {
			return nil, ErrUserNotFound
		}
		if !user.IsActive {
			return nil, ErrUserInactive
		}
		// プロフィールの変更に追随させる
		if err := s.link(user.ID, providerName, info); err != nil {
			logger.Warnf("[oauth] failed to refresh identity for %s: %v", user.Username, err)
		}
		return user, nil
	}

	if info.Email != "" {
		byEmail, err := s.authRepo.FindUserByEmail(info.Email)
		if err != nil {
			return nil, err
		}
		if byEmail != nil {
			if !info.EmailVerified || !byEmail.EmailVerified {
				return nil, ErrEmailTaken
			}
			if err := s.link(byEmail.ID, providerName, info); err != nil {
				return nil, err
			}
			return byEmail, nil
		}
	}

	return s.register(providerName, info)
}

// register は外部アカウントから新しい利用者を作る（既定は viewer ロール）。
func (s *OAuthService) register(providerName string, info *oauth.UserInfo) (*models.User, error) {
	role, err := s.authRepo.FindRoleByName(defaultOAuthRole)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, fmt.Errorf("既定ロール %q が見つかりません", defaultOAuthRole)
	}

	username, err := s.uniqueUsername(info)
	if err != nil {
		return nil, err
	}
	displayName := strings.TrimSpace(info.DisplayName)
	if displayName == "" {
		displayName = username
	}

	user, err := s.authRepo.CreateOAuthUser(username, displayName, info.Email, info.EmailVerified, role.ID)
	if err != nil {
		return nil, err
	}
	if err := s.link(user.ID, providerName, info); err != nil {
		return nil, err
	}
	logger.Infof("[oauth] registered new user %s via %s", user.Username, providerName)
	return user, nil
}

var usernameSanitizer = regexp.MustCompile(`[^a-z0-9_]+`)

// uniqueUsername は表示名やメールから使えるユーザー名を作り、衝突すれば連番を付ける。
func (s *OAuthService) uniqueUsername(info *oauth.UserInfo) (string, error) {
	base := ""
	if info.Email != "" {
		base = strings.SplitN(info.Email, "@", 2)[0]
	}
	if base == "" {
		base = info.DisplayName
	}
	base = usernameSanitizer.ReplaceAllString(strings.ToLower(base), "")
	if len(base) < 3 {
		base = "user"
	}
	if len(base) > 24 {
		base = base[:24]
	}

	for i := 0; i < 100; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s%d", base, i)
		}
		exists, err := s.authRepo.UsernameExists(candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	// 100 個試して埋まっているのは異常。ランダム後綴で確実に空きを作る。
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate username suffix: %w", err)
	}
	return base + base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *OAuthService) link(userID uuid.UUID, providerName string, info *oauth.UserInfo) error {
	identity := &models.OAuthIdentity{
		UserID:         userID,
		Provider:       providerName,
		ProviderUserID: info.ProviderUserID,
	}
	if info.Email != "" {
		identity.Email = &info.Email
	}
	if info.DisplayName != "" {
		identity.DisplayName = &info.DisplayName
	}
	if info.AvatarURL != "" {
		identity.AvatarURL = &info.AvatarURL
	}
	return s.oauthRepo.Link(identity)
}

func (s *OAuthService) issueSession(user *models.User) (string, error) {
	if !user.IsActive {
		return "", ErrUserInactive
	}
	token, err := auth.GenerateToken()
	if err != nil {
		return "", err
	}
	if err := s.authRepo.CreateSession(auth.HashToken(token), user.ID, time.Now().Add(SessionTTL)); err != nil {
		return "", err
	}
	if err := s.authRepo.TouchLastLogin(user.ID); err != nil {
		logger.Warnf("[oauth] failed to update last_login for %s: %v", user.Username, err)
	}
	return token, nil
}

// storeExchangeCode はセッショントークンを短命な引き換えコードの裏に隠す。
func (s *OAuthService) storeExchangeCode(token string) (string, error) {
	code, err := oauth.GenerateState()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.pruneLocked()
	s.exchangeCodes[code] = exchangeEntry{Token: token, ExpiresAt: time.Now().Add(exchangeCodeTTL)}
	s.mu.Unlock()
	return code, nil
}

// RedeemExchangeCode は引き換えコードをセッショントークンに替える（1回限り）。
func (s *OAuthService) RedeemExchangeCode(code string) (string, error) {
	s.mu.Lock()
	entry, ok := s.exchangeCodes[code]
	delete(s.exchangeCodes, code)
	s.mu.Unlock()
	if !ok || time.Now().After(entry.ExpiresAt) {
		return "", ErrOAuthCodeExpired
	}
	return entry.Token, nil
}

// ListIdentities は利用者に紐付く連携一覧（設定画面用）。
func (s *OAuthService) ListIdentities(userID uuid.UUID) ([]models.OAuthIdentity, error) {
	return s.oauthRepo.FindByUserID(userID)
}

// Unlink は連携を解除する。解除するとログインできなくなる場合は拒否する
// （パスワードも他の連携も無い状態を作らない）。
func (s *OAuthService) Unlink(userID uuid.UUID, providerName string) error {
	user, err := s.authRepo.FindUserByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrUserNotFound
	}
	count, err := s.oauthRepo.CountByUserID(userID)
	if err != nil {
		return err
	}
	if user.PasswordHash == "" && count <= 1 {
		return ErrLastLoginMethod
	}
	return s.oauthRepo.Unlink(userID, providerName)
}

// pruneLocked は期限切れの state / 引き換えコードを捨てる（呼び出し側で mu を保持）。
func (s *OAuthService) pruneLocked() {
	now := time.Now()
	for k, v := range s.flows {
		if now.After(v.ExpiresAt) {
			delete(s.flows, k)
		}
	}
	for k, v := range s.exchangeCodes {
		if now.After(v.ExpiresAt) {
			delete(s.exchangeCodes, k)
		}
	}
}
