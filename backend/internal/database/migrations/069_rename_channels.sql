-- issue #62 第 3 段。チャンネルの実体と参加者を改名する。
-- migration runner がこのファイル全体と適用記録を 1 Tx で確定する。
-- バックアップ -> 停止 -> migration -> 起動。旧 backend は 069 適用後には使えない。
-- 行を書き換えず、FK の参照先と ON DELETE/UPDATE は PostgreSQL が保持する。
-- performance_singers と singer_id は歌った人なので残す。
-- API/保存済み JSON の singer(s)/singer_id(s) は互換のため書き換えない。

ALTER TABLE singers RENAME TO channels;
ALTER TABLE stream_singers RENAME TO stream_channels;
ALTER TABLE stream_channels RENAME COLUMN singer_id TO channel_id;
ALTER TABLE batch_fill_runs RENAME COLUMN singer_id TO channel_id;

-- PK の制約を改名すると、それに属する index も PostgreSQL が改名する。
ALTER TABLE channels RENAME CONSTRAINT singers_pkey TO channels_pkey;
ALTER TABLE channels RENAME CONSTRAINT singers_organization_fkey TO channels_organization_fkey;
ALTER TABLE channels RENAME CONSTRAINT singers_organization_override_fkey TO channels_organization_override_fkey;
ALTER TABLE channels RENAME CONSTRAINT singers_members_only_policy_check TO channels_members_only_policy_check;
ALTER TABLE stream_channels RENAME CONSTRAINT stream_singers_pkey TO stream_channels_pkey;
ALTER TABLE stream_channels RENAME CONSTRAINT stream_singers_stream_id_fkey TO stream_channels_stream_id_fkey;
ALTER TABLE stream_channels RENAME CONSTRAINT stream_singers_singer_id_fkey TO stream_channels_channel_id_fkey;

ALTER INDEX idx_singers_visible RENAME TO idx_channels_visible;
ALTER INDEX idx_singers_auto_fill RENAME TO idx_channels_auto_fill;
ALTER INDEX idx_stream_singers_stream_id RENAME TO idx_stream_channels_stream_id;
ALTER INDEX idx_stream_singers_singer_id RENAME TO idx_stream_channels_channel_id;
ALTER INDEX idx_stream_singers_owner RENAME TO idx_stream_channels_owner;
ALTER TRIGGER update_singers_updated_at ON channels RENAME TO update_channels_updated_at;

-- singers/stream_singers の主キーは VARCHAR/複合キーで、シーケンスは存在しない。
-- UUID 採番関数や共有の update_updated_at_column はチャンネル専用でないので残す。
-- performance_singers の FK/index/PK 名も歌った人のまま（参照先は channels(id)）。
