-- 複合配信検索（配信元・ボーカル・楽曲タグ）の EXISTS 条件を支える索引。
CREATE INDEX IF NOT EXISTS idx_stream_singers_owner
    ON stream_singers(singer_id, stream_id)
    WHERE is_owner = TRUE;

CREATE INDEX IF NOT EXISTS idx_performance_singers_singer_id
    ON performance_singers(singer_id, performance_id);

CREATE INDEX IF NOT EXISTS idx_performance_performance_tags_tag_id
    ON performance_performance_tags(tag_id, performance_id);
