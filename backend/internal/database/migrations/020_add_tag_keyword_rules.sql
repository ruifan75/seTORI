-- 配信タイトルの文字列マッチによる自動タグ付けルール。
-- Holodex の topic は 歌枠(singing) しか対応せず、誤判定もあるため、
-- タイトルに特定の語を「含む」場合に stream_tags を付与する独自ロジックを用意する。
-- 1配信に複数タグ可（追加のみ・既存タグは消さない）。マッチは大小無視の部分一致。
CREATE TABLE IF NOT EXISTS tag_keyword_rules (
    id         SERIAL PRIMARY KEY,
    tag_id     VARCHAR(50) NOT NULL REFERENCES stream_tags(id) ON DELETE CASCADE,
    keyword    TEXT NOT NULL,                 -- タイトルに含まれていれば tag_id を付与（ILIKE 部分一致）
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE (tag_id, keyword)
);

CREATE INDEX IF NOT EXISTS idx_tag_keyword_rules_tag ON tag_keyword_rules(tag_id);

-- 既定ルールを投入。参照先の stream_tags が存在するものだけ入れる
-- （mv / shorts などユーザーが後から追加したタグは新規DBには無いため、WHERE EXISTS でスキップ）。
INSERT INTO tag_keyword_rules (tag_id, keyword)
SELECT v.tag_id, v.keyword
FROM (VALUES
    ('singing', '歌枠'), ('singing', '歌回'), ('singing', 'SINGING'),
    ('karaoke', 'カラオケ'),
    ('birthday', '誕生日'), ('birthday', '生誕'), ('birthday', 'バースデー'),
    ('anniversary', '周年'), ('anniversary', '記念'), ('anniversary', '万人'), ('anniversary', '登録者'),
    ('3d', '3D'),
    ('home3D', 'おうち3D'), ('home3D', 'お家3D'),
    ('members_only', 'メン限'), ('members_only', 'メンバー限定'), ('members_only', 'メンバーシップ'),
    ('concert', 'ワンマン'), ('concert', 'ライブ'),
    ('music_cover', '歌ってみた'), ('music_cover', '歌みた'),
    ('mv', 'MV'), ('mv', 'ミュージックビデオ'),
    ('original_song', 'オリジナル曲'), ('original_song', 'オリジナルソング'), ('original_song', 'オリ曲'),
    ('relay', 'リレー'),
    ('shorts', 'shorts'), ('shorts', 'ショート'),
    ('opening', '開会式'),
    ('unarchived', 'アーカイブなし')
) AS v(tag_id, keyword)
WHERE EXISTS (SELECT 1 FROM stream_tags t WHERE t.id = v.tag_id)
ON CONFLICT (tag_id, keyword) DO NOTHING;
