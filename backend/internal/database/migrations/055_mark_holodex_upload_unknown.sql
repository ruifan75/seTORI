-- 台帳（054）より前から存在する配信に「送信の有無が分からない」印を付ける。
--
-- **NULL を「送っていない」と読ませないための列。** 054 は nullable な時刻を足しただけなので、
-- 旧行は台帳が NULL のままになる。docs では「NULL＝分からない」と書いたが、
-- 画面がそれを何も出さずに扱うと、**警告が無いことを安全の根拠にできない**。
--
-- 2 つ目の信号（holodex_data に曲がある）でも旧行は拾えない。
-- SyncSetoriToHolodex は PUT の結果を holodex_data へ書き戻さないので、
-- 送信に成功しても保存済みのスナップショットは空のまま。
--
-- そこで「この行は追跡の開始より前から存在する」という事実そのものを持つ。
--   TRUE  … 追跡開始より前からある。過去に送信された可能性を否定できない
--   FALSE … 追跡開始後に作られた。台帳が NULL なら本当に送っていない
-- 新しい行は既定の FALSE で入るので、時間が経つほど TRUE は減っていく。
ALTER TABLE streams
    ADD COLUMN IF NOT EXISTS holodex_upload_unknown BOOLEAN NOT NULL DEFAULT FALSE;

-- 適用時点で存在する行が対象。以後の新規行には付かない。
UPDATE streams SET holodex_upload_unknown = TRUE WHERE holodex_uploaded_at IS NULL;
