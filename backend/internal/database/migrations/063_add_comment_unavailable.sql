-- コメントが取れないと YouTube が明言した配信を、取り直しで後回しにする（issue #56）。
--
-- 自動処理のコメント取り直し（FindStreamsNeedingCommentRefresh）は「歌単が空 ×
-- N 日以内」の配信を毎回取りに行く。配信者がコメント欄を閉じている配信では
-- YouTube が 404 を返し、Holodex も 0 件なので、**30 日の窓口が切れるまで毎時
-- 失敗し続けていた**（本番：3QjeckSIFTw）。データは壊れないが、外部呼び出しだけが残る。
--
--   comment_unavailable_at    … YouTube が最後に「取れない」と明言した時刻
--   comment_unavailable_count … 連続した回数（取れたら 0 に戻す）
--
-- **恒久とは決めない。** 配信者があとでコメント欄を開くことはあるので、
-- 間隔を空けて取り直すだけにする（1 日 → 2 日 → 4 日 → 以後 7 日おき）。
-- 30 日の窓口の中で、毎時 720 回だったものが 6 回ほどになる。
--
-- 一時的な失敗（5xx・クォータ超過・Holodex の障害）は数えない。数えると
-- 障害のあいだに触った配信が全部後回しになる（youtube.ErrCommentsUnavailable）。
ALTER TABLE streams
    ADD COLUMN IF NOT EXISTS comment_unavailable_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS comment_unavailable_count INTEGER NOT NULL DEFAULT 0;
