-- 修復 song_itunes 的 is_primary 標誌
-- 目的：確保每首歌曲至少有一個 Primary iTunes ID
-- 規則：
--   - 如果歌曲只有一個 iTunes ID，設為 Primary
--   - 如果歌曲有多個 iTunes ID，將最早創建的設為 Primary，其他設為 False

-- 首先，將所有已有的 Primary 設為 False（重置）
UPDATE song_itunes SET is_primary = FALSE;

-- 對於每首歌曲，設置最早的 iTunes 記錄為 Primary
UPDATE song_itunes si
SET is_primary = TRUE
WHERE id IN (
    SELECT id
    FROM song_itunes
    WHERE (song_id, created_at) IN (
        SELECT song_id, MIN(created_at)
        FROM song_itunes
        GROUP BY song_id
    )
);

-- 驗證：確認每首歌曲都有至少一個 Primary iTunes ID
-- 可選：運行此查詢以檢查結果
-- SELECT song_id, COUNT(*) as total_count, SUM(CASE WHEN is_primary THEN 1 ELSE 0 END) as primary_count
-- FROM song_itunes
-- GROUP BY song_id
-- HAVING SUM(CASE WHEN is_primary THEN 1 ELSE 0 END) != 1
-- ORDER BY song_id;
