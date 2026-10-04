-- 読み取り同期 (sync:run) と運用者名義の Holodex 書き込みを分ける。
-- 初期の付与先は system admin だけ。editor とカスタムロールの sync:run は
-- そのまま残し、新しい権限には変換しない。'*' の全権限という意味も変えない。
-- ロール編集は未知キーを許容するため、非 admin に先行保存された同名キーも除く。
UPDATE roles
SET permissions = array_remove(permissions, 'holodex:upload'), updated_at = NOW()
WHERE NOT (is_system AND name = 'admin')
  AND 'holodex:upload' = ANY(permissions);

-- admin の旧来の送信能力を保つ。権限編集で '*' と sync:run を両方外した
-- admin に、運用者名義の書き込みを新しく許可しない。
UPDATE roles
SET permissions = array_append(permissions, 'holodex:upload'), updated_at = NOW()
WHERE is_system AND name = 'admin'
  AND ('*' = ANY(permissions) OR 'sync:run' = ANY(permissions))
  AND NOT ('holodex:upload' = ANY(permissions));
