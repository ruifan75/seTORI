-- 外部アカウント連携（OAuth）
--
-- provider を列で持つ独立テーブルにして、Google 以外（X / Discord …）を
-- 追加するときにスキーマ変更が要らないようにする。
-- 1人の利用者が複数のプロバイダーを紐付けられる（provider ごとに1件まで）。

CREATE TABLE IF NOT EXISTS oauth_identities (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         VARCHAR(32) NOT NULL,      -- 'google' | 'x' | 'discord' …
    provider_user_id VARCHAR(255) NOT NULL,     -- プロバイダー側の一意 ID（Google なら sub）
    email            VARCHAR(255),              -- 連携時点のメール（表示用。認証には使わない）
    display_name     VARCHAR(255),              -- 連携時点の表示名（初回登録の既定値に使う）
    avatar_url       TEXT,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    -- 同じ外部アカウントを複数の seTORI ユーザーに紐付けさせない
    UNIQUE (provider, provider_user_id),
    -- 1人が同じプロバイダーを二重に紐付けない
    UNIQUE (user_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_oauth_identities_user ON oauth_identities(user_id);
