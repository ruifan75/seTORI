-- AI プロバイダー設定（OpenAI 互換エンドポイント）。管理画面で複数プロバイダーを登録して順番に使う
CREATE TABLE IF NOT EXISTS ai_providers (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,           -- 表示名（Groq / Gemini / Cerebras など）
    base_url TEXT NOT NULL,               -- OpenAI 互換の base（例：https://api.groq.com/openai/v1）
    model VARCHAR(200) NOT NULL,          -- モデル名
    api_key TEXT NOT NULL,                -- API key（バックエンドだけで使い、フロントエンドには返さない）
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    priority INTEGER NOT NULL DEFAULT 0,  -- 数字が小さいものから試す
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_providers_enabled_priority ON ai_providers(enabled, priority);
