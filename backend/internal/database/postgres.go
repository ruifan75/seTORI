package database

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// Connect 連接到 PostgreSQL 資料庫
func Connect(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("データベース接続のオープンに失敗: %w", err)
	}

	// 測試連接
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("データベースに接続できません: %w", err)
	}

	// 設定連接池
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	return db, nil
}
