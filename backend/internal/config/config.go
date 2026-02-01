package config

import (
	"os"
)

// Config 應用程式設定
type Config struct {
	DatabaseURL        string
	HolodexAPIKey      string
	HolodexEditorToken string
	YouTubeAPIKey      string
	GroqAPIKey         string
	JWTSecret          string
	Environment        string
}

// Load 載入設定從環境變數
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/setori?sslmode=disable"),
		HolodexAPIKey:      getEnv("HOLODEX_API_KEY", ""),
		HolodexEditorToken: getEnv("HOLODEX_EDITOR_TOKEN", ""),
		YouTubeAPIKey:      getEnv("YOUTUBE_API_KEY", ""),
		GroqAPIKey:         getEnv("GROQ_API_KEY", ""),
		JWTSecret:          getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
		Environment:        getEnv("ENVIRONMENT", "development"),
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
