-- 分析結果のキャッシュ用カラム。
-- comment_songs / holodex 正規化結果を「元データの hash」と一緒に保存し、
-- 元データが変わっていなければ AI を再実行せず再利用する（トークン節約）。
ALTER TABLE streams ADD COLUMN IF NOT EXISTS comment_songs_hash TEXT;
ALTER TABLE streams ADD COLUMN IF NOT EXISTS holodex_songs_normalized JSONB;
ALTER TABLE streams ADD COLUMN IF NOT EXISTS holodex_songs_hash TEXT;
