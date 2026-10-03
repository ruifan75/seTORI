-- 背景処理の「実行 1 回」を残す（issue #22）。
--
-- yt-dlp を起動する backfill（拍手 end・チャプター）は goroutine を投げっぱなしで、
-- 進捗も失敗も log にしか出なかった。`GET /api/logs` はメモリ上の直近 1000 件なので、
-- 長い backfill は自分の進捗行で失敗行を押し流し、終わった頃には何が失敗したか
-- 残っていない。しかも DB を見ても「失敗」と「まだ処理していない」が区別できない
-- （一時的な失敗は再試行できるよう記録しない）。**最も失敗を見たいものが、最も見えなかった。**
--
-- 汎用の job framework にはしない（単一インスタンス・単一運用者）。要るのは
-- 「実行 1 回の記録」だけ。batch-fill は撤回まで持つ別表（batch_fill_runs）のまま。
CREATE TABLE IF NOT EXISTS task_runs (
    id          UUID PRIMARY KEY,
    kind        TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('running', 'done', 'failed', 'interrupted')),
    total       INTEGER NOT NULL DEFAULT 0,
    done        INTEGER NOT NULL DEFAULT 0,
    succeeded   INTEGER NOT NULL DEFAULT 0,
    skipped     INTEGER NOT NULL DEFAULT 0,
    failed      INTEGER NOT NULL DEFAULT 0,
    params      JSONB NOT NULL DEFAULT '{}',
    -- 失敗した対象と理由（直近の一部）。log と違って消えないので、
    -- 「cookie を直して再実行すべき対象」が後から引ける。
    failures    JSONB NOT NULL DEFAULT '[]',
    message     TEXT NOT NULL DEFAULT '',
    started_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_task_runs_started ON task_runs (started_at DESC);
