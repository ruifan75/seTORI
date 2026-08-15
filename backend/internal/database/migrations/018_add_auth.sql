-- 認証・認可（RBAC / ACL）
-- users … ログインアカウント、roles … 権限セット（編集可能）、sessions … Bearer トークン
-- 匿名（未ログイン）は閲覧のみ。編集は role の permissions で制御する。

-- 旧スキャフォールドの users テーブル（role VARCHAR 列・未使用・どのマイグレーションも作成していない）が
-- 残っている場合は削除する。role_id 列を持つ新スキーマなら削除しない（冪等）。
DO $$
BEGIN
    IF to_regclass('public.users') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1 FROM information_schema.columns
           WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'role_id'
       ) THEN
        DROP TABLE users CASCADE;
    END IF;
END $$;

-- ========== roles ==========
-- permissions は権限キーの配列。'*' は全権限（管理者）。
-- 既知の権限キー: content:edit / sync:run / ai:manage / logs:view / users:manage / *
CREATE TABLE IF NOT EXISTS roles (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(64) UNIQUE NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    permissions TEXT[] NOT NULL DEFAULT '{}',
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,  -- 組み込みロール（削除不可）
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 組み込みロールを投入（既に存在すれば何もしない）
INSERT INTO roles (name, description, permissions, is_system) VALUES
    ('admin',  '管理者（全権限）',          ARRAY['*'],                                          TRUE),
    ('editor', '編集者（コンテンツ編集）',  ARRAY['content:edit','sync:run','logs:view'],        TRUE),
    ('viewer', '閲覧のみ',                  ARRAY[]::TEXT[],                                     TRUE)
ON CONFLICT (name) DO NOTHING;

-- ========== users ==========
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username      VARCHAR(64) UNIQUE NOT NULL,
    display_name  VARCHAR(128) NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,                 -- pbkdf2_sha256$iter$salt$hash
    role_id       UUID NOT NULL REFERENCES roles(id),
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    last_login    TIMESTAMP WITH TIME ZONE,
    created_at    TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at    TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role_id);

-- ========== sessions ==========
-- token 自体は保存せず SHA-256 ハッシュのみ保存する（DB 流出時にトークンを再利用させない）。
CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
