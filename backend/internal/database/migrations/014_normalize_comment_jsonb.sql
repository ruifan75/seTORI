-- 014_normalize_comment_jsonb.sql
-- 古いデータにあり得る [null]、null、空配列形式の comment_songs / comment_raw を修正する
-- これらの値は lib/pq との往復で "invalid input syntax for type json" を起こしやすい
-- 古いデータと新しいデータの挙動を揃えるため、空値は [] に統一する

-- comment_songs を対象にする
UPDATE streams
SET comment_songs = '[]'::jsonb
WHERE 
    comment_songs IS NULL
    OR jsonb_typeof(comment_songs) = 'null'
    OR (jsonb_typeof(comment_songs) = 'array' AND comment_songs @> 'null'::jsonb)
    OR comment_songs::text ~ '^\s*\[\s*null\s*\]\s*$'
    OR comment_songs::text = '[null]';

-- comment_raw も対象にする（頻度は低いが安全のため）
UPDATE streams
SET comment_raw = '[]'::jsonb
WHERE 
    comment_raw IS NULL
    OR jsonb_typeof(comment_raw) = 'null'
    OR (jsonb_typeof(comment_raw) = 'array' AND comment_raw @> 'null'::jsonb)
    OR comment_raw::text ~ '^\s*\[\s*null\s*\]\s*$'
    OR comment_raw::text = '[null]';

-- holodex_data では少ない問題だが、併せて処理する
UPDATE streams
SET holodex_data = '{}'::jsonb
WHERE 
    holodex_data IS NULL
    OR jsonb_typeof(holodex_data) = 'null'
    OR holodex_data::text = 'null'
    OR holodex_data::text = '[null]';
