-- seTORI → Holodex へセットリストを送った時刻。
--
-- **外部に残る非正規化コピーの台帳。** Holodex への書き戻しは運用者の名義で
-- 向こうのデータベースへ曲名・開始/終了秒を書き込む（`PUT /api/v2/songs`）。
-- あとから配信が秘匿になっても、**seTORI 側を全部伏せても向こうのコピーは残る**。
-- seTORI から取り消す手段は無い（そもそも Holodex が公開している API ではない）。
--
-- 自動で撤回はしない。判断と実行は運用者に委ねるが、**送信済みであることを
-- 忘れないよう記録して画面に出す**。記録が無いと「何を外へ出したか」を
-- 後から辿れず、秘匿にした時点で気付く手がかりが消える。
ALTER TABLE streams
    ADD COLUMN IF NOT EXISTS holodex_uploaded_at TIMESTAMPTZ;
