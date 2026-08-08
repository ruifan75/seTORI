-- 歌唱記録の終了時間について「どこから来た値か」と「人が確認したか」を記録する。
--
-- 背景（詳細は docs/DATA_COMPLETION.md）：
-- 終了時間はコメントに書かれないことが多く、大半が推定値である。供給源は
-- 人手 / Holodex / コメント明示 / live chat の拍手検出 / iTunes の再生時間 /
-- 次の曲の開始時間 / 240 秒の既定値、と確度がばらつく。
-- しかし performances は数値しか持たないため、保存された時点で由来が失われていた。
--
-- 今は「編集画面で保存された ＝ 人が確認した」という暗黙の規則で回っているが、
-- 配信を一括処理して performance を自動作成すると、この規則は黙って崩れる。
-- 自動生成された行と人手確認済みの行が見分けられなくなり、事後に遡ることもできない。
--
-- 由来と確認状態は直交する（「chat が推定した値を人が見て承認した」がありうる）ため
-- 2 列に分ける。1 列だと、確認済みにした瞬間に由来が失われる。

ALTER TABLE performances
    ADD COLUMN IF NOT EXISTS end_source VARCHAR(20) NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS end_confirmed BOOLEAN NOT NULL DEFAULT FALSE;

-- 取りうる値。増えるたびにここを更新すること。
-- unknown は「この列を導入する前に作られた行」を表す。
ALTER TABLE performances
    DROP CONSTRAINT IF EXISTS performances_end_source_check;
ALTER TABLE performances
    ADD CONSTRAINT performances_end_source_check
    CHECK (end_source IN ('manual', 'holodex', 'comment', 'chat', 'itunes', 'next_start', 'default', 'unknown'));

-- 既存行はすべて人手の編集画面を通っているので確認済みとして良い。
-- ただし由来までは遡れないので 'unknown' のままにし、
-- 「記録を始める前のデータ」であることが後から分かるようにしておく。
UPDATE performances SET end_confirmed = TRUE WHERE end_source = 'unknown';

-- 検証 UI が「まだ誰も確認していない、かつ確度の低い」行を引くための索引。
-- 想定クエリ: WHERE end_confirmed = FALSE AND end_source IN ('itunes','next_start','default')
CREATE INDEX IF NOT EXISTS idx_performances_end_unconfirmed
    ON performances (end_source)
    WHERE end_confirmed = FALSE;

COMMENT ON COLUMN performances.end_source IS
    '終了時間の由来。manual/holodex/comment/chat は確度が高く、itunes/next_start/default は推定。unknown は列の導入前に作られた行';
COMMENT ON COLUMN performances.end_confirmed IS
    '人が値を見て認めたか。由来とは独立（推定値を人が承認した場合は source=chat かつ confirmed=true になる）';
