-- 將 streams.stream_date 從 DATE 改為 TIMESTAMP WITH TIME ZONE
-- 這樣可以儲存精確到秒的時間資訊，並包含時區

ALTER TABLE streams 
ALTER COLUMN stream_date TYPE TIMESTAMP WITH TIME ZONE 
USING stream_date::TIMESTAMP WITH TIME ZONE;
