-- 楽曲の照合キーと、統合候補の記録。
--
-- 背景：楽曲の同一性を (name, original_artist) の文字列完全一致で判定していたため、
-- コメント側の表記ゆれ（"みきとP feat. 初音ミク" vs "みきとP"、
-- "ランカ・リー(中島愛)" vs "ランカ・リー=中島愛"）がことごとく外れていた。
-- 外れると findOrCreateSong が新曲を作るので、照合漏れがそのまま
-- 近似重複として DB に積もる（次からもっと当たらなくなる）。
--
-- 対策は 2 つ。
--  1. song_match_keys … pkg/songmatch で畳んだ照合キーを持ち、索引で引く
--  2. song_merge_candidates … それでも新曲を作るとき、似た既存曲があれば
--     黙って作らずここに記録して人の統合待ちにする（重複の可視化）

-- ---------- 1. 照合キー ----------
-- キーの計算規則は Go 側（pkg/songmatch）にある。SQL の immutable function に
-- するより規則を書きやすく、テストしやすいため。規則を変えたときは
-- songmatch.RulesVersion を上げると、起動時に rules_version の古い行が作り直される。
CREATE TABLE IF NOT EXISTS song_match_keys (
    song_id        UUID PRIMARY KEY REFERENCES songs(id) ON DELETE CASCADE,
    name_key       TEXT NOT NULL,          -- 曲名を畳んだキー（クレジット括弧は除去、Remix 等は保持）
    artist_primary TEXT NOT NULL DEFAULT '', -- アーティストの主体（feat. の前）
    artist_tokens  TEXT[] NOT NULL DEFAULT '{}', -- 登場する名前すべて（括弧の中も含む）
    rules_version  INTEGER NOT NULL,
    -- キーの計算元。songs 側が別経路で書き換わっても（アーティスト改名・統合など）
    -- 起動時の突き合わせでズレを検出して作り直せるようにするための控え。
    -- 「更新を呼び忘れたらキーが腐る」という壊れ方を構造的に潰すためにある。
    src_name       TEXT NOT NULL DEFAULT '',
    src_artist     TEXT NOT NULL DEFAULT '',
    updated_at     TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 照合の主索引。曲名キーで引いてからアーティストで絞る
CREATE INDEX IF NOT EXISTS idx_song_match_keys_name ON song_match_keys(name_key);
CREATE INDEX IF NOT EXISTS idx_song_match_keys_tokens ON song_match_keys USING gin(artist_tokens);
-- 起動時の再構築で「版が古い行」を拾うため
CREATE INDEX IF NOT EXISTS idx_song_match_keys_version ON song_match_keys(rules_version);

-- ---------- 2. 統合候補 ----------
-- new_song_id を作ったとき、既存の existing_song_id と似ていた、という記録。
-- 解決は既存の統合機能（POST /api/songs/{id}/merge）に任せる。
CREATE TABLE IF NOT EXISTS song_merge_candidates (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    new_song_id      UUID NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
    existing_song_id UUID NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
    score            REAL NOT NULL,
    reason           TEXT NOT NULL,   -- songmatch の判定理由（title_unique 等）
    status           TEXT NOT NULL DEFAULT 'open',  -- open | resolved | dismissed
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    resolved_at      TIMESTAMP WITH TIME ZONE,
    CONSTRAINT song_merge_candidates_pair_unique UNIQUE (new_song_id, existing_song_id),
    CONSTRAINT song_merge_candidates_not_self CHECK (new_song_id <> existing_song_id)
);

CREATE INDEX IF NOT EXISTS idx_song_merge_candidates_open
    ON song_merge_candidates(created_at DESC) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS idx_song_merge_candidates_new ON song_merge_candidates(new_song_id);
CREATE INDEX IF NOT EXISTS idx_song_merge_candidates_existing ON song_merge_candidates(existing_song_id);
