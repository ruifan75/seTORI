package config

import (
	"os"
)

// Config アプリケーション設定
type Config struct {
	DatabaseURL        string
	HolodexAPIKey      string
	HolodexEditorToken string
	YouTubeAPIKey      string
	GroqAPIKey         string
	BootstrapAdminUser string
	BootstrapAdminPass string
	Environment        string
	LogLevel           string

	// バックアップ関連
	BackupDir             string // ダンプの保存先ディレクトリ
	BackupDockerContainer string // ホストに pg_dump が無い場合に使う PostgreSQL コンテナ名
	GoogleOAuthClientID   string // Google Drive 連携用 OAuth クライアント（TV と限定入力デバイス型）
	GoogleOAuthSecret     string

	// 外部アカウントでのログイン（サインイン用。上の Drive 用とは別のクライアントが必要。
	// Drive 側は「TV と限定入力デバイス」型で、ウェブのリダイレクトフローには使えない）
	GoogleSigninClientID string
	GoogleSigninSecret   string
	// OAuth コールバックの受け口。Google 側の「承認済みのリダイレクト URI」と完全一致させる。
	OAuthRedirectBaseURL string
	// 認証後に戻すフロントエンドの URL
	FrontendBaseURL string

	// 管理画面から保存する API キー類を DB 上で暗号化するための鍵。
	// DB のバックアップは Google Drive へ自動アップロードされるため、
	// 鍵だけは DB に置かず環境変数で持つ（鍵が無ければ機密の保存を拒否する）。
	SettingsEncryptionKey string
}

// Load 環境変数から設定を読み込み
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/setori?sslmode=disable"),
		HolodexAPIKey:      getEnv("HOLODEX_API_KEY", ""),
		HolodexEditorToken: getEnv("HOLODEX_EDITOR_TOKEN", ""),
		YouTubeAPIKey:      getEnv("YOUTUBE_API_KEY", ""),
		GroqAPIKey:         getEnv("GROQ_API_KEY", ""),
		// 初回起動時、ユーザーが 0 件なら作成する管理者アカウント。
		BootstrapAdminUser: getEnv("ADMIN_USERNAME", "admin"),
		BootstrapAdminPass: getEnv("ADMIN_PASSWORD", "admin"),
		Environment:        getEnv("ENVIRONMENT", "development"),
		LogLevel:           getEnv("LOG_LEVEL", "INFO"),

		BackupDir:             getEnv("BACKUP_DIR", "./backups"),
		BackupDockerContainer: getEnv("BACKUP_PG_CONTAINER", "setori-postgres"),
		GoogleOAuthClientID:   getEnv("GOOGLE_OAUTH_CLIENT_ID", ""),
		GoogleOAuthSecret:     getEnv("GOOGLE_OAUTH_CLIENT_SECRET", ""),

		GoogleSigninClientID: getEnv("GOOGLE_SIGNIN_CLIENT_ID", ""),
		GoogleSigninSecret:   getEnv("GOOGLE_SIGNIN_CLIENT_SECRET", ""),
		OAuthRedirectBaseURL: getEnv("OAUTH_REDIRECT_BASE_URL", "http://localhost:8080"),
		FrontendBaseURL:      getEnv("FRONTEND_BASE_URL", "http://localhost:5173"),

		SettingsEncryptionKey: getEnv("SETTINGS_ENCRYPTION_KEY", ""),
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
