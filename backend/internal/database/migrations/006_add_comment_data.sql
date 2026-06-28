-- 006_add_comment_data.sql
-- comment_data カラムを追加してコメント解析結果を保存

ALTER TABLE streams ADD COLUMN IF NOT EXISTS comment_data JSONB;

-- クエリ高速化のためのインデックス作成
CREATE INDEX IF NOT EXISTS idx_streams_comment_data ON streams USING GIN (comment_data);
