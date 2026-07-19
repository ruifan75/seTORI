-- アプリ設定の汎用 KV ストア（バックアップ設定・Google Drive トークンなどを JSONB で保存）
CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
