-- AI provider 增加 timeout_seconds 欄位，讓不同 provider（例如 OpenRouter）可以設定較長的超時時間
-- 預設 60 秒，OpenRouter 等較慢的 provider 可調高（90~180）
ALTER TABLE ai_providers
ADD COLUMN IF NOT EXISTS timeout_seconds INTEGER NOT NULL DEFAULT 60;

-- 索引不需要特別加，單純設定值
