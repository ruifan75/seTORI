-- 中身を公開してよいか決まっていない配信に立てる旗。
--
-- **`is_hidden` とは別の軸にする。** `is_hidden` は 3 つの意味を兼ねている：
--   1. 音楽ではない（雑談・ゲーム・ラジオ。約 700 本）── 秘匿の必要なし
--   2. 誤判定（本当は歌枠）              ── 見つけたら表に出したい
--   3. 会限（歌はあるが公開可否が未確認）── 秘匿が要る
-- 3 のために 1 と 2 まで秘匿すると、「一覧から外すだけ」という既存の設計
--（singers.is_hidden と同じ思想）が壊れる。issue #4。
--
-- **秘匿するのは中身（歌唱記録・解析結果）だけで、配信そのものは隠さない。**
-- タイトルは YouTube 側でも会限バッジ付きで公開されているので秘密ではない。
-- 伏せたいのは「こちらが作ったセットリスト」であって、配信の存在ではない。
ALTER TABLE streams
    ADD COLUMN IF NOT EXISTS is_restricted BOOLEAN NOT NULL DEFAULT FALSE;

-- 既存データの初期化は**過剰に倒す**。取りこぼすと公開してしまうが、
-- 余分に倒しても編集者が外せばよい。
--
-- 材料は 2 つ。
--   1. availability = 'subscriber_only' … yt-dlp の積極的な判定（migration 051 / issue #3）
--   2. Holodex の topic_id = 'membersonly' … 候補抽出用。単値なので singing 等と排他になり
--      取りこぼしがあるが、**倒す方向にしか使わない**ので害はない
--
-- 本番はまだ availability の backfill を回していないので、実質は 2 が効く。
UPDATE streams SET is_restricted = TRUE
WHERE availability = 'subscriber_only'
   OR holodex_data->>'topic_id' = 'membersonly';

-- 秘匿の絞り込みは歌唱の読み取りごとに入るので、索引を付けておく。
CREATE INDEX IF NOT EXISTS idx_streams_restricted ON streams (is_restricted) WHERE is_restricted;
