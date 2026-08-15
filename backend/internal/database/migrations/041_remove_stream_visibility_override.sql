-- 039 で追加した 3 状態の表示 override は使わない。
-- 配信の自動判定は初回登録時だけ行い、その後は is_hidden を直接手動編集する。
-- 039 の時点で override を設定していた行も、実効値は既に is_hidden に反映済みなので、
-- 列を削除しても現在の表示状態は失われない。

ALTER TABLE streams
    DROP COLUMN IF EXISTS visibility_override;
