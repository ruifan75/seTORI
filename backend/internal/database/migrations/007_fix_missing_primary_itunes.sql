-- 古いデータを修正：iTunes ID が一つだけで is_primary = false のレコードを true にする

UPDATE song_itunes si1
SET is_primary = true
WHERE is_primary = false
  AND (
    SELECT COUNT(*) 
    FROM song_itunes si2 
    WHERE si2.song_id = si1.song_id
  ) = 1;

-- 各楽曲に primary iTunes ID が一つ以上あることを保証する（複数あるのに primary がなければ最初のものを primary にする）
UPDATE song_itunes si1
SET is_primary = true
WHERE id IN (
    SELECT DISTINCT ON (song_id) id
    FROM song_itunes si2
    WHERE song_id IN (
        -- iTunes ID はあるが primary が一つもない楽曲を探す
        SELECT song_id
        FROM song_itunes
        GROUP BY song_id
        HAVING COUNT(*) > 0 AND SUM(CASE WHEN is_primary THEN 1 ELSE 0 END) = 0
    )
    ORDER BY song_id, created_at ASC
);
