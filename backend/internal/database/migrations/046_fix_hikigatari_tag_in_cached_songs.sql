-- 解析キャッシュに残っている演奏バージョンタグ '弾き語り' を 'self-accompanied' に直す。
--
-- migration 019 で performance_tags.id を '弾き語り' → 'self-accompanied' に変えたが、
-- pkg/perftag の語彙は '弾き語り' のままだったので、その後の解析結果は
-- 存在しないタグ ID をキャッシュに書き続けていた。保存時に GetValidTagIDs が
-- 黙って捨てるため、このタグだけ歌唱に一度も付かない（画面には「タグが無い」
-- としか出ないので気付けない）。
--
-- 語彙側は同じ変更で直したが、既に書かれたキャッシュは再解析するまで残る。
-- 再解析は AI と yt-dlp を呼ぶ高い処理なので、ここで値だけ直す。
--
-- 触るのは tags 配列の中身だけ。comment_songs_hash / holodex_songs_hash は
-- 「どの入力から作ったか」を指すので据え置く（動かすと次回の解析が
-- キャッシュを外して AI を呼ぶ）。曲の順序は WITH ORDINALITY で保つ。

-- コメント経路のキャッシュ
UPDATE streams s
SET comment_songs = (
    SELECT jsonb_agg(
               CASE
                   WHEN e->'tags' @> '["弾き語り"]'::jsonb THEN jsonb_set(e, '{tags}', (
                       SELECT COALESCE(jsonb_agg(DISTINCT CASE WHEN t = '弾き語り' THEN 'self-accompanied' ELSE t END), '[]'::jsonb)
                       FROM jsonb_array_elements_text(e->'tags') AS t
                   ))
                   ELSE e
               END
               ORDER BY ord
           )
    FROM jsonb_array_elements(s.comment_songs) WITH ORDINALITY AS x(e, ord)
)
WHERE s.comment_songs @> '[{"tags": ["弾き語り"]}]'::jsonb;

-- Holodex 経路のキャッシュ（同じ構造の tags を持つ）
UPDATE streams s
SET holodex_songs_normalized = (
    SELECT jsonb_agg(
               CASE
                   WHEN e->'tags' @> '["弾き語り"]'::jsonb THEN jsonb_set(e, '{tags}', (
                       SELECT COALESCE(jsonb_agg(DISTINCT CASE WHEN t = '弾き語り' THEN 'self-accompanied' ELSE t END), '[]'::jsonb)
                       FROM jsonb_array_elements_text(e->'tags') AS t
                   ))
                   ELSE e
               END
               ORDER BY ord
           )
    FROM jsonb_array_elements(s.holodex_songs_normalized) WITH ORDINALITY AS x(e, ord)
)
WHERE s.holodex_songs_normalized @> '[{"tags": ["弾き語り"]}]'::jsonb;
