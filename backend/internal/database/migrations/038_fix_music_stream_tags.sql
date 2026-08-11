-- Holodex の音楽系 topic と seTORI のタグ・表示状態を揃える。
--
-- Holodex は Original_Song / Music_Cover を返すが、seTORI の安定 ID は
-- original_song / music_cover。topic_id をそのまま FK へ入れていたためタグが付かず、
-- is_hidden も「topic_id != singing」だけで先に決めていたため、タイトル規則で
-- ライブ / MV が付いた配信も hidden のまま残っていた。

-- 新規 DB でも音楽分類に必要なタグが必ず存在するようにする。
INSERT INTO stream_tags (id, display_name, color) VALUES
    ('music_cover', '歌ってみた', '#9932CC'),
    ('mv', 'MV', '#FF5722'),
    ('original_song', 'オリジナル曲', '#b38665')
ON CONFLICT (id) DO NOTHING;

-- 020 実行時に上のタグがまだ無く、WHERE EXISTS でスキップされた DB を補完する。
INSERT INTO tag_keyword_rules (tag_id, keyword) VALUES
    ('music_cover', '歌ってみた'),
    ('music_cover', '歌みた'),
    ('mv', 'MV'),
    ('mv', 'ミュージックビデオ'),
    ('original_song', 'オリジナル曲'),
    ('original_song', 'オリジナルソング'),
    ('original_song', 'オリ曲')
ON CONFLICT (tag_id, keyword) DO NOTHING;

-- 既存データにも Holodex topic の別表記を backfill する。
INSERT INTO stream_stream_tags (stream_id, tag_id)
SELECT s.id, m.tag_id
FROM streams s
JOIN (VALUES
    ('concert', 'concert'),
    ('karaoke', 'karaoke'),
    ('live', 'concert'),
    ('music_cover', 'music_cover'),
    ('music_video', 'mv'),
    ('mv', 'mv'),
    ('original_song', 'original_song'),
    ('singing', 'singing')
) AS m(topic_id, tag_id)
  ON LOWER(BTRIM(COALESCE(s.holodex_data->>'topic_id', ''))) = m.topic_id
ON CONFLICT (stream_id, tag_id) DO NOTHING;

-- タイトル規則は追加のみ・冪等。既に同期済みだがタグだけ欠けた配信もここで揃える。
INSERT INTO stream_stream_tags (stream_id, tag_id)
SELECT s.id, r.tag_id
FROM streams s
JOIN tag_keyword_rules r ON s.title ILIKE '%' || r.keyword || '%'
ON CONFLICT (stream_id, tag_id) DO NOTHING;

-- ライブ、カラオケ、歌ってみた、MV、オリジナル曲、歌枠のいずれかなら一覧へ出す。
UPDATE streams s
SET is_hidden = FALSE, updated_at = NOW()
WHERE s.is_hidden = TRUE
  AND EXISTS (
      SELECT 1
      FROM stream_stream_tags sst
      WHERE sst.stream_id = s.id
        AND sst.tag_id IN ('concert', 'karaoke', 'music_cover', 'mv', 'original_song', 'singing')
  );
