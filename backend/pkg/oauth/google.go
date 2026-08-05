package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	googleAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL    = "https://oauth2.googleapis.com/token"
	googleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
)

// GoogleProvider は Google アカウントでのログイン。
// ウェブアプリケーション型の OAuth クライアント（クライアント ID / シークレット）が必要。
// Drive バックアップで使っている「TV と限定入力デバイス」型のクライアントとは別物で、
// あちらはデバイスフロー専用のためリダイレクトフローには使えない。
type GoogleProvider struct {
	mu           sync.RWMutex // 資格情報は管理画面から実行中に差し替えられる
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

// SetCredentials は資格情報を差し替える（設定画面での変更を再起動なしに反映するため）。
func (p *GoogleProvider) SetCredentials(clientID, clientSecret string) {
	p.mu.Lock()
	p.clientID = clientID
	p.clientSecret = clientSecret
	p.mu.Unlock()
}

func (p *GoogleProvider) creds() (string, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.clientID, p.clientSecret
}

func NewGoogleProvider(clientID, clientSecret string) *GoogleProvider {
	return &GoogleProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *GoogleProvider) Name() string { return "google" }

func (p *GoogleProvider) Configured() bool {
	id, secret := p.creds()
	return id != "" && secret != ""
}

func (p *GoogleProvider) AuthCodeURL(state, redirectURI string) string {
	id, _ := p.creds()
	q := url.Values{}
	q.Set("client_id", id)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	// 毎回アカウント選択を出す（複数アカウント利用者が意図せず別アカウントで入るのを防ぐ）
	q.Set("prompt", "select_account")
	return googleAuthURL + "?" + q.Encode()
}

// googleUserInfo は userinfo エンドポイントの応答（必要な項目のみ）。
type googleUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func (p *GoogleProvider) Exchange(ctx context.Context, code, redirectURI string) (*UserInfo, error) {
	if !p.Configured() {
		return nil, fmt.Errorf("google oauth client is not configured")
	}

	clientID, clientSecret := p.creds()
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 応答本文にはコードやクライアント情報が含まれうるのでそのままは返さない
		return nil, fmt.Errorf("exchange code: google returned %s", resp.Status)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("exchange code: no access token in response")
	}

	return p.fetchUserInfo(ctx, tokenResp.AccessToken)
}

func (p *GoogleProvider) fetchUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch userinfo: google returned %s", resp.Status)
	}

	var info googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	if info.Sub == "" {
		return nil, fmt.Errorf("fetch userinfo: missing subject")
	}

	return &UserInfo{
		ProviderUserID: info.Sub,
		Email:          info.Email,
		EmailVerified:  info.EmailVerified,
		DisplayName:    info.Name,
		AvatarURL:      info.Picture,
	}, nil
}
