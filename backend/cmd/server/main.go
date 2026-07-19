package main

import (
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ruifan75/setori/internal/config"
	"github.com/ruifan75/setori/internal/database"
	"github.com/ruifan75/setori/internal/handler"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/joho/godotenv"
)

func main() {
	// .env ファイルを読み込み
	if err := godotenv.Load(); err != nil {
		logger.Warnf("警告: .env ファイルの読み込みに失敗しました: %v", err)
	}

	// 設定を読み込み
	cfg, err := config.Load()
	if err != nil {
		logger.Errorf("設定の読み込みに失敗しました: %v", err)
		// since logger may not be set yet, but for fatal we can use os.Exit or keep
		os.Exit(1)
	}

	logger.SetLevel(cfg.LogLevel)
	logger.Infof("log level set to %s", logger.GetLevel())

	// データベースに接続
	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		logger.Errorf("データベース接続に失敗しました: %v", err)
		os.Exit(1)
	}
	defer db.Close()

	// データベースマイグレーションを実行
	if err := database.RunMigrations(db); err != nil {
		logger.Errorf("データベースマイグレーションに失敗しました: %v", err)
		os.Exit(1)
	}

	// ルーターを設定
	router := handler.NewRouter(db, cfg)

	// 初期管理者アカウントをブートストラップ（ユーザーが 0 件のときのみ作成）
	if err := router.AuthService().EnsureBootstrapAdmin(cfg.BootstrapAdminUser, cfg.BootstrapAdminPass); err != nil {
		logger.Errorf("初期管理者の作成に失敗しました: %v", err)
		os.Exit(1)
	}

	// 期限切れセッションを定期的に掃除
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		router.AuthService().PurgeExpiredSessions()
		for range ticker.C {
			router.AuthService().PurgeExpiredSessions()
		}
	}()

	// 自動バックアップ（設定で有効な場合、間隔ごとに pg_dump + Google Drive アップロード）
	router.BackupService().StartScheduler()

	// サーバーを起動
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	addr := net.JoinHostPort(host, port)
	logger.Infof("サーバーを起動します: http://%s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		logger.Errorf("サーバーの起動に失敗しました: %v", err)
		os.Exit(1)
	}
}
