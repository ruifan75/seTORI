-- seTORI 資料庫 Schema
-- 建立日期: 2026-01-29

-- 啟用 UUID 擴展
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
-- 啟用模糊搜尋擴展
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ==========================================
-- 1. singers（演唱者/VTuber）
-- ==========================================
CREATE TABLE singers (
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
CREATE TABLE songs (
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
CREATE TABLE song_itunes (
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
CREATE TABLE streams (
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
CREATE TABLE performances (
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
CREATE TABLE performance_tags (
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
    ('medley', 'Medley', '#FF69B4');

-- ==========================================
-- 7. performance_performance_tags（演出-版本標籤關聯）
-- ==========================================
CREATE TABLE performance_performance_tags (
    performance_id UUID NOT NULL REFERENCES performances(id) ON DELETE CASCADE,
    tag_id VARCHAR(50) NOT NULL REFERENCES performance_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (performance_id, tag_id)
);

-- ==========================================
-- 8. stream_tags（直播類型標籤）
-- ==========================================
CREATE TABLE stream_tags (
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
    ('members_only', 'メン限', '#4CAF50');

-- ==========================================
-- 9. stream_stream_tags（直播-類型標籤關聯）
-- ==========================================
CREATE TABLE stream_stream_tags (
    stream_id VARCHAR(64) NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    tag_id VARCHAR(50) NOT NULL REFERENCES stream_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (stream_id, tag_id)
);

-- ==========================================
-- 10. performance_singers（演出-演唱者關聯）
-- ==========================================
CREATE TABLE performance_singers (
    performance_id UUID NOT NULL REFERENCES performances(id) ON DELETE CASCADE,
    singer_id VARCHAR(64) NOT NULL REFERENCES singers(id) ON DELETE RESTRICT,
    PRIMARY KEY (performance_id, singer_id)
);

-- ==========================================
-- 11. normalization_queue（正規化佇列）
-- ==========================================
CREATE TYPE normalization_status AS ENUM ('pending', 'ai_suggested', 'confirmed', 'rejected', 'manual');

CREATE TABLE normalization_queue (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    stream_id VARCHAR(64) NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    source VARCHAR(20) NOT NULL,                   -- holodex / comment

    -- 原始資料
    raw_song_id UUID,                              -- Holodex 原始 song entry ID
    raw_name VARCHAR(500) NOT NULL,                -- 原始歌曲名稱
    raw_artist VARCHAR(255),                       -- 原始藝人名稱
    raw_itunes_id BIGINT,                          -- Holodex 的 iTunes ID
    raw_art_url TEXT,                              -- 原始封面圖
    raw_comment TEXT,                              -- 原始 comment 文本
    start_seconds INTEGER NOT NULL,
    end_seconds INTEGER DEFAULT 0,                 -- 0 表示未知

    -- AI 建議：匹配現有歌曲
    suggested_song_id UUID REFERENCES songs(id),

    -- AI 建議：新建歌曲
    suggested_new_name VARCHAR(500),
    suggested_new_artist VARCHAR(255),
    suggested_new_arts TEXT,

    -- AI 建議：共用欄位
    suggested_tags TEXT[],
    suggested_singers VARCHAR(64)[],
    ai_confidence DECIMAL(3,2),                    -- 0.00 - 1.00
    ai_reasoning TEXT,

    -- 狀態管理
    status normalization_status DEFAULT 'pending',
    reviewed_by VARCHAR(100),
    reviewed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(stream_id, raw_song_id)
);

-- ==========================================
-- 12. users（使用者）
-- ==========================================
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(50) NOT NULL UNIQUE,
    display_name VARCHAR(100) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,           -- bcrypt hash
    role VARCHAR(20) DEFAULT 'editor',             -- admin / editor / viewer
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_login TIMESTAMP WITH TIME ZONE
);

-- ==========================================
-- 索引設計（查詢優化）
-- ==========================================

-- songs 索引
CREATE INDEX idx_songs_name ON songs(name);
CREATE INDEX idx_songs_original_artist ON songs(original_artist);
CREATE INDEX idx_songs_name_trgm ON songs USING gin(name gin_trgm_ops);
CREATE INDEX idx_songs_artist_trgm ON songs USING gin(original_artist gin_trgm_ops);

-- streams 索引
CREATE INDEX idx_streams_date ON streams(stream_date DESC);
CREATE INDEX idx_streams_title_trgm ON streams USING gin(title gin_trgm_ops);

-- performances 索引
CREATE INDEX idx_performances_song_id ON performances(song_id);
CREATE INDEX idx_performances_stream_id ON performances(stream_id);

-- normalization_queue 索引
CREATE INDEX idx_normalization_queue_status ON normalization_queue(status);
CREATE INDEX idx_normalization_queue_stream ON normalization_queue(stream_id);
CREATE INDEX idx_normalization_queue_source ON normalization_queue(source);

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

CREATE TRIGGER update_singers_updated_at
    BEFORE UPDATE ON singers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_songs_updated_at
    BEFORE UPDATE ON songs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_streams_updated_at
    BEFORE UPDATE ON streams
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_normalization_queue_updated_at
    BEFORE UPDATE ON normalization_queue
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
