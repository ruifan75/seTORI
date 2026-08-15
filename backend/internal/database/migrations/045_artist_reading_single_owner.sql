-- アーティストの読みの持ち主を artists に一本化する。
--
-- 読みは artists.name_reading が正で、songs.original_artist_reading はその写し
-- （表示・並び替えのために楽曲側にも持っている）。ところが今まで楽曲側でも
-- 編集でき、書いても artists には伝わらなかった。結果、本番 820 行のうち
-- 158 行が食い違っていた。
--
--   artist 空・song 有  128 行 … 楽曲側でだけ入力されたもの
--   両方あって値が違う   19 行 … artist 側が正しく、song 側は誤読や
--                                 歌った人の名前（`まこうりさ・ゆめかわかなう・きばすう`
--                                 のような）が紛れ込んでいる
--   artist 有・song 空    11 行 … 写し漏れ
--
-- ここで artists 側へ寄せ切る。以後 songs.original_artist_reading を書くのは
-- SyncSongArtist と UpdateReadingPropagate（どちらも artists から写す向き）だけ。

-- 1. アーティストに読みが無いものだけ、楽曲側の読みを種として引き上げる。
--    複数の楽曲で値が割れていたら最も多いものを採る（同数なら文字列順で固定）。
--    漢字を含む読みは「読みとして未整備」なので種にしない（ContainsHan と同じ判定）。
WITH seed AS (
    SELECT sa.artist_id,
           s.original_artist_reading AS reading,
           COUNT(*) AS n
    FROM songs s
    JOIN song_artists sa ON sa.song_id = s.id
    WHERE COALESCE(s.original_artist_reading, '') <> ''
      AND s.original_artist_reading !~ '[一-龯]'
    GROUP BY sa.artist_id, s.original_artist_reading
),
picked AS (
    SELECT DISTINCT ON (artist_id) artist_id, reading
    FROM seed
    ORDER BY artist_id, n DESC, reading ASC
)
UPDATE artists a
SET name_reading = picked.reading, updated_at = NOW()
FROM picked
WHERE a.id = picked.artist_id
  AND COALESCE(a.name_reading, '') = '';

-- 2. 全楽曲の表示用の読みをアーティスト側に合わせる。
--    1 で引き上げた分はそのまま、食い違っていた分は artists 側が勝つ。
UPDATE songs s
SET original_artist_reading = a.name_reading, updated_at = NOW()
FROM song_artists sa
JOIN artists a ON a.id = sa.artist_id
WHERE sa.song_id = s.id
  AND COALESCE(s.original_artist_reading, '') IS DISTINCT FROM COALESCE(a.name_reading, '');
