-- Add is_processed and is_hidden columns to streams table
ALTER TABLE streams ADD COLUMN IF NOT EXISTS is_processed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE streams ADD COLUMN IF NOT EXISTS is_hidden BOOLEAN NOT NULL DEFAULT FALSE;

-- Create index for efficient filtering
CREATE INDEX IF NOT EXISTS idx_streams_is_processed ON streams(is_processed);
CREATE INDEX IF NOT EXISTS idx_streams_is_hidden ON streams(is_hidden);
