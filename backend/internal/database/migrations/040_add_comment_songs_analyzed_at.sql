-- 解析をいつ走らせたかを記録する。
--
-- streams.updated_at では代用できない。あれは行に対する更新すべてで動くので、
-- 毎日回る Holodex 同期が全配信の updated_at を今日に押し上げてしまう。
--
-- これが無いと、プロンプトや抽出規則を変えたあとに「どの配信がまだ古い規則のままか」
-- が分からず、選択肢が「全部再解析する」しか無くなる。
-- song_match_keys.rules_version が照合キーに対してやっているのと同じ役割。
--
-- 書くのは解析の保存（SaveCommentSongs）のときだけ。拍手 end の付与のように
-- 抽出をやり直していない更新では動かさない ── 動かすと「解析済み」の意味が濁る。
ALTER TABLE streams ADD COLUMN IF NOT EXISTS comment_songs_analyzed_at TIMESTAMP WITH TIME ZONE;

-- 「まだ新しい規則で解析していない配信」を引くための索引。
-- 解析済みの行だけを対象にする（NULL は「一度も解析していない」で別の条件）。
CREATE INDEX IF NOT EXISTS idx_streams_comment_analyzed_at
    ON streams (comment_songs_analyzed_at) WHERE comment_songs_analyzed_at IS NOT NULL;
