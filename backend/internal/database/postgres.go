package database

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// Connect は PostgreSQL データベースへ接続する。
func Connect(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("データベース接続のオープンに失敗: %w", err)
	}

	// 接続を確認する
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("データベースに接続できません: %w", err)
	}

	// 接続プールを設定する
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	return db, nil
}
