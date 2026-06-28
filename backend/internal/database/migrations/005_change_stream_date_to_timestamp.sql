-- streams.stream_date を DATE から TIMESTAMP WITH TIME ZONE に変更
-- これにより秒単位の正確な時間情報とタイムゾーンを保存可能にする

ALTER TABLE streams 
ALTER COLUMN stream_date TYPE TIMESTAMP WITH TIME ZONE 
USING stream_date::TIMESTAMP WITH TIME ZONE;
