package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// AppSettingsRepository は app_settings（汎用 KV、値は JSONB）へのアクセスを提供する。
type AppSettingsRepository struct {
	db *sql.DB
}

func NewAppSettingsRepository(db *sql.DB) *AppSettingsRepository {
	return &AppSettingsRepository{db: db}
}

// Get は key の値を dest（JSON アンマーシャル先）に読み込む。
// キーが存在しない場合は (false, nil) を返し dest は変更しない。
func (r *AppSettingsRepository) Get(key string, dest interface{}) (bool, error) {
	var raw []byte
	err := r.db.QueryRow(`SELECT value FROM app_settings WHERE key = $1`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get app setting %s: %w", key, err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return false, fmt.Errorf("unmarshal app setting %s: %w", key, err)
	}
	return true, nil
}

// Set は key の値を JSON で保存（upsert）する。
func (r *AppSettingsRepository) Set(key string, value interface{}) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal app setting %s: %w", key, err)
	}
	_, err = r.db.Exec(`
		INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		key, raw)
	if err != nil {
		return fmt.Errorf("set app setting %s: %w", key, err)
	}
	return nil
}

// Delete は key を削除する（存在しなくてもエラーにしない）。
func (r *AppSettingsRepository) Delete(key string) error {
	if _, err := r.db.Exec(`DELETE FROM app_settings WHERE key = $1`, key); err != nil {
		return fmt.Errorf("delete app setting %s: %w", key, err)
	}
	return nil
}
