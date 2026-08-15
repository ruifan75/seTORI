-- 008_add_comment_raw.sql
-- 元のコメント一覧を保存する comment_raw 列を追加する

ALTER TABLE streams ADD COLUMN IF NOT EXISTS comment_raw JSONB;

-- 検索を高速化するインデックスを作成する（全文検索が必要なら GIN に変更可能）
CREATE INDEX IF NOT EXISTS idx_streams_comment_raw ON streams USING GIN (comment_raw);
