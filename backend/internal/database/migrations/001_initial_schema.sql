-- seTORI データベース Schema - 初期化
-- 作成日: 2026-01-29
-- 最終更新: 2026-02-01
-- 説明：簡略版、実際に使用するテーブルのみ含む

-- UUID 拡張を有効化
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
-- あいまい検索拡張を有効化
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ==========================================
-- 1. singers（歌手/VTuber）
-- ==========================================
CREATE TABLE IF NOT EXISTS singers (
    id VARCHAR(64) PRIMARY KEY,                    -- YouTube Channel ID
    name VARCHAR(255) NOT NULL,                    -- 表示名
    english_name VARCHAR(255),                     -- 英語名（任意）
    photo_url TEXT,                                -- アバター URL
    organization VARCHAR(100),                     -- 所属組織 (Hololive, ReAcT など)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ==========================================
-- 2. songs（楽曲マスター）
-- ==========================================
CREATE TABLE IF NOT EXISTS songs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(500) NOT NULL,                    -- 曲名
    name_reading VARCHAR(500),                     -- 読み（平仮名、並び替え／検索用）
    original_artist VARCHAR(255) NOT NULL,         -- 原曲アーティスト
    original_artist_reading VARCHAR(255),          -- 原曲アーティストの読み
    arts TEXT,                                     -- 楽曲のジャケット画像 URL
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(name, original_artist)
);

-- ==========================================
-- 3. song_itunes（楽曲の iTunes ID、1 対多）
-- ==========================================
CREATE TABLE IF NOT EXISTS song_itunes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    song_id UUID NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
    itunes_id BIGINT NOT NULL,                     -- iTunes Track ID
    collection_name VARCHAR(255),                  -- アルバム名
    country VARCHAR(10),                           -- 国コード（JP、US など）
    is_primary BOOLEAN DEFAULT FALSE,              -- primary iTunes ID か
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(song_id, itunes_id)
);

-- ==========================================
-- 4. streams（歌枠配信）
-- ==========================================
CREATE TABLE IF NOT EXISTS streams (
    id VARCHAR(64) PRIMARY KEY,                    -- YouTube Video ID
    title VARCHAR(500) NOT NULL,                   -- 配信タイトル
    stream_date DATE NOT NULL,                     -- 配信日
    duration_seconds INTEGER,                      -- 配信の総時間（秒）
    thumbnail_url TEXT,                            -- サムネイル URL
    holodex_data JSONB,                            -- Holodex の元データ（バックアップ）
    holodex_hash VARCHAR(64),                      -- Holodex データのハッシュ（更新検出用）
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ==========================================
-- 5. performances（歌唱記録）
-- ==========================================
CREATE TABLE IF NOT EXISTS performances (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    stream_id VARCHAR(64) NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    song_id UUID NOT NULL REFERENCES songs(id) ON DELETE RESTRICT,
    start_seconds INTEGER NOT NULL,                -- 開始時刻（秒）
    end_seconds INTEGER NOT NULL,                  -- 終了時刻（秒）
    order_index INTEGER NOT NULL,                  -- 歌枠内での順序
    holodex_song_id UUID,                          -- Holodex の元 song ID（追跡用）
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(stream_id, song_id, start_seconds)
);

-- ==========================================
-- 6. performance_tags（歌唱バージョンのタグ）
-- ==========================================
CREATE TABLE IF NOT EXISTS performance_tags (
    id VARCHAR(50) PRIMARY KEY,                    -- タグ ID（acoustic など）
    display_name VARCHAR(100) NOT NULL,            -- 表示名
    color VARCHAR(7),                              -- 色の Hex 値（#FF5733）
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 既定タグ
INSERT INTO performance_tags (id, display_name, color) VALUES
    ('acoustic', 'Acoustic Ver.', '#8B4513'),
    ('piano', 'Piano Ver.', '#4169E1'),
    ('弾き語り', '弾き語り', '#228B22'),
    ('acappella', 'A Cappella', '#9932CC'),
    ('short', 'Short Ver.', '#FF8C00'),
    ('full', 'Full Ver.', '#20B2AA'),
    ('medley', 'Medley', '#FF69B4')
ON CONFLICT (id) DO NOTHING;

-- ==========================================
-- 7. performance_performance_tags（歌唱とバージョンタグの関連）
-- ==========================================
CREATE TABLE IF NOT EXISTS performance_performance_tags (
    performance_id UUID NOT NULL REFERENCES performances(id) ON DELETE CASCADE,
    tag_id VARCHAR(50) NOT NULL REFERENCES performance_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (performance_id, tag_id)
);

-- ==========================================
-- 8. stream_tags（配信種別のタグ）
-- ==========================================
CREATE TABLE IF NOT EXISTS stream_tags (
    id VARCHAR(50) PRIMARY KEY,                    -- タグ ID（singing など）
    display_name VARCHAR(100) NOT NULL,            -- 表示名
    color VARCHAR(7),                              -- 色の Hex 値
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 既定タグ
INSERT INTO stream_tags (id, display_name, color) VALUES
    ('singing', '歌枠', '#E91E63'),
    ('anniversary', '周年', '#FFD700'),
    ('birthday', '誕生日', '#FF69B4'),
    ('concert', 'ライブ', '#9C27B0'),
    ('karaoke', 'カラオケ', '#2196F3'),
    ('unarchived', 'アーカイブなし', '#607D8B'),
    ('members_only', 'メン限', '#4CAF50')
ON CONFLICT (id) DO NOTHING;

-- ==========================================
-- 9. stream_stream_tags（配信と種別タグの関連）
-- ==========================================
CREATE TABLE IF NOT EXISTS stream_stream_tags (
    stream_id VARCHAR(64) NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    tag_id VARCHAR(50) NOT NULL REFERENCES stream_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (stream_id, tag_id)
);

-- ==========================================
-- 10. performance_singers（歌唱と歌手の関連）
-- ==========================================
CREATE TABLE IF NOT EXISTS performance_singers (
    performance_id UUID NOT NULL REFERENCES performances(id) ON DELETE CASCADE,
    singer_id VARCHAR(64) NOT NULL REFERENCES singers(id) ON DELETE RESTRICT,
    PRIMARY KEY (performance_id, singer_id)
);

-- ==========================================
-- インデックス設計（クエリ最適化）
-- ==========================================

-- songs のインデックス
CREATE INDEX IF NOT EXISTS idx_songs_name ON songs(name);
CREATE INDEX IF NOT EXISTS idx_songs_original_artist ON songs(original_artist);
CREATE INDEX IF NOT EXISTS idx_songs_name_trgm ON songs USING gin(name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_songs_artist_trgm ON songs USING gin(original_artist gin_trgm_ops);

-- streams のインデックス
CREATE INDEX IF NOT EXISTS idx_streams_date ON streams(stream_date DESC);
CREATE INDEX IF NOT EXISTS idx_streams_title_trgm ON streams USING gin(title gin_trgm_ops);

-- performances のインデックス
CREATE INDEX IF NOT EXISTS idx_performances_song_id ON performances(song_id);
CREATE INDEX IF NOT EXISTS idx_performances_stream_id ON performances(stream_id);

-- ==========================================
-- 更新時刻のトリガー
-- ==========================================
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- 既存のトリガーがあれば削除する
DROP TRIGGER IF EXISTS update_singers_updated_at ON singers;
DROP TRIGGER IF EXISTS update_songs_updated_at ON songs;
DROP TRIGGER IF EXISTS update_streams_updated_at ON streams;

-- トリガーを作成する
CREATE TRIGGER update_singers_updated_at
    BEFORE UPDATE ON singers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_songs_updated_at
    BEFORE UPDATE ON songs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_streams_updated_at
    BEFORE UPDATE ON streams
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
