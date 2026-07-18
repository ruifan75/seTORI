-- 閲覧モードからの「修正提案」。
-- 匿名・編集権限のないユーザーでも、曲/アーティストの修正案を投稿できる。
-- 管理者（content:edit）が内容を確認し、承認すると対象へ反映、却下すると破棄する。
--
-- target_type … 'song' | 'artist'
-- target_id   … 対象の UUID（songs.id / artists.id）。FK は張らない（型が可変のため）。
-- before/after … 提案時点のスナップショットと提案内容（編集可能フィールドのみ）。JSONB。
-- status      … 'pending' | 'approved' | 'rejected'

CREATE TABLE IF NOT EXISTS edit_suggestions (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    target_type  VARCHAR(16) NOT NULL,
    target_id    UUID NOT NULL,
    target_label VARCHAR(512) NOT NULL DEFAULT '',  -- 提案時の表示名（一覧表示用）
    before_data  JSONB NOT NULL DEFAULT '{}',
    after_data   JSONB NOT NULL DEFAULT '{}',
    note         TEXT NOT NULL DEFAULT '',           -- 提案者のコメント
    status       VARCHAR(16) NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    reviewed_at  TIMESTAMP WITH TIME ZONE
);

-- pending を新しい順で引くための複合インデックス
CREATE INDEX IF NOT EXISTS idx_edit_suggestions_status ON edit_suggestions(status, created_at DESC);
