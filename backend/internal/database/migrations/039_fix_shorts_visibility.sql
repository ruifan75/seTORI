-- 038 で「音楽タグが 1 つでもあれば表示」とした結果、短い Shorts まで
-- original_song / music_cover / mv だけで一覧へ出るようになった。
-- Holodex topic とタイトルタグはいずれも自動判定で誤るため、shorts の印だけでは
-- 隠さず、動画長（3分以下）と組み合わせて短尺動画を判定する。
--
-- 自動判定は今後も変わりうるので、人が表示状態を固定できる override も分けて持つ。
-- is_hidden は検索を単純に保つため有効値を保持し、同期時は
-- COALESCE(visibility_override, auto_hidden) を書く。

ALTER TABLE streams
    ADD COLUMN IF NOT EXISTS visibility_override BOOLEAN;

COMMENT ON COLUMN streams.visibility_override IS
    'NULL=自動判定、TRUE=非表示に固定、FALSE=表示に固定。同期はこの値を上書きしない';

-- 自動判定が shorts タグを前提にするため、新規 DB でも必ず存在させる。
-- 020 は当時タグが無い場合にキーワード規則をスキップしているので、規則も補完する。
INSERT INTO stream_tags (id, display_name, color) VALUES
    ('shorts', 'ショート', '#14B8A6')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tag_keyword_rules (tag_id, keyword) VALUES
    ('shorts', 'shorts'),
    ('shorts', 'ショート')
ON CONFLICT (tag_id, keyword) DO NOTHING;

INSERT INTO stream_stream_tags (stream_id, tag_id)
SELECT s.id, 'shorts'
FROM streams s
WHERE LOWER(BTRIM(COALESCE(s.holodex_data->>'topic_id', ''))) = 'shorts'
ON CONFLICT (stream_id, tag_id) DO NOTHING;

INSERT INTO stream_stream_tags (stream_id, tag_id)
SELECT s.id, r.tag_id
FROM streams s
JOIN tag_keyword_rules r ON s.title ILIKE '%' || r.keyword || '%'
WHERE r.tag_id = 'shorts'
ON CONFLICT (stream_id, tag_id) DO NOTHING;

-- 038 が表示した既存 Shorts を修正する。長い歌枠のタイトルに #shorts がある場合は
-- duration が 180 秒を超えるため対象外（例: CKppP9S5ZPA）。
UPDATE streams s
SET is_hidden = TRUE, updated_at = NOW()
WHERE s.visibility_override IS NULL
  AND (s.duration_seconds IS NULL OR s.duration_seconds <= 0 OR s.duration_seconds <= 180)
  AND (
      LOWER(BTRIM(COALESCE(s.holodex_data->>'topic_id', ''))) = 'shorts'
      OR EXISTS (
          SELECT 1
          FROM stream_stream_tags sst
          WHERE sst.stream_id = s.id
            AND sst.tag_id = 'shorts'
      )
  );
