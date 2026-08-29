-- 会限セットリストを公開してよいかは、**チャンネル単位の判断**。
--
-- 配信主に「会限の歌単を公開してよいか」と訊くと、答えはほぼ「全部いい」か「全部だめ」の
-- どちらかになる。配信ごとではない。ところが migration 052/053 の
-- restriction_override は配信単位なので、1 チャンネルに会限が 60 本あれば
-- 60 回チェックを外すことになる ── 運用が成り立たない。
--
--   NULL    … まだ訊いていない。**伏せる**（既定。訊く前に公開しない）
--   'allow' … そのチャンネルの会限は公開してよい
--   'deny'  … 公開しない（明示。NULL と実効は同じだが「訊いた結果」が残る）
--
-- **NULL と 'deny' を分けるのは、訊いたかどうかが後から分かるようにするため。**
-- 実効値は同じだが、「まだ訊いていない 40 チャンネル」を一覧したいときに要る。
-- organizations.is_unaffiliated が「情報が無い」と「無所属と明示」を分けたのと同じ。
ALTER TABLE singers
    ADD COLUMN IF NOT EXISTS members_only_policy TEXT
        CHECK (members_only_policy IN ('allow', 'deny'));

COMMENT ON COLUMN singers.members_only_policy IS
    '会限セットリストの公開可否（チャンネル単位）。NULL＝未確認で伏せる';
