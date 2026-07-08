-- Track where singer/channel metadata came from.
-- Holodex sourced channels are canonical and not manually editable from seTORI.
ALTER TABLE singers
ADD COLUMN IF NOT EXISTS metadata_source VARCHAR(20) NOT NULL DEFAULT 'holodex';

UPDATE singers
SET metadata_source = 'holodex'
WHERE metadata_source IS NULL OR metadata_source = '';

