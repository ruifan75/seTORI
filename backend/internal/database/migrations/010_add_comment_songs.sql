-- regex 解析後の重複排除前の楽曲一覧を保存する comment_songs 列を追加する
ALTER TABLE streams ADD COLUMN IF NOT EXISTS comment_songs JSONB;
