-- 一括セットリスト作成で「入力元を確定できずに飛ばした配信」を実行履歴に残す（issue #7）。
--
-- 飛ばした配信はメモリ上の状態（GET /api/streams/batch-fill/status）にしか無く、
-- **実行中か直後に API を直接見たときだけ**見えた。プロセスの再起動か次の実行で消え、
-- 履歴には残らなかった。しかも対象 1 件が飛ばされただけの実行は、第 3 段の
-- 進捗保存が一度も呼ばれないので `streams_total = 0, status = done` になっていた
-- ── 何もしなかった実行と見分けがつかない。
--
-- 件数ではなく ID を持つ。件数だけでは「どれを手で見ればよいか」が辿れない。
-- 件数は配列の長さで足りる。
ALTER TABLE batch_fill_runs
    ADD COLUMN IF NOT EXISTS skipped_stream_ids TEXT[] NOT NULL DEFAULT '{}';
