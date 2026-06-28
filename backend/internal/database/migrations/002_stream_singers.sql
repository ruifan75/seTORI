-- seTORI データベース Schema - Stream Singers
-- 作成日: 2026-01-31

-- ==========================================
-- stream_singers（配信-参加者関連）
-- 各配信に参加した歌手を記録
-- ==========================================
CREATE TABLE IF NOT EXISTS stream_singers (
    stream_id VARCHAR(64) NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    singer_id VARCHAR(64) NOT NULL REFERENCES singers(id) ON DELETE RESTRICT,
    is_owner BOOLEAN DEFAULT FALSE,  -- チャンネルオーナーかどうか
    PRIMARY KEY (stream_id, singer_id)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_stream_singers_stream_id ON stream_singers(stream_id);
CREATE INDEX IF NOT EXISTS idx_stream_singers_singer_id ON stream_singers(singer_id);
