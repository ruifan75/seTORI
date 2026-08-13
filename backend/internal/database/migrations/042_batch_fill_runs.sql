-- 一括でセットリストを埋めた記録。
--
-- 一括は performances（主データ）に直接書く。書く以上、後から**その回の分だけ**
-- 戻せる必要がある。これが無いと、誤った実行の後始末が「数千件から手で探す」になる。
--
-- 一括プレ分析（batch-analyze）とは別物なので混同しないこと。あちらは
-- comment_raw → comment_songs の抽出だけで、何度回しても主データは変わらない。
CREATE TABLE IF NOT EXISTS batch_fill_runs (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    mode         VARCHAR(32)  NOT NULL,          -- unprocessed / force
    singer_id    VARCHAR(64),                    -- 範囲を絞った場合のチャンネル
    started_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    status       VARCHAR(16)  NOT NULL DEFAULT 'running', -- running / done / cancelled / failed / reverted
    streams_total    INTEGER  NOT NULL DEFAULT 0,
    streams_done     INTEGER  NOT NULL DEFAULT 0,
    songs_created    INTEGER  NOT NULL DEFAULT 0, -- 自動で作った歌唱
    songs_review     INTEGER  NOT NULL DEFAULT 0, -- 人の審査へ回した曲
    ai_asked         INTEGER  NOT NULL DEFAULT 0, -- AI に問い合わせた行
    message      TEXT NOT NULL DEFAULT '',
    started_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    finished_at  TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_batch_fill_runs_started ON batch_fill_runs (started_at DESC);

-- 歌唱がどう作られたかを残す。
--
-- batch_run_id … どの実行が作ったか。撤回はこの列で引く
-- created_via  … rule（規則で照合）/ ai（AI が照合）/ NULL（人が作った）
-- match_confidence … AI が決めた場合の確信度。低いものを後から見直せる
ALTER TABLE performances ADD COLUMN IF NOT EXISTS batch_run_id UUID
    REFERENCES batch_fill_runs(id) ON DELETE SET NULL;
ALTER TABLE performances ADD COLUMN IF NOT EXISTS created_via VARCHAR(16);
ALTER TABLE performances ADD COLUMN IF NOT EXISTS match_confidence REAL;

CREATE INDEX IF NOT EXISTS idx_performances_batch_run ON performances (batch_run_id)
    WHERE batch_run_id IS NOT NULL;

COMMENT ON COLUMN performances.batch_run_id IS '一括で作られた場合の実行 ID。撤回はこの列で引く';
COMMENT ON COLUMN performances.created_via IS 'rule / ai / NULL（人の手）';
