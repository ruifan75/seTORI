-- 「DB にあるが源に無い」歌唱の記録。
--
-- force 実行は源（Holodex のセットリスト / コメント）と既存の歌唱を突き合わせる。
-- そのとき 4 通りが出るが、行き先があったのは 3 つだけだった：
--
--   源にあり DB に無い       → 審査へ（perf.missing 提案）
--   両方にあり内容が違う     → 審査へ（conflict）
--   両方にあり一致           → 何もしない
--   **DB にあり源に無い**    → 数えてログに出すだけで、画面から見えなかった
--
-- 最後のものを提案として積まないのは今も同じ。源は欠けているのが普通で、欠落 1 件ごとに
-- 待ち行列を作ると人が処理できない量になり、しかも「源に無い」だけでは何をすべきか
-- 決まらない（消すべきとは限らない）。かといってログだけでは誰も気付けないので、
-- 実行の付随情報として残し、実行履歴から辿れるようにする。
--
-- 歌唱が消えれば記録も消える（CASCADE）。この行は「その歌唱についての観察」なので、
-- 対象が無くなったら残す意味が無い。
CREATE TABLE IF NOT EXISTS batch_fill_gaps (
    run_id         UUID NOT NULL REFERENCES batch_fill_runs(id) ON DELETE CASCADE,
    stream_id      VARCHAR(64) NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    performance_id UUID NOT NULL REFERENCES performances(id) ON DELETE CASCADE,
    PRIMARY KEY (run_id, performance_id)
);

CREATE INDEX IF NOT EXISTS idx_batch_fill_gaps_run ON batch_fill_gaps (run_id);

-- 実行履歴の行に件数を出すための集計値（一覧のたびに数え直さない）。
ALTER TABLE batch_fill_runs ADD COLUMN IF NOT EXISTS songs_gap INTEGER NOT NULL DEFAULT 0;

COMMENT ON TABLE batch_fill_gaps IS 'force 実行で「DB にあるが源に無い」と分かった歌唱';
COMMENT ON COLUMN batch_fill_runs.songs_gap IS '源に無かった既存の歌唱の件数';
