-- 修正舊資料：將只有一個 iTunes ID 但 is_primary = false 的記錄設為 true

UPDATE song_itunes si1
SET is_primary = true
WHERE is_primary = false
  AND (
    SELECT COUNT(*) 
    FROM song_itunes si2 
    WHERE si2.song_id = si1.song_id
  ) = 1;

-- 確保每首歌至少有一個 primary iTunes ID（如果有多個但都不是 primary，將最早的設為 primary）
UPDATE song_itunes si1
SET is_primary = true
WHERE id IN (
    SELECT DISTINCT ON (song_id) id
    FROM song_itunes si2
    WHERE song_id IN (
        -- 找出有 iTunes ID 但沒有任何一個是 primary 的歌曲
        SELECT song_id
        FROM song_itunes
        GROUP BY song_id
        HAVING COUNT(*) > 0 AND SUM(CASE WHEN is_primary THEN 1 ELSE 0 END) = 0
    )
    ORDER BY song_id, created_at ASC
);
