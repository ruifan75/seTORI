-- 一括の表示変更を、確認した実行と各配信の前後値として残す（issue #27）。
CREATE TABLE visibility_review_runs (
 id UUID PRIMARY KEY,
 status TEXT NOT NULL CHECK (status IN ('preview', 'applied', 'reverted')),
 item_count INTEGER NOT NULL CHECK (item_count > 0),
 reverted_count INTEGER NOT NULL DEFAULT 0,
 started_by UUID REFERENCES users(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 applied_at TIMESTAMPTZ,
 reverted_at TIMESTAMPTZ
);
CREATE TABLE visibility_review_items (
 run_id UUID NOT NULL REFERENCES visibility_review_runs(id) ON DELETE CASCADE,
 -- 配信が消えても「何を変えたか」は残す。stream_id に FK は付けない。
 stream_id TEXT NOT NULL,
 stream_title TEXT NOT NULL,
 field_name TEXT NOT NULL DEFAULT 'is_hidden' CHECK (field_name = 'is_hidden'),
 before_hidden BOOLEAN NOT NULL CHECK (before_hidden = TRUE),
 after_hidden BOOLEAN NOT NULL CHECK (after_hidden = FALSE),
 before_updated_at TIMESTAMPTZ NOT NULL,
 after_updated_at TIMESTAMPTZ,
 reverted_at TIMESTAMPTZ,
 PRIMARY KEY (run_id, stream_id)
);
CREATE INDEX visibility_review_runs_created ON visibility_review_runs (created_at DESC);
