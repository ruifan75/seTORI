-- 照合の「学習」層。030 の照合キーで拾えなかったぶんを、人と AI の判断で埋める。
--
-- 030 の時点で残っていた穴は 2 つとも「文字列では原理的に解けない」もの：
--   1. 同一人物の別名義（松任谷由実 = 荒井由実、ランカ・リー = 中島愛）
--   2. 表記からは同定できないが、人が「これは同じ曲だ」と判断したもの
--
-- どちらも「一度判断したら二度と迷わない」形で持つのが要点。
-- 判断を捨てていると同じ問い合わせを何度も繰り返すことになり、自動化が収束しない。

-- ---------- 1. 学習した楽曲の別表記（Layer 3） ----------
--
-- 「この曲名・アーティストの表記は、この楽曲を指す」という対応表。
-- 種は**楽曲の統合**から採る。030 の受け皿によって、照合を外した表記は
-- そのまま新曲として登録されるので、統合するときの統合元の名前が
-- 「照合を外した生の表記」そのものになっている。
--
--   統合元 ひこうき雲 / 松任谷由実   ← コメントの表記そのまま
--   統合先 ひこうき雲 / 荒井由実
--   → alias("ひこうき雲", "松任谷由実") = 荒井由実版の楽曲 ID
--
-- 次から同じ表記が来たら、類似度計算も AI も通さず一発で当たる。
--
-- 「曲の差し替え（perf.meta）」は種にしない。「この歌唱は Eve の惑星ループでは
-- なくナユタン星人の惑星ループだ」は、その歌唱の割り当てが誤りという意味であって、
-- 2 つの表記が等価という意味ではないため。
CREATE TABLE IF NOT EXISTS song_aliases (
    name_key   TEXT NOT NULL,   -- songmatch.TitleKey
    artist_key TEXT NOT NULL,   -- songmatch.ParseArtist(...).String()
    song_id    UUID NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
    source     TEXT NOT NULL DEFAULT 'merge',  -- merge | manual
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (name_key, artist_key)
);

CREATE INDEX IF NOT EXISTS idx_song_aliases_song ON song_aliases(song_id);

-- ---------- 2. アーティストの別名義（Layer 2） ----------
--
-- 同じ group_id を持つ名前は同一人物。推移的な関係（A=B, B=C なら A=C）を
-- 素直に表せるのでペアではなくグループで持つ。
--
-- artists テーブルに alias_of を生やす案は採らなかった。artists は
-- songs.original_artist の文字列をそのまま upsert したものなので、
-- 「松任谷由実」名義の曲が 1 つも無ければ行自体が存在せず、リンクを張れない。
-- また既存の MergeArtists は表示テキストまで寄せてしまうため、
-- 『ひこうき雲』が荒井由実名義で出された事実が消える。
-- ここでは「同一人物である」ことだけを、表示とは独立に記録する。
CREATE TABLE IF NOT EXISTS artist_aliases (
    name_key     TEXT PRIMARY KEY,  -- songmatch で畳んだ名前（1 名ぶん）
    group_id     UUID NOT NULL,
    display_name TEXT NOT NULL,     -- 人が読む用の元表記
    source       TEXT NOT NULL,     -- manual | ai
    note         TEXT,              -- AI の理由づけなど
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_artist_aliases_group ON artist_aliases(group_id);

-- ---------- 3. 別名義の判定履歴 ----------
--
-- 「同一人物ではない」という判定も残す。ここが無いと、否定された組み合わせに
-- 対して毎回 AI を呼び直してしまい、費用も遅延も収束しない。
-- pair_key は 2 つの name_key を並べ替えて連結したもの（順序で重複しないように）。
CREATE TABLE IF NOT EXISTS artist_alias_checks (
    pair_key   TEXT PRIMARY KEY,
    name_key_a TEXT NOT NULL,
    name_key_b TEXT NOT NULL,
    same       BOOLEAN NOT NULL,
    source     TEXT NOT NULL,   -- ai | manual
    note       TEXT,
    checked_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
