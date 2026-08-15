-- song_itunes の is_primary フラグを修正する
-- 目的：各楽曲に Primary iTunes ID が一つ以上あることを保証する
-- 規則：
--   - iTunes ID が一つだけなら Primary にする
--   - 複数ある場合は最初に作成されたものを Primary、その他を False にする

-- 最初に既存の Primary をすべて False に戻す
UPDATE song_itunes SET is_primary = FALSE;

-- 各楽曲で最初の iTunes レコードを Primary にする
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

-- 検証：各楽曲に Primary iTunes ID が一つ以上あることを確認する
-- 任意：このクエリを実行して結果を確認する
-- SELECT song_id, COUNT(*) as total_count, SUM(CASE WHEN is_primary THEN 1 ELSE 0 END) as primary_count
-- FROM song_itunes
-- GROUP BY song_id
-- HAVING SUM(CASE WHEN is_primary THEN 1 ELSE 0 END) != 1
-- ORDER BY song_id;
