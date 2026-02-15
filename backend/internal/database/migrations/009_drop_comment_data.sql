-- 009_drop_comment_data.sql
-- 移除 comment_data 欄位，改為即時分析不快取

ALTER TABLE streams DROP COLUMN IF EXISTS comment_data;
DROP INDEX IF EXISTS idx_streams_comment_data;
