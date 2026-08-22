-- 配信が実際に再生できるかを yt-dlp から拾って持つ。
--
-- 会限（メンバー限定）配信は YouTube が埋め込みを塞いでいるため、IFrame API が
-- onError: 150 を返して黒い枠だけが残る。YouTube Data API の part=status は
-- 会限と公開で完全に同一の応答（どちらも embeddable: true）を返すので判別できない。
-- yt-dlp の availability / playable_in_embed だけが構造化された答えを持つ。
--
-- 3 列に分けてあるのは、それぞれ別の事実だから：
--   availability            … 生の値（public / subscriber_only / unlisted / premium_only …）
--   playable_in_embed       … 埋め込み再生の可否。公開でも所有者が埋め込みを切っていると false
--   availability_checked_at … いつ調べたか。NULL＝未調査で、「調べたが公開だった」と区別する
--
-- **NULL と値ありを同一視しないこと。** 未調査を「公開」とみなすと調べる導線が消え、
-- 逆に「再生不可」とみなすと全配信のプレイヤーが消える。
ALTER TABLE streams
    ADD COLUMN IF NOT EXISTS availability TEXT,
    ADD COLUMN IF NOT EXISTS playable_in_embed BOOLEAN,
    ADD COLUMN IF NOT EXISTS availability_checked_at TIMESTAMPTZ;

-- backfill の対象を引くための索引（未調査＝checked_at IS NULL を拾う）。
CREATE INDEX IF NOT EXISTS idx_streams_availability_checked
    ON streams (availability_checked_at NULLS FIRST);
