-- プレイリスト（私人 / 限定公開 / 公開）
--
-- 1項目＝1歌唱記録（performances）。配信内の start〜end 区間を指すので、
-- 楽曲ではなく performance を参照する（プレイヤーの PlayerTrack と同じ粒度）。
-- performance ID は 025 以降の差分更新により編集をまたいで維持される。

CREATE TABLE IF NOT EXISTS playlists (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        VARCHAR(200) NOT NULL,
    description TEXT NOT NULL DEFAULT '',

    -- private   : 本人のみ
    -- unlisted  : share_slug を知っている人だけ（一覧には出さない）
    -- public    : 誰でも閲覧可・一覧に出る
    visibility  VARCHAR(16) NOT NULL DEFAULT 'private'
                CHECK (visibility IN ('private', 'unlisted', 'public')),

    -- 限定公開用の推測困難なキー。private でも保持し、visibility を戻しても同じ URL が使える。
    share_slug  VARCHAR(32) NOT NULL UNIQUE,

    created_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_playlists_user ON playlists(user_id);
-- 公開一覧の取得用
CREATE INDEX IF NOT EXISTS idx_playlists_public ON playlists(updated_at DESC) WHERE visibility = 'public';

CREATE TABLE IF NOT EXISTS playlist_items (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    playlist_id    UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    performance_id UUID NOT NULL REFERENCES performances(id) ON DELETE CASCADE,
    position       INTEGER NOT NULL,
    added_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    -- 同じ歌唱を同一プレイリストに重複して入れない
    UNIQUE (playlist_id, performance_id)
);

CREATE INDEX IF NOT EXISTS idx_playlist_items_playlist ON playlist_items(playlist_id, position);
CREATE INDEX IF NOT EXISTS idx_playlist_items_performance ON playlist_items(performance_id);
