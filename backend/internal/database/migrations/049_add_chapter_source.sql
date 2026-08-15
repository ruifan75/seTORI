-- 049_add_chapter_source.sql
-- 配信者が付けた YouTube チャプターを、コメント・Holodex に次ぐ 3 つ目の入力元にする。
--
-- 列の形はコメント経路をそのまま写している（源 / 抽出キャッシュ / 由来の hash / 解析時刻）。
-- 揃えているのは趣味ではなく、キャッシュの効き方と「照合は保存しない」という約束を
-- 経路ごとに考え直さずに済ませるため。
--
--   chapter_raw               … yt-dlp が返したチャプター配列そのまま（[{start,end,title}]）
--   chapter_songs             … 抽出＋正規化＋拍手 end まで済ませた結果（照合は含まない）
--   chapter_songs_hash        … chapter_songs の計算元 chapter_raw の sha256
--   chapter_songs_analyzed_at … 抽出を最後に走らせた時刻（updated_at では代用できない）

ALTER TABLE streams ADD COLUMN IF NOT EXISTS chapter_raw JSONB;
ALTER TABLE streams ADD COLUMN IF NOT EXISTS chapter_songs JSONB;
ALTER TABLE streams ADD COLUMN IF NOT EXISTS chapter_songs_hash TEXT;
ALTER TABLE streams ADD COLUMN IF NOT EXISTS chapter_songs_analyzed_at TIMESTAMP WITH TIME ZONE;
