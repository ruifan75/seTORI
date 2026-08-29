-- 会限かどうかの判定を streams.is_restricted から members_only タグへ移す（issue #32）。
--
-- **なぜ移すか。** 会限を確実に判定する方法が無い：
--
--   yt-dlp の availability … 本番の会限 86 本のうち 6 本が public を返す（7% の取りこぼし）。
--                            さらに --ignore-no-formats-error 付きでは視聴不可でも public
--   Holodex の topic_id   … 単値なので singing と membersonly が排他（実測 409 / 85）
--   タイトル規則           … 「メン限」と書かない会限がある（LHIsmaRxLOU など）
--
-- つまり**人が補強するしかない**。そして人が「これは会限だ」と言う場所はタグで、
-- 初回同期の自動判定も元からタグを読んでいた。にもかかわらず、後から付けたタグには
-- 何の効果も無かった（実測：タグを付けても未ログインで歌唱 25 件が見えたまま）。
--
-- **restriction_override は残す。** 「タグを外す」＝「これは会限ではない」（事実の訂正）と、
-- 「override=false」＝「会限だが配信主が公開してよいと言った」（許可）は別の陳述。
-- 潰すと、公開許可済みの会限が「会限ではない」ように見える
-- （organizations.is_unaffiliated で NULL と Independents を分けたのと同じ理由）。

-- ==========================================
-- 1. 既存の判定をタグへ写す
-- ==========================================
-- ここを飛ばすと、列を落とした瞬間に本番の会限 86 本が公開側へ倒れる。
INSERT INTO stream_stream_tags (stream_id, tag_id)
SELECT id, 'members_only' FROM streams WHERE is_restricted
ON CONFLICT (stream_id, tag_id) DO NOTHING;

-- ==========================================
-- 2. 列を落とす
-- ==========================================
-- 情報は 1 で全部タグへ移っているので失われない（タグ ⊇ 旧 is_restricted）。
-- 残しておくと「どちらが本物か」が分からなくなり、片方だけ更新する経路が生える。
DROP INDEX IF EXISTS idx_streams_restricted;
ALTER TABLE streams DROP COLUMN IF EXISTS is_restricted;

-- 検出はタグの EXISTS で引くので、stream_id 側から members_only を探せるようにする。
-- 主キーは (stream_id, tag_id) なので前方一致で使えるが、タグ側から全件を引く
-- （「会限を何本持つチャンネルか」）経路のために tag_id 始まりも要る。
CREATE INDEX IF NOT EXISTS idx_stream_tags_by_tag
    ON stream_stream_tags (tag_id, stream_id);
