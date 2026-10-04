-- 人の裁定（restriction_override）を下したときの、自動判定の値を控える（issue #26）。
--
-- 裁定は「そのとき分かっていた事実」のもとで下される。配信者が後から会限へ
-- 変えると、同期が members_only タグを付けて自動判定は「伏せる」へ変わるが、
-- override が勝つので**公開のまま、何の知らせも無い**:
--
--   公開の歌枠 → 人が「公開してよい」（override = FALSE）
--             → 後日、配信者が会限へ変更 → 同期が members_only を付ける
--             → COALESCE(override, 自動判定) は override が勝つ → 公開のまま
--
-- 自動判定が人の裁定を上書きするのは誤り（裁定の意味が無くなる）。要るのは
-- **食い違いを見えるようにすること**で、そのために「裁定したとき自動判定は何と
-- 言っていたか」が要る。それが無いと、会限と分かったうえで「公開してよい」と
-- 決めたもの（正当な例外）まで毎回警告することになる。
--
--   restriction_override_auto … 裁定の時点の自動判定（members_only かつ
--                                所有者全員が allow ではない）。
--                                NULL＝分からない（この列より前の裁定・未裁定）
--
-- 書くのは裁定を書くときだけ（StreamRepository.SetRestrictionOverride）。
-- 題名の編集など、裁定に関係の無い更新で取り直すと、警告が理由なく消える。
ALTER TABLE streams
    ADD COLUMN IF NOT EXISTS restriction_override_auto BOOLEAN;

COMMENT ON COLUMN streams.restriction_override_auto IS
    '人が restriction_override を書いた時点の自動判定。NULL＝不明（この列より前の裁定）。食い違いの検出に使う（issue #26）';
