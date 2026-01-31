-- seTORI 資料庫 Schema - Stream Singers
-- 建立日期: 2026-01-31

-- ==========================================
-- stream_singers（直播-參與者關聯）
-- 記錄每次直播有哪些歌手參與
-- ==========================================
CREATE TABLE IF NOT EXISTS stream_singers (
    stream_id VARCHAR(64) NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    singer_id VARCHAR(64) NOT NULL REFERENCES singers(id) ON DELETE RESTRICT,
    is_owner BOOLEAN DEFAULT FALSE,  -- 是否為頻道擁有者
    PRIMARY KEY (stream_id, singer_id)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_stream_singers_stream_id ON stream_singers(stream_id);
CREATE INDEX IF NOT EXISTS idx_stream_singers_singer_id ON stream_singers(singer_id);
