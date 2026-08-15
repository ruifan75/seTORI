-- AI プロバイダーに timeout_seconds 列を追加し、OpenRouter などでは長めのタイムアウトを設定できるようにする
-- 既定は 60 秒。OpenRouter など遅いプロバイダーでは 90〜180 秒へ延長できる
ALTER TABLE ai_providers
ADD COLUMN IF NOT EXISTS timeout_seconds INTEGER NOT NULL DEFAULT 60;

-- 単なる設定値なので追加のインデックスは不要
