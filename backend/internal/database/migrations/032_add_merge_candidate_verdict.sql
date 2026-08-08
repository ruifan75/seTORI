-- 統合候補に「AI の見立て」と「どこから来た候補か」を持たせる。
--
-- 背景：同じ曲名で複数の楽曲がある状態には、少なくとも 3 通りの正解がある。
--
--   惑星ループ（Eve / ナユタン星人）      … 同じ曲。artist 欄が原唱と作曲のどちらを
--                                          記録したかの違いでしかない → 統合する
--   翼をください（赤い鳥 / 桜高軽音部）    … 同じ作曲だが編曲が大きく違う。
--                                          どちらを歌ったかが情報になる → 分けたまま
--   オレンジ（SPYAIR / 逢坂大河ほか）      … そもそも別の曲 → 分けたまま
--
-- 「どれだけ違えば分けるべきか」は編集方針であってデータから導けない。
-- 一方で「同じ作曲か」「編曲は同系統か」は公開された事実で、AI が答えられる。
-- そこで **AI には事実の判定だけをさせ、統合するかどうかは人が決める**。
-- 統合は破壊的（統合元が消え、歌唱が移る）なので、AI の判定で自動実行はしない。
--
-- 判定は候補行に残す。同じ組を二度 AI に聞かないため。
-- 却下（status='dismissed'）した組も行が残るので、再走査で蒸し返されない。

ALTER TABLE song_merge_candidates
    -- create … 取り込み時に新曲を作った際に気づいたもの
    -- scan   … 既存データの走査で見つかったもの（どちらが「新しい」でもない）
    ADD COLUMN IF NOT EXISTS origin           TEXT NOT NULL DEFAULT 'create',
    ADD COLUMN IF NOT EXISTS same_composition BOOLEAN,
    ADD COLUMN IF NOT EXISTS same_arrangement BOOLEAN,
    -- merge | keep_separate。あくまで助言で、実行はしない
    ADD COLUMN IF NOT EXISTS recommendation   TEXT,
    -- 各曲の立ち位置（「原曲（1971・フォーク）」「K-ON! 版・バンドアレンジ」など）。
    -- 判断の根拠が見えないと、AI が知らない曲で作った答えを見抜けない。
    ADD COLUMN IF NOT EXISTS role_new         TEXT,
    ADD COLUMN IF NOT EXISTS role_existing    TEXT,
    ADD COLUMN IF NOT EXISTS verdict_note     TEXT,
    ADD COLUMN IF NOT EXISTS verdict_source   TEXT,
    ADD COLUMN IF NOT EXISTS verdict_at       TIMESTAMP WITH TIME ZONE;

-- 未判定の候補を拾うため
CREATE INDEX IF NOT EXISTS idx_song_merge_candidates_unjudged
    ON song_merge_candidates(created_at) WHERE status = 'open' AND verdict_at IS NULL;
