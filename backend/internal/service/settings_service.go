package service

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/secrets"
)

// appSettingsKey は app_settings 上の保存キー。
const integrationSettingsKey = "integrations"

// IntegrationSettings は管理画面から編集できる外部サービス連携の設定。
//
// 値の解決順は「DB に保存された値 → 環境変数」。これにより、
// 既存の .env 運用はそのまま動き続け、UI で設定した時点でそちらが優先される。
// DATABASE_URL のように DB を読む前に必要な設定はここには入れられない。
type IntegrationSettings struct {
	HolodexAPIKey      string `json:"holodex_api_key"`
	HolodexEditorToken string `json:"holodex_editor_token"`
	YouTubeAPIKey      string `json:"youtube_api_key"`
	GroqAPIKey         string `json:"groq_api_key"`
	// Google Drive バックアップ用（TV と限定入力デバイス型クライアント）
	GoogleDriveClientID string `json:"google_drive_client_id"`
	GoogleDriveSecret   string `json:"google_drive_secret"`
	// ログイン用（ウェブアプリケーション型クライアント）
	GoogleSigninClientID string `json:"google_signin_client_id"`
	GoogleSigninSecret   string `json:"google_signin_secret"`
	// live chat 取得（拍手による歌唱 end 推定）で yt-dlp に渡す cookies.txt の中身。
	// YouTube に BOT 判定される環境ではこれが無いと live chat を落とせない。
	YtdlpCookies string `json:"ytdlp_cookies"`
}

// secretFields は暗号化して保存し、API では末尾4桁のヒントしか返さない項目。
func (s *IntegrationSettings) secretFields() map[string]*string {
	return map[string]*string{
		"holodex_api_key":      &s.HolodexAPIKey,
		"holodex_editor_token": &s.HolodexEditorToken,
		"youtube_api_key":      &s.YouTubeAPIKey,
		"groq_api_key":         &s.GroqAPIKey,
		"google_drive_secret":  &s.GoogleDriveSecret,
		"google_signin_secret": &s.GoogleSigninSecret,
		"ytdlp_cookies":        &s.YtdlpCookies,
	}
}

// envFallback は各項目に対応する環境変数値（設定サービス生成時に config から渡される）。
type envFallback struct {
	HolodexAPIKey        string
	HolodexEditorToken   string
	YouTubeAPIKey        string
	GroqAPIKey           string
	GoogleDriveClientID  string
	GoogleDriveSecret    string
	GoogleSigninClientID string
	GoogleSigninSecret   string
	YtdlpCookies         string
}

// SettingsService は連携設定の読み書きと、変更の即時反映を担う。
type SettingsService struct {
	repo   *repository.AppSettingsRepository
	cipher *secrets.Cipher
	env    envFallback

	mu       sync.RWMutex
	current  IntegrationSettings
	appliers []func(IntegrationSettings)
}

func NewSettingsService(
	repo *repository.AppSettingsRepository,
	cipher *secrets.Cipher,
	holodexKey, holodexEditorToken, youtubeKey, groqKey string,
	driveClientID, driveSecret, signinClientID, signinSecret string,
	ytdlpCookies string,
) *SettingsService {
	return &SettingsService{
		repo:   repo,
		cipher: cipher,
		env: envFallback{
			HolodexAPIKey:        holodexKey,
			HolodexEditorToken:   holodexEditorToken,
			YouTubeAPIKey:        youtubeKey,
			GroqAPIKey:           groqKey,
			GoogleDriveClientID:  driveClientID,
			GoogleDriveSecret:    driveSecret,
			GoogleSigninClientID: signinClientID,
			GoogleSigninSecret:   signinSecret,
			YtdlpCookies:         ytdlpCookies,
		},
	}
}

// OnChange は設定が変わったときに呼ぶ反映処理を登録する。
// 各サービスへ新しい値を押し込むことで、再起動なしに変更を効かせる。
func (s *SettingsService) OnChange(fn func(IntegrationSettings)) {
	s.mu.Lock()
	s.appliers = append(s.appliers, fn)
	s.mu.Unlock()
}

// Load は DB から設定を読み、env で穴埋めしてキャッシュし、反映処理を走らせる。
// 起動時に1回呼ぶ。
func (s *SettingsService) Load() error {
	stored, err := s.readStored()
	if err != nil {
		return err
	}
	resolved := s.applyEnvFallback(stored)

	s.mu.Lock()
	s.current = resolved
	appliers := append([]func(IntegrationSettings){}, s.appliers...)
	s.mu.Unlock()

	for _, fn := range appliers {
		fn(resolved)
	}
	return nil
}

// Current は解決済みの設定（復号済み）を返す。サービスからの参照用。
func (s *SettingsService) Current() IntegrationSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// readStored は DB の保存値を復号して返す。未保存ならゼロ値。
func (s *SettingsService) readStored() (IntegrationSettings, error) {
	var raw IntegrationSettings
	found, err := s.repo.Get(integrationSettingsKey, &raw)
	if err != nil {
		return IntegrationSettings{}, fmt.Errorf("read integration settings: %w", err)
	}
	if !found {
		return IntegrationSettings{}, nil
	}

	for name, field := range raw.secretFields() {
		plain, err := s.cipher.Decrypt(*field)
		if err != nil {
			// 1項目の復号失敗で全体を落とさない。鍵の取り違えに気付けるよう警告は出す。
			logger.Warnf("[settings] failed to decrypt %s: %v", name, err)
			*field = ""
			continue
		}
		*field = plain
	}
	return raw, nil
}

// applyEnvFallback は未設定の項目を環境変数で埋める。
func (s *SettingsService) applyEnvFallback(v IntegrationSettings) IntegrationSettings {
	fallback := func(target *string, envValue string) {
		if strings.TrimSpace(*target) == "" {
			*target = envValue
		}
	}
	fallback(&v.HolodexAPIKey, s.env.HolodexAPIKey)
	fallback(&v.HolodexEditorToken, s.env.HolodexEditorToken)
	fallback(&v.YouTubeAPIKey, s.env.YouTubeAPIKey)
	fallback(&v.GroqAPIKey, s.env.GroqAPIKey)
	fallback(&v.GoogleDriveClientID, s.env.GoogleDriveClientID)
	fallback(&v.GoogleDriveSecret, s.env.GoogleDriveSecret)
	fallback(&v.GoogleSigninClientID, s.env.GoogleSigninClientID)
	fallback(&v.GoogleSigninSecret, s.env.GoogleSigninSecret)
	fallback(&v.YtdlpCookies, s.env.YtdlpCookies)
	return v
}

// Describe は管理画面向けの表示用データを返す。機密の値そのものは返さず、
// 「設定済みか」と末尾4桁のヒント、どこ由来か（DB / 環境変数）だけを返す。
func (s *SettingsService) Describe() (*dto.IntegrationSettingsResponse, error) {
	stored, err := s.readStored()
	if err != nil {
		return nil, err
	}
	resolved := s.applyEnvFallback(stored)

	storedSecrets := stored.secretFields()
	resolvedSecrets := resolved.secretFields()

	resp := &dto.IntegrationSettingsResponse{
		EncryptionEnabled: s.cipher.Enabled(),
		Secrets:           make(map[string]dto.SecretFieldStatus, len(resolvedSecrets)),
		Plain: map[string]string{
			"google_drive_client_id":  resolved.GoogleDriveClientID,
			"google_signin_client_id": resolved.GoogleSigninClientID,
		},
		PlainFromEnv: map[string]bool{
			"google_drive_client_id":  strings.TrimSpace(stored.GoogleDriveClientID) == "" && resolved.GoogleDriveClientID != "",
			"google_signin_client_id": strings.TrimSpace(stored.GoogleSigninClientID) == "" && resolved.GoogleSigninClientID != "",
		},
	}

	for name, resolvedField := range resolvedSecrets {
		fromEnv := strings.TrimSpace(*storedSecrets[name]) == "" && *resolvedField != ""
		resp.Secrets[name] = dto.SecretFieldStatus{
			Configured: *resolvedField != "",
			Hint:       secrets.Hint(*resolvedField),
			FromEnv:    fromEnv,
		}
	}
	return resp, nil
}

// Update は送られてきた項目だけを更新する。
// 機密は「空文字＝変更なし」として扱う（UI は値を保持していないため、
// 空送信を消去と解釈すると画面を開くたびに消えてしまう）。
// 明示的に消したい場合は Clear で項目名を指定する。
func (s *SettingsService) Update(req *dto.UpdateIntegrationSettingsRequest) error {
	stored, err := s.readStored()
	if err != nil {
		return err
	}

	fields := stored.secretFields()
	for name, value := range req.Secrets {
		field, ok := fields[name]
		if !ok {
			return fmt.Errorf("不明な設定項目です: %s", name)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue // 変更なし
		}
		if !s.cipher.Enabled() {
			return secrets.ErrNoKey
		}
		*field = value
	}

	for _, name := range req.Clear {
		field, ok := fields[name]
		if !ok {
			// 平文項目のクリアも受け付ける
			switch name {
			case "google_drive_client_id":
				stored.GoogleDriveClientID = ""
				continue
			case "google_signin_client_id":
				stored.GoogleSigninClientID = ""
				continue
			}
			return fmt.Errorf("不明な設定項目です: %s", name)
		}
		*field = ""
	}

	if req.GoogleDriveClientID != nil {
		stored.GoogleDriveClientID = strings.TrimSpace(*req.GoogleDriveClientID)
	}
	if req.GoogleSigninClientID != nil {
		stored.GoogleSigninClientID = strings.TrimSpace(*req.GoogleSigninClientID)
	}

	// 保存直前に暗号化（stored は復号済みの状態で持ち回っている）
	toSave := stored
	for name, field := range toSave.secretFields() {
		enc, err := s.cipher.Encrypt(*field)
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", name, err)
		}
		*field = enc
	}
	if err := s.repo.Set(integrationSettingsKey, toSave); err != nil {
		return fmt.Errorf("save integration settings: %w", err)
	}

	// 再読込して反映（env フォールバックも含めた最終値を配る）
	return s.Load()
}
