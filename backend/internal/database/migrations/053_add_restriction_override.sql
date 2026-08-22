-- 秘匿の「自動判定」と「人の裁定」を分ける。
--
-- 052 は 1 列だったので、次の順で人の判断が消えていた：
--   1. availability が subscriber_only を返して is_restricted = true
--   2. 編集者が「公開してよい」と判断して false にする
--   3. チャプター取得・live chat・availability backfill のどれかが同じ動画を取り直す
--   4. SaveAvailability の `is_restricted OR subscriber_only` で **true に戻る**
-- 会限配信は 3 が繰り返し起きるので、解除は事実上維持できなかった。
--
-- 形は organization / organization_override と同じ（CLAUDE.md §3.5）。
-- 外部・自動が書く列と、人が書く列を分け、読むときに COALESCE する。
--   is_restricted        … 自動判定の候補（Holodex topic / members_only タグ / availability）
--   restriction_override … 人の裁定。NULL＝未裁定、TRUE＝伏せる、FALSE＝公開してよい
--   実効値 = COALESCE(restriction_override, is_restricted)
--
-- **自動判定の側を凍結しない**のは、後から会限化した配信を検出し続けたいため
-- （singers.is_hidden のように凍結すると、新しい事実を受け取れなくなる）。
ALTER TABLE streams
    ADD COLUMN IF NOT EXISTS restriction_override BOOLEAN;

-- 候補の取りこぼしを埋める。**倒す方向にしか使わない**ので過剰でよい。
--
-- 052 は availability と Holodex topic だけを見ていたが、seTORI 側には既に
-- members_only の配信タグがあり（タイトル規則の「メン限」「メンバー限定」
-- 「メンバーシップ」で付く）、FindRandom とプリセットはこのタグを除外している。
-- topic が singing で availability 未取得の会限はこのタグでしか拾えない。
UPDATE streams SET is_restricted = TRUE
WHERE NOT is_restricted
  AND id IN (SELECT stream_id FROM stream_stream_tags WHERE tag_id = 'members_only');
