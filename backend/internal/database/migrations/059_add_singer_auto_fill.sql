-- チャンネル単位の「自動処理」の旗（issue #35）。
--
-- 立てたチャンネルは、**将来**の自動処理の対象になる：定期的に Holodex から
-- 同期し、まだ歌単の無い配信のコメントを解析して、確信があれば歌唱を作り、
-- 無ければ審査へ回す。**is_processed（最後の確認）は自動では付かない**。
--
-- **定期実行器はこの migration の時点ではまだ無い**（issue #35 の ③）。
-- ここで足すのは旗だけで、読むのは一覧 API のみ。
--
-- **既定は FALSE（オプトイン）。** `singers.is_hidden` の既定を取り違えたときは
-- 本番 148 件中 147 件を手で直すことになった（CLAUDE.md §3）。自動で外部 API と
-- AI を呼ぶ旗なので、なおさら黙って有効にしない。
ALTER TABLE singers
    ADD COLUMN IF NOT EXISTS auto_fill_enabled BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN singers.auto_fill_enabled IS
    '自動処理の対象か（定期同期＋コメント解析＋歌単作成）。既定 FALSE のオプトイン';

-- 対象は普通ごく少数なので、有効な行だけの部分索引で足りる。
CREATE INDEX IF NOT EXISTS idx_singers_auto_fill
    ON singers (id) WHERE auto_fill_enabled;
