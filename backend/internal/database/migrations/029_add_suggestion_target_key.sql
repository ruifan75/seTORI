-- 「この曲が抜けている」（kind = 'perf.missing'）の提案を載せるための拡張。
--
-- これまでの提案は既存レコード（曲・アーティスト・歌唱）の修正だったので、対象は必ず UUID だった。
-- 未登録曲の報告は「どの配信のどの時点に曲がある」という指摘で、対象は配信になる。
-- 配信の主キーは YouTube の動画 ID（文字列）で UUID に収まらないため、
-- 文字列キー用の列を足す。
--
-- target_key … UUID で表せない対象の識別子（現状は配信の YouTube 動画 ID）。
--              UUID 対象では空文字。対象の同一性は (target_type, target_id, target_key) で見る。

ALTER TABLE edit_suggestions
    ADD COLUMN IF NOT EXISTS target_key VARCHAR(64) NOT NULL DEFAULT '';

-- 集約表示は (種別, UUID, 文字列キー) 単位で引く
DROP INDEX IF EXISTS idx_edit_suggestions_target;
CREATE INDEX IF NOT EXISTS idx_edit_suggestions_target
    ON edit_suggestions(target_type, target_id, target_key, status);
