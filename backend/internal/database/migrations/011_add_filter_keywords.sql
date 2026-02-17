-- フィルターキーワードテーブル（コメント解析時の除外/保持キーワード）
CREATE TABLE IF NOT EXISTS filter_keywords (
    id SERIAL PRIMARY KEY,
    keyword VARCHAR(255) NOT NULL,
    type VARCHAR(10) NOT NULL CHECK (type IN ('filter', 'keep')),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_filter_keywords_keyword_type ON filter_keywords(keyword, type);

-- Seed: 除外キーワード
INSERT INTO filter_keywords (keyword, type) VALUES
  ('opening', 'filter'), ('オープニング', 'filter'), ('op', 'filter'), ('intro', 'filter'), ('イントロ', 'filter'),
  ('ending', 'filter'), ('エンディング', 'filter'), ('ed', 'filter'), ('outro', 'filter'), ('アウトロ', 'filter'),
  ('start', 'filter'), ('end', 'filter'), ('開始', 'filter'), ('終了', 'filter'), ('終わり', 'filter'),
  ('トーク', 'filter'), ('talk', 'filter'), ('mc', 'filter'), ('雑談', 'filter'), ('おしゃべり', 'filter'), ('chat', 'filter'),
  ('スパチャ', 'filter'), ('superchat', 'filter'), ('super chat', 'filter'), ('スーパーチャット', 'filter'),
  ('marshmallow', 'filter'), ('マシュマロ', 'filter'), ('質問コーナー', 'filter'), ('q&a', 'filter'),
  ('休憩', 'filter'), ('break', 'filter'), ('intermission', 'filter'), ('水分補給', 'filter'),
  ('告知', 'filter'), ('お知らせ', 'filter'), ('notice', 'filter'), ('announcement', 'filter'),
  ('メン限', 'filter'), ('member', 'filter'), ('メンバー限定', 'filter'),
  ('bgm', 'filter'), ('se', 'filter'), ('jingle', 'filter'), ('ジングル', 'filter'),
  ('待機', 'filter'), ('waiting', 'filter'), ('カウントダウン', 'filter'), ('countdown', 'filter'),
  ('テスト', 'filter'), ('test', 'filter'), ('チェック', 'filter'), ('check', 'filter'),
  ('自己紹介', 'filter'), ('introduction', 'filter')
ON CONFLICT DO NOTHING;

-- Seed: 保持キーワード
INSERT INTO filter_keywords (keyword, type) VALUES
  ('cover', 'keep'), ('カバー', 'keep'), ('歌ってみた', 'keep'),
  ('acoustic', 'keep'), ('アコースティック', 'keep'),
  ('piano', 'keep'), ('ピアノ', 'keep'),
  ('original', 'keep'), ('オリジナル', 'keep')
ON CONFLICT DO NOTHING;
