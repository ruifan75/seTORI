-- 009_drop_comment_data.sql
-- comment_data 列を削除し、キャッシュを使わない都度解析に変更する

ALTER TABLE streams DROP COLUMN IF EXISTS comment_data;
DROP INDEX IF EXISTS idx_streams_comment_data;
