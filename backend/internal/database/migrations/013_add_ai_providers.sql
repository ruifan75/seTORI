-- AI provider 設定（OpenAI 相容端點），供管理介面設定多個 provider 輪流使用
CREATE TABLE IF NOT EXISTS ai_providers (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,           -- 顯示名稱，如 Groq / Gemini / Cerebras
    base_url TEXT NOT NULL,               -- OpenAI 相容 base，如 https://api.groq.com/openai/v1
    model VARCHAR(200) NOT NULL,          -- 模型名稱
    api_key TEXT NOT NULL,                -- API key（僅後端使用，不回傳前端）
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    priority INTEGER NOT NULL DEFAULT 0,  -- 數字小者優先嘗試
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_providers_enabled_priority ON ai_providers(enabled, priority);
