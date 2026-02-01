-- 006_add_comment_data.sql
-- 新增 comment_data 欄位來儲存 Comment 分析結果

ALTER TABLE streams ADD COLUMN IF NOT EXISTS comment_data JSONB;

-- 建立索引以加速查詢
CREATE INDEX IF NOT EXISTS idx_streams_comment_data ON streams USING GIN (comment_data);
