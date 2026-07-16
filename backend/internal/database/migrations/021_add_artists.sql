-- 原曲アーティストを正規化テーブルに分離する。
-- 狙い：
--  1. アーティスト起点で楽曲を探せる（アーティストページ・検索）
--  2. 読み仮名（name_reading）をアーティスト単位で一元管理でき、
--     一箇所直せば全楽曲に反映される（読み間違い問題の恒久対策）
--
-- 方針：songs.original_artist（表示用テキスト）はそのまま残し、
-- artists + song_artists を検索・集計用のマッピングとして併設する。
-- 既存フロー（楽曲作成/更新/AI正規化）を壊さず、サービス層が保存時に同期する。
-- 「A & B」等の合唱表記の分割は行わない（文字列全体を1アーティストとして扱い、
-- 分割・統合は将来のマージ機能で対応）。

CREATE TABLE IF NOT EXISTS artists (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name         VARCHAR(255) UNIQUE NOT NULL,  -- songs.original_artist と同一表記
    name_reading VARCHAR(255),                  -- 読み（平仮名）
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- あいまい検索（名前・読み）
CREATE INDEX IF NOT EXISTS idx_artists_name_trgm ON artists USING gin (name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_artists_reading_trgm ON artists USING gin (name_reading gin_trgm_ops);

CREATE TABLE IF NOT EXISTS song_artists (
    song_id   UUID NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
    artist_id UUID NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    PRIMARY KEY (song_id, artist_id)
);

CREATE INDEX IF NOT EXISTS idx_song_artists_artist ON song_artists(artist_id);

-- 既存データの回填：distinct な original_artist を artists 化。
-- 読みは同名楽曲の original_artist_reading から非空値をひとつ採用（無ければ NULL）。
INSERT INTO artists (name, name_reading)
SELECT original_artist,
       MAX(original_artist_reading) FILTER (WHERE original_artist_reading IS NOT NULL AND original_artist_reading <> '')
FROM songs
WHERE original_artist <> ''
GROUP BY original_artist
ON CONFLICT (name) DO NOTHING;

-- 楽曲 ↔ アーティストのマッピング回填
INSERT INTO song_artists (song_id, artist_id)
SELECT s.id, a.id
FROM songs s
JOIN artists a ON a.name = s.original_artist
ON CONFLICT DO NOTHING;
