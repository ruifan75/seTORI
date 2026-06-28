-- 014_normalize_comment_jsonb.sql
-- 修復舊資料中可能出現的 [null]、null 或空陣列形式的 comment_songs / comment_raw
-- 這些值在 lib/pq 往返時容易導致 "invalid input syntax for type json"
-- 我們希望舊資料與新資料行為一致（空值統一為 []）

-- 針對 comment_songs
UPDATE streams
SET comment_songs = '[]'::jsonb
WHERE 
    comment_songs IS NULL
    OR jsonb_typeof(comment_songs) = 'null'
    OR (jsonb_typeof(comment_songs) = 'array' AND comment_songs @> 'null'::jsonb)
    OR comment_songs::text ~ '^\s*\[\s*null\s*\]\s*$'
    OR comment_songs::text = '[null]';

-- 針對 comment_raw（雖然比較少見，但為了安全）
UPDATE streams
SET comment_raw = '[]'::jsonb
WHERE 
    comment_raw IS NULL
    OR jsonb_typeof(comment_raw) = 'null'
    OR (jsonb_typeof(comment_raw) = 'array' AND comment_raw @> 'null'::jsonb)
    OR comment_raw::text ~ '^\s*\[\s*null\s*\]\s*$'
    OR comment_raw::text = '[null]';

-- holodex_data 比較少有這種問題，但也一併處理
UPDATE streams
SET holodex_data = '{}'::jsonb
WHERE 
    holodex_data IS NULL
    OR jsonb_typeof(holodex_data) = 'null'
    OR holodex_data::text = 'null'
    OR holodex_data::text = '[null]';