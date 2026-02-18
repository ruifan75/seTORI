-- Performance に自由文字 custom tags を追加
ALTER TABLE performances ADD COLUMN IF NOT EXISTS custom_tags TEXT[] DEFAULT '{}';
