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
	JWTSecret          string
	APIAuthToken       string
	BootstrapAdminUser string
	BootstrapAdminPass string
	Environment        string
	LogLevel           string

	// バックアップ関連
	BackupDir             string // ダンプの保存先ディレクトリ
	BackupDockerContainer string // ホストに pg_dump が無い場合に使う PostgreSQL コンテナ名
	GoogleOAuthClientID   string // Google Drive 連携用 OAuth クライアント（TV と限定入力デバイス型）
	GoogleOAuthSecret     string
}

// Load 環境変数から設定を読み込み
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/setori?sslmode=disable"),
		HolodexAPIKey:      getEnv("HOLODEX_API_KEY", ""),
		HolodexEditorToken: getEnv("HOLODEX_EDITOR_TOKEN", ""),
		YouTubeAPIKey:      getEnv("YOUTUBE_API_KEY", ""),
		GroqAPIKey:         getEnv("GROQ_API_KEY", ""),
		JWTSecret:          getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
		// APIAuthToken が設定されている場合、書き込み操作（POST/PUT/DELETE/PATCH）に対し
		// Authorization: Bearer <token> を要求。空欄時は公開（ローカル開発向け）。
		APIAuthToken: getEnv("API_AUTH_TOKEN", ""),
		// 初回起動時、ユーザーが 0 件なら作成する管理者アカウント。
		BootstrapAdminUser: getEnv("ADMIN_USERNAME", "admin"),
		BootstrapAdminPass: getEnv("ADMIN_PASSWORD", "admin"),
		Environment:        getEnv("ENVIRONMENT", "development"),
		LogLevel:           getEnv("LOG_LEVEL", "INFO"),

		BackupDir:             getEnv("BACKUP_DIR", "./backups"),
		BackupDockerContainer: getEnv("BACKUP_PG_CONTAINER", "setori-postgres"),
		GoogleOAuthClientID:   getEnv("GOOGLE_OAUTH_CLIENT_ID", ""),
		GoogleOAuthSecret:     getEnv("GOOGLE_OAUTH_CLIENT_SECRET", ""),
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
