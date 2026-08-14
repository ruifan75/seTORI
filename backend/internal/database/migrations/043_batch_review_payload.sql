-- 一括セットリスト作成の審査待ち（perf.missing 提案）を引くための索引。
--
-- 同じ行の提案が何度でも積まれていた。createMissingSong に重複の判定が無く、
-- 投稿の絞り込みも System で飛ばしているので、一括を 2 回回すと同じ
-- (配信, 開始秒, 曲名) が 2 件並び、却下したものも次の実行でまた出ていた。
--
-- 判定は「同じ配信の perf.missing を引いて、開始秒と曲名キーで突き合わせる」。
-- 配信あたりの件数はせいぜい数十なので、配信で絞れれば十分速い。
-- 却下済みも引く（＝却下を覚える先がここになる）ので status は条件に入れない。
CREATE INDEX IF NOT EXISTS idx_edit_suggestions_missing_by_stream
    ON edit_suggestions (target_key)
    WHERE kind = 'perf.missing';
