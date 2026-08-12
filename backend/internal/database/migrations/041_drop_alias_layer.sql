-- 照合の学習層をたたむ。
--
-- 経緯：「一度決めたら二度考えない」ために、表記 → 楽曲（song_aliases）と
-- 同一人物のグループ（artist_aliases）を別テーブルで持っていた。照合が
-- 規則だけで行われていた頃は、規則で解けない組を溜める場所が要った。
--
-- 照合が「候補 → AI、候補ゼロ → 曲庫まるごと AI」の二段になったので、前提が変わった。
-- 本番の 255 行で実測すると、
--   候補あり 212 行 … 候補を見せて AI に選ばせる  正解 98.6% / 誤答 0
--   候補ゼロ  43 行 … 曲庫 820 曲を丸ごと見せる   正解 76.7% / 誤答 0
-- で、規則で解けない組は AI がその場で解く。学習を溜めておく必要が無くなった。
-- 溜めた場合の節約も小さい：未照合 82 行のうち表記の種類は 65 で、
-- 同じ表記を二度聞く率は 1.26 倍にすぎない。
--
-- 一方アーティストの別名義は「その人の全楽曲に効く」ので、照合のたびに
-- AI へ聞き直すのは筋が悪い。こちらは artists の列として残す。

-- 1. アーティストの別名義は artists.aliases へ移す。
--
-- artists.alias_of（FK）にしなかった理由は前と同じ：曲が 1 つも無い名義は
-- artists に行が無く、参照できない。配列なら本体の行にぶら下げられる。
ALTER TABLE artists ADD COLUMN IF NOT EXISTS aliases TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_artists_aliases ON artists USING GIN (aliases);

-- 既存の別名義グループを移す。
--
-- 旧テーブルはグループ（group_id）に名前をぶら下げる形で、どれが本体かを持っていない。
-- artists に行があるものを本体とみなし、同じグループの他の表示名を別名義として寄せる
-- （両方に行があれば互いに別名義になる。照合はどちらから引いても同じ結果になる）。
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'artist_aliases') THEN
        UPDATE artists a
        SET aliases = sub.names
        FROM (
            SELECT me.display_name AS canonical,
                   array_agg(DISTINCT other.display_name) AS names
            FROM artist_aliases me
            JOIN artist_aliases other
              ON other.group_id = me.group_id AND other.name_key <> me.name_key
            GROUP BY me.display_name
        ) sub
        WHERE a.name = sub.canonical;
    END IF;
END $$;

DROP TABLE IF EXISTS song_aliases;
DROP TABLE IF EXISTS artist_aliases;

-- 2. 判定履歴は**否定だけ**残す。
--
-- 肯定を残す必要はもう無い。楽曲の同一性は AI がその場で解き、結果は
-- performances として保存される。アーティストの同一人物は artists.aliases に入る。
--
-- 否定は残す。batch では確信度の高い AI 判定がそのまま performances になるので、
-- 人が「これは違う」と消したものを、次の force 実行が同じ理由で書き戻してしまう。
-- 「人が否決した組」を覚えておくのが、その唯一の歯止めになる。
DELETE FROM artist_alias_checks WHERE same;
DELETE FROM song_identity_checks WHERE same;

COMMENT ON TABLE artist_alias_checks IS '「別人」という否定の記録だけを持つ。肯定は artists.aliases が持つ';
COMMENT ON TABLE song_identity_checks IS '「別の曲」という否定の記録だけを持つ。肯定は performances が持つ';
COMMENT ON COLUMN artists.aliases IS '同一人物の別名義（松任谷由実 に対する 荒井由実）。照合時に本体へ寄せる';
