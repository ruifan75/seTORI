-- 修正提案の拡張：提案者の記録・種別・承認時の衝突検出。
--
-- 背景：023 の edit_suggestions は「閲覧ページの鉛筆アイコンから曲/アーティストの
-- フィールドを直す」低頻度の用途を想定していた。歌唱記録（performance）の提案と、
-- 再生中のワンタップ通報を載せると件数が桁違いに増えるため、以下を足す。
--
-- kind            … 提案の種別。'field'（既存＝編集可能フィールドの差し替え）。
--                   将来 'perf.timing' / 'perf.meta' / 'perf.missing' / 'perf.bogus' を足す。
-- payload         … kind ごとの追加情報（'field' では未使用）。
-- created_by      … 提案者。匿名投稿を許すため NULL 可。ユーザー削除時は NULL に落とす。
-- created_by_name … 提案時点の表示名スナップショット（ユーザー削除後も誰の提案か分かるように）。
-- client_hint     … 匿名提案の同一性の手がかり（IP の SHA-256 先頭16桁）。生 IP は保存しない。
-- reviewed_by     … 承認/却下した管理者。
-- review_note     … 却下理由・衝突内容など、レビュー時の記録。
--
-- status に 'conflict' を追加：提案時点のスナップショット（before_data）と現在値が
-- 食い違っている状態。承認すると他人の編集を黙って巻き戻すため、一旦ここで止める。

ALTER TABLE edit_suggestions
    ADD COLUMN IF NOT EXISTS kind            VARCHAR(32)   NOT NULL DEFAULT 'field',
    ADD COLUMN IF NOT EXISTS payload         JSONB         NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS created_by      UUID          REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS created_by_name VARCHAR(128)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS client_hint     VARCHAR(64)   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reviewed_by     UUID          REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS review_note     TEXT          NOT NULL DEFAULT '';

-- 同一対象への未処理提案をまとめて引く（管理画面のグルーピング用）
CREATE INDEX IF NOT EXISTS idx_edit_suggestions_target
    ON edit_suggestions(target_type, target_id, status);

-- 提案者ごとの履歴（濫用の追跡・本人への結果表示用）
CREATE INDEX IF NOT EXISTS idx_edit_suggestions_created_by
    ON edit_suggestions(created_by, created_at DESC)
    WHERE created_by IS NOT NULL;
