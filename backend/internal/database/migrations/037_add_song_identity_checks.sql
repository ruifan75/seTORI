-- 楽曲の同一性判定の記録。
--
-- 「コメントに書かれた表記」と「DB の楽曲」が同じ曲かどうかは、文字列だけでは
-- 決まらないものが残る。深昏睡 と 深昏睡 (Deep coma) は同じ曲だが、
-- Starry night と Starry night (instrumental) は別録音。どちらも「括弧内の未知語」で、
-- 語彙リストでは区別できない（実測で規則を書こうとして行き詰まった）。
--
-- ここは AI か人が下した判定を残す場所。artist_alias_checks と同じ考え方で、
-- **否定も残す**のが要点。残さないと、当たらない組に毎回 AI を呼び続けることになり、
-- 費用も待ち時間も収束しない。
--
-- song_aliases との違い：
--   song_aliases       … 「この表記はこの曲」＝ 肯定だけ。照合の最初に引く（確信度 1.00）
--   song_identity_checks … 肯定・否定の両方。「もう聞いた」ことの記録
CREATE TABLE IF NOT EXISTS song_identity_checks (
    -- name_key + artist_key + song_id を畳んだキー。同じ問いを二度作らないため
    pair_key   TEXT PRIMARY KEY,
    -- 問い合わせた側（コメントの表記）
    name_key   TEXT NOT NULL,
    artist_key TEXT NOT NULL,
    -- 突き合わせた既存楽曲
    song_id    UUID NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
    same       BOOLEAN NOT NULL,
    source     TEXT NOT NULL,   -- ai | manual
    note       TEXT,            -- 判定の理由（AI の説明。人が見直すときの手がかり）
    checked_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- 「この表記について、もう判定済みの曲はどれか」を引く
CREATE INDEX IF NOT EXISTS idx_song_identity_checks_name
    ON song_identity_checks (name_key, artist_key);

-- 楽曲を統合したときに判定を追随させる／消えた曲の判定を掃除するため
CREATE INDEX IF NOT EXISTS idx_song_identity_checks_song
    ON song_identity_checks (song_id);
