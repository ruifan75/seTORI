-- 公開自助登録と外部アカウント連携のための users 拡張
--
-- これまで users は管理者が発行する前提で、username + password_hash のみだった。
-- 一般利用者が自分で登録できるようにするため email を持たせ、
-- 外部アカウント（Google / X / Discord …）だけで登録した利用者は
-- パスワードを持たないため password_hash を NULL 許容にする。

-- email：既存の管理者アカウントは持っていないので NULL 許容。
-- 大文字小文字を無視して一意にしたいので、正規化した式インデックスで一意性を担保する。
ALTER TABLE users ADD COLUMN IF NOT EXISTS email VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower
    ON users (LOWER(email)) WHERE email IS NOT NULL;

-- 外部アカウントのみの利用者はパスワードを持たない
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;

-- username も大文字小文字を区別せず一意にする（Admin と admin の併存を防ぐ）。
-- 既存の UNIQUE 制約は残したまま、正規化した一意インデックスを追加する。
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_lower ON users (LOWER(username));
