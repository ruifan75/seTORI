package main

import (
	"log"
	"net/http"
	"os"

	"github.com/ruifan75/setori/internal/config"
	"github.com/ruifan75/setori/internal/database"
	"github.com/ruifan75/setori/internal/handler"
	"github.com/joho/godotenv"
)

func main() {
	// 載入 .env 檔案
	if err := godotenv.Load(); err != nil {
		log.Printf("警告: 無法載入 .env 檔案: %v", err)
	}

	// 載入設定
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("設定載入失敗: %v", err)
	}

	// 連接資料庫
	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("資料庫連接失敗: %v", err)
	}
	defer db.Close()

	// 設定路由
	router := handler.NewRouter(db, cfg)

	// 啟動伺服器
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("伺服器啟動於 http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatalf("伺服器啟動失敗: %v", err)
	}
}
