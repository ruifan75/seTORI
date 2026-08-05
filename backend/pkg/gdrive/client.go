// Package gdrive は Google Drive API の最小クライアント。
// OAuth 2.0 デバイスフロー（TV と限定入力デバイス向けフロー）で認可し、
// drive.file スコープ（このアプリが作成したファイルのみアクセス可）で
// バックアップのアップロード・一覧・削除・ダウンロードを行う。
// 外部依存なし（標準ライブラリのみ）。
package gdrive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	deviceCodeURL = "https://oauth2.googleapis.com/device/code"
	tokenURL      = "https://oauth2.googleapis.com/token"
	revokeURL     = "https://oauth2.googleapis.com/revoke"
	driveAPI      = "https://www.googleapis.com/drive/v3"
	driveUpload   = "https://www.googleapis.com/upload/drive/v3"

	// ScopeDriveFile はこのアプリが作成・開いたファイルのみを扱うスコープ。
	// デバイスフローで許可されている数少ない Drive スコープの一つ。
	ScopeDriveFile = "https://www.googleapis.com/auth/drive.file"
)

// Client は OAuth クライアント資格情報を保持する。
// 資格情報は管理画面から実行中に差し替えられるため mu で保護する。
type Client struct {
	mu           sync.RWMutex
	ClientID     string
	ClientSecret string
	http         *http.Client
}

// SetCredentials は資格情報を差し替える（設定画面での変更を再起動なしに反映するため）。
func (c *Client) SetCredentials(clientID, clientSecret string) {
	c.mu.Lock()
	c.ClientID = clientID
	c.ClientSecret = clientSecret
	c.mu.Unlock()
}

// creds は資格情報を安全に読み出す。
func (c *Client) creds() (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ClientID, c.ClientSecret
}

func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		http:         &http.Client{Timeout: 5 * time.Minute},
	}
}

// Configured はクライアント ID/シークレットが設定済みかを返す。
func (c *Client) Configured() bool {
	if c == nil {
		return false
	}
	id, secret := c.creds()
	return id != "" && secret != ""
}

// ========== OAuth デバイスフロー ==========

// DeviceAuth はデバイスフロー開始時のレスポンス。
type DeviceAuth struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// Token はトークンエンドポイントのレスポンス。
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// StartDeviceAuth はデバイスフローを開始し、ユーザーに提示するコードと URL を返す。
func (c *Client) StartDeviceAuth() (*DeviceAuth, error) {
	id, _ := c.creds()
	form := url.Values{"client_id": {id}, "scope": {ScopeDriveFile}}
	var auth DeviceAuth
	if err := c.postForm(deviceCodeURL, form, &auth); err != nil {
		return nil, err
	}
	if auth.Interval <= 0 {
		auth.Interval = 5
	}
	return &auth, nil
}

// ErrAuthPending はユーザーがまだ承認していないことを示す（ポーリング継続）。
var ErrAuthPending = fmt.Errorf("authorization_pending")

// PollDeviceToken はデバイスフローのトークン取得を1回試行する。
// ユーザー未承認の間は ErrAuthPending を返す。
func (c *Client) PollDeviceToken(deviceCode string) (*Token, error) {
	clientID, clientSecret := c.creds()
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"device_code":   {deviceCode},
		"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	var tok Token
	if err := c.postForm(tokenURL, form, &tok); err != nil {
		// authorization_pending / slow_down は「まだ」の意味
		if strings.Contains(err.Error(), "authorization_pending") || strings.Contains(err.Error(), "slow_down") {
			return nil, ErrAuthPending
		}
		return nil, err
	}
	return &tok, nil
}

// RefreshAccessToken はリフレッシュトークンからアクセストークンを取得する。
func (c *Client) RefreshAccessToken(refreshToken string) (*Token, error) {
	clientID, clientSecret := c.creds()
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	var tok Token
	if err := c.postForm(tokenURL, form, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// Revoke はトークン（リフレッシュ/アクセス）を無効化する。失敗しても致命的ではない。
func (c *Client) Revoke(token string) error {
	resp, err := c.http.PostForm(revokeURL, url.Values{"token": {token}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// postForm はフォーム POST → JSON レスポンス。Google のエラー形式を読み取ってエラー化する。
func (c *Client) postForm(endpoint string, form url.Values, dest interface{}) error {
	resp, err := c.http.PostForm(endpoint, form)
	if err != nil {
		return fmt.Errorf("google oauth request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error != "" {
			return fmt.Errorf("google oauth: %s (%s)", e.Error, e.ErrorDescription)
		}
		return fmt.Errorf("google oauth: HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("google oauth: decode response: %w", err)
	}
	return nil
}

// ========== Drive API ==========

// File は Drive 上のファイルのメタ情報。
type File struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size,string,omitempty"`
	CreatedTime string `json:"createdTime,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// AboutUser は連携アカウント情報。
type AboutUser struct {
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

// About は連携中の Google アカウント情報を取得する。
func (c *Client) About(accessToken string) (*AboutUser, error) {
	var out struct {
		User AboutUser `json:"user"`
	}
	if err := c.apiJSON(accessToken, http.MethodGet, driveAPI+"/about?fields=user(displayName,emailAddress)", nil, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

// EnsureFolder は指定名のフォルダを（無ければ作成して）返す。
// drive.file スコープではこのアプリが作成したフォルダのみ検索にヒットする。
func (c *Client) EnsureFolder(accessToken, name string) (*File, error) {
	q := fmt.Sprintf("name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", strings.ReplaceAll(name, "'", "\\'"))
	listURL := driveAPI + "/files?q=" + url.QueryEscape(q) + "&fields=" + url.QueryEscape("files(id,name)")
	var list struct {
		Files []File `json:"files"`
	}
	if err := c.apiJSON(accessToken, http.MethodGet, listURL, nil, &list); err != nil {
		return nil, err
	}
	if len(list.Files) > 0 {
		return &list.Files[0], nil
	}

	meta := map[string]interface{}{"name": name, "mimeType": "application/vnd.google-apps.folder"}
	body, _ := json.Marshal(meta)
	var created File
	if err := c.apiJSON(accessToken, http.MethodPost, driveAPI+"/files?fields=id,name", bytes.NewReader(body), &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// Upload は resumable upload でファイルを folderID 直下にアップロードする。
// （multipart は 5MB 制限があるため、サイズによらず resumable を使う）
func (c *Client) Upload(accessToken, folderID, name string, r io.Reader, size int64) (*File, error) {
	meta := map[string]interface{}{"name": name}
	if folderID != "" {
		meta["parents"] = []string{folderID}
	}
	metaBody, _ := json.Marshal(meta)

	// 1) セッション開始
	req, err := http.NewRequest(http.MethodPost, driveUpload+"/files?uploadType=resumable&fields=id,name,size,createdTime", bytes.NewReader(metaBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	if size > 0 {
		req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(size, 10))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("drive upload init: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("drive upload init: HTTP %d", resp.StatusCode)
	}
	session := resp.Header.Get("Location")
	if session == "" {
		return nil, fmt.Errorf("drive upload init: no session URI")
	}

	// 2) 本体を一括 PUT
	put, err := http.NewRequest(http.MethodPut, session, r)
	if err != nil {
		return nil, err
	}
	put.Header.Set("Content-Type", "application/octet-stream")
	if size > 0 {
		put.ContentLength = size
	}
	resp2, err := c.http.Do(put)
	if err != nil {
		return nil, fmt.Errorf("drive upload: %w", err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("drive upload: HTTP %d: %s", resp2.StatusCode, truncate(string(body), 200))
	}
	var f File
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("drive upload: decode response: %w", err)
	}
	return &f, nil
}

// ListFiles は folderID 直下のファイル一覧（新しい順）を返す。
func (c *Client) ListFiles(accessToken, folderID string) ([]File, error) {
	q := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
	u := driveAPI + "/files?q=" + url.QueryEscape(q) +
		"&fields=" + url.QueryEscape("files(id,name,size,createdTime,mimeType)") +
		"&orderBy=" + url.QueryEscape("createdTime desc") + "&pageSize=100"
	var list struct {
		Files []File `json:"files"`
	}
	if err := c.apiJSON(accessToken, http.MethodGet, u, nil, &list); err != nil {
		return nil, err
	}
	return list.Files, nil
}

// DeleteFile はファイルを完全削除する。
func (c *Client) DeleteFile(accessToken, fileID string) error {
	req, err := http.NewRequest(http.MethodDelete, driveAPI+"/files/"+url.PathEscape(fileID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("drive delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("drive delete: HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return nil
}

// Download はファイル内容のリーダーを返す。呼び出し側で Close すること。
func (c *Client) Download(accessToken, fileID string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, driveAPI+"/files/"+url.PathEscape(fileID)+"?alt=media", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("drive download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("drive download: HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return resp.Body, nil
}

// apiJSON は Bearer 付き JSON API 呼び出しの共通処理。
func (c *Client) apiJSON(accessToken, method, u string, body io.Reader, dest interface{}) error {
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("drive api: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error.Message != "" {
			return fmt.Errorf("drive api: %s", e.Error.Message)
		}
		return fmt.Errorf("drive api: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	if dest != nil {
		if err := json.Unmarshal(raw, dest); err != nil {
			return fmt.Errorf("drive api: decode response: %w", err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
