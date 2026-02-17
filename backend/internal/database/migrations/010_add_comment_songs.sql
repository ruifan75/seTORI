-- 新增 comment_songs 欄位，儲存 regex 解析後的未去重歌曲列表
ALTER TABLE streams ADD COLUMN IF NOT EXISTS comment_songs JSONB;
