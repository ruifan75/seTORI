package database

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"
)

// マイグレーションファイルを埋め込み
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// RunMigrations 待機中のすべてのマイグレーションを実行
func RunMigrations(db *sql.DB) error {
	// マイグレーション追跡テーブルを作成
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(50) PRIMARY KEY,
			executed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);
	`
	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("建立遷移表失敗: %w", err)
	}

	// マイグレーションファイルを読み込み
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("讀取遷移目錄失敗: %w", err)
	}

	// ファイル名順でマイグレーションを実行（ファイル名は 001_*, 002_* などであるべき）
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := entry.Name()

		// 檢查是否已執行
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("檢查遷移狀態失敗: %w", err)
		}

		if exists {
			fmt.Printf("✓ 遷移已執行: %s\n", version)
			continue
		}

		// マイグレーションファイルを読み込み
		content, err := migrationFS.ReadFile(fmt.Sprintf("migrations/%s", version))
		if err != nil {
			return fmt.Errorf("讀取遷移文件 %s 失敗: %w", version, err)
		}

		// 執行遷移
		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("執行遷移 %s 失敗: %w", version, err)
		}

		// 記錄遷移
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			return fmt.Errorf("記錄遷移 %s 失敗: %w", version, err)
		}

		fmt.Printf("✓ 遷移已執行: %s\n", version)
	}

	return nil
}
