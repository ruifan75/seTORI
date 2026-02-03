-- 008_add_comment_raw.sql
-- 新增 comment_raw 欄位來儲存原始 Comment 清單

ALTER TABLE streams ADD COLUMN IF NOT EXISTS comment_raw JSONB;

-- 建立索引以加速查詢（若需要全文檢索可改用 GIN）
CREATE INDEX IF NOT EXISTS idx_streams_comment_raw ON streams USING GIN (comment_raw);
