-- seTORI 資料庫 Schema - 初始化
-- 建立日期: 2026-01-29
-- 最後更新: 2026-02-01
-- 說明：簡化版本，只包含實際使用的表

-- 啟用 UUID 擴展
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
-- 啟用模糊搜尋擴展
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ==========================================
-- 1. singers（演唱者/VTuber）
-- ==========================================
CREATE TABLE IF NOT EXISTS singers (
    id VARCHAR(64) PRIMARY KEY,                    -- YouTube Channel ID
    name VARCHAR(255) NOT NULL,                    -- 顯示名稱
    english_name VARCHAR(255),                     -- 英文名稱（可選）
    photo_url TEXT,                                -- 頭像 URL
    organization VARCHAR(100),                     -- 所屬組織 (Hololive, ReAcT 等)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ==========================================
-- 2. songs（歌曲 Master）
-- ==========================================
CREATE TABLE IF NOT EXISTS songs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(500) NOT NULL,                    -- 歌曲名稱
    name_reading VARCHAR(500),                     -- 讀音（平假名，用於排序/搜尋）
    original_artist VARCHAR(255) NOT NULL,         -- 原唱藝人
    original_artist_reading VARCHAR(255),          -- 原唱藝人讀音
    arts TEXT,                                     -- 歌曲封面圖 URL
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(name, original_artist)
);

-- ==========================================
-- 3. song_itunes（歌曲的 iTunes ID，一對多）
-- ==========================================
CREATE TABLE IF NOT EXISTS song_itunes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    song_id UUID NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
    itunes_id BIGINT NOT NULL,                     -- iTunes Track ID
    collection_name VARCHAR(255),                  -- 專輯名稱
    country VARCHAR(10),                           -- 國家代碼 (JP, US 等)
    is_primary BOOLEAN DEFAULT FALSE,              -- 是否為主要 iTunes ID
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(song_id, itunes_id)
);

-- ==========================================
-- 4. streams（歌回直播）
-- ==========================================
CREATE TABLE IF NOT EXISTS streams (
    id VARCHAR(64) PRIMARY KEY,                    -- YouTube Video ID
    title VARCHAR(500) NOT NULL,                   -- 直播標題
    stream_date DATE NOT NULL,                     -- 直播日期
    duration_seconds INTEGER,                      -- 直播總長度（秒）
    thumbnail_url TEXT,                            -- 縮圖 URL
    holodex_data JSONB,                            -- Holodex 原始資料（備份）
    holodex_hash VARCHAR(64),                      -- Holodex 資料的 hash（用於檢測更新）
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ==========================================
-- 5. performances（演出紀錄）
-- ==========================================
CREATE TABLE IF NOT EXISTS performances (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    stream_id VARCHAR(64) NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    song_id UUID NOT NULL REFERENCES songs(id) ON DELETE RESTRICT,
    start_seconds INTEGER NOT NULL,                -- 開始時間（秒）
    end_seconds INTEGER NOT NULL,                  -- 結束時間（秒）
    order_index INTEGER NOT NULL,                  -- 在歌回中的順序
    holodex_song_id UUID,                          -- Holodex 原始 song ID（追蹤用）
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(stream_id, song_id, start_seconds)
);

-- ==========================================
-- 6. performance_tags（演出版本標籤）
-- ==========================================
CREATE TABLE IF NOT EXISTS performance_tags (
    id VARCHAR(50) PRIMARY KEY,                    -- 標籤代碼（如 acoustic）
    display_name VARCHAR(100) NOT NULL,            -- 顯示名稱
    color VARCHAR(7),                              -- 顏色 Hex (#FF5733)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 預設標籤
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
-- 7. performance_performance_tags（演出-版本標籤關聯）
-- ==========================================
CREATE TABLE IF NOT EXISTS performance_performance_tags (
    performance_id UUID NOT NULL REFERENCES performances(id) ON DELETE CASCADE,
    tag_id VARCHAR(50) NOT NULL REFERENCES performance_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (performance_id, tag_id)
);

-- ==========================================
-- 8. stream_tags（直播類型標籤）
-- ==========================================
CREATE TABLE IF NOT EXISTS stream_tags (
    id VARCHAR(50) PRIMARY KEY,                    -- 標籤代碼（如 singing）
    display_name VARCHAR(100) NOT NULL,            -- 顯示名稱
    color VARCHAR(7),                              -- 顏色 Hex
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 預設標籤
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
-- 9. stream_stream_tags（直播-類型標籤關聯）
-- ==========================================
CREATE TABLE IF NOT EXISTS stream_stream_tags (
    stream_id VARCHAR(64) NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    tag_id VARCHAR(50) NOT NULL REFERENCES stream_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (stream_id, tag_id)
);

-- ==========================================
-- 10. performance_singers（演出-演唱者關聯）
-- ==========================================
CREATE TABLE IF NOT EXISTS performance_singers (
    performance_id UUID NOT NULL REFERENCES performances(id) ON DELETE CASCADE,
    singer_id VARCHAR(64) NOT NULL REFERENCES singers(id) ON DELETE RESTRICT,
    PRIMARY KEY (performance_id, singer_id)
);

-- ==========================================
-- 索引設計（查詢優化）
-- ==========================================

-- songs 索引
CREATE INDEX IF NOT EXISTS idx_songs_name ON songs(name);
CREATE INDEX IF NOT EXISTS idx_songs_original_artist ON songs(original_artist);
CREATE INDEX IF NOT EXISTS idx_songs_name_trgm ON songs USING gin(name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_songs_artist_trgm ON songs USING gin(original_artist gin_trgm_ops);

-- streams 索引
CREATE INDEX IF NOT EXISTS idx_streams_date ON streams(stream_date DESC);
CREATE INDEX IF NOT EXISTS idx_streams_title_trgm ON streams USING gin(title gin_trgm_ops);

-- performances 索引
CREATE INDEX IF NOT EXISTS idx_performances_song_id ON performances(song_id);
CREATE INDEX IF NOT EXISTS idx_performances_stream_id ON performances(stream_id);

-- ==========================================
-- 更新時間觸發器
-- ==========================================
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- 刪除既有觸發器（如果存在）
DROP TRIGGER IF EXISTS update_singers_updated_at ON singers;
DROP TRIGGER IF EXISTS update_songs_updated_at ON songs;
DROP TRIGGER IF EXISTS update_streams_updated_at ON streams;

-- 建立觸發器
CREATE TRIGGER update_singers_updated_at
    BEFORE UPDATE ON singers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_songs_updated_at
    BEFORE UPDATE ON songs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_streams_updated_at
    BEFORE UPDATE ON streams
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
