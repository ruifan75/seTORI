package database

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/logger"
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
		return fmt.Errorf("マイグレーションテーブル作成失敗: %w", err)
	}

	// マイグレーションファイルを読み込み
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("マイグレーションディレクトリ読み込み失敗: %w", err)
	}

	// ファイル名順でマイグレーションを実行（ファイル名は 001_*, 002_* などであるべき）
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := entry.Name()

		// 実行済みか確認する
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("マイグレーション状態確認失敗: %w", err)
		}

		if exists {
			logger.Infof("✓ マイグレーション実行済み: %s", version)
			continue
		}

		// マイグレーションファイルを読み込み
		content, err := migrationFS.ReadFile(fmt.Sprintf("migrations/%s", version))
		if err != nil {
			return fmt.Errorf("マイグレーションファイル %s の読み込み失敗: %w", version, err)
		}

		// マイグレーションを実行する
		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("マイグレーション %s の実行失敗: %w", version, err)
		}

		// マイグレーションを記録する
		if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			return fmt.Errorf("マイグレーション %s の記録失敗: %w", version, err)
		}

		fmt.Printf("✓ マイグレーション実行済み: %s\n", version)
	}

	return nil
}
