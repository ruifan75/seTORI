-- Holodex の topic に対応するタグを、既存の配信へ後から付ける。
--
-- `holodexTopicTagAliases` に 4 つの topic が抜けていて、同期のときに
-- 「表に無いので topic をそのまま tag_id として試す → FK に当たらない → 握りつぶす」
-- という経路へ落ちていた。つまりこの 4 つは **topic からは一度も付いていない**。
--
-- ただし実害は小さい。タイトル規則（020）が偶然ほとんどを拾っていた。
-- 実測（2026-08-29、本番 1314 本）：
--
--   topic         配信   タグ有り   この migration で付く
--   membersonly    86      84            2
--   3D_Stream      22      21            1
--   Birthday       19      19            0
--   Anniversary    17      17            0
--
-- 残る 3 本は、タイトルに「メン限」「3D」等を書いていないもの
-- （例：`LHIsmaRxLOU`「散歩をする稀羽すう / 24時間配信…」）。
-- 秘匿は holodex_data を直接読む migration 052 が押さえているので漏れてはおらず、
-- 実際この 2 本とも is_restricted = TRUE。直すのは**タグを見る側**
-- （FindRandom / preset の除外、画面の表示）と、今後の取りこぼし。

-- ==========================================
-- 1. `3d` タグと、020 が取りこぼした規則
-- ==========================================
-- 表に載せた tag_id が stream_tags に無いと、その topic の配信は同期のたびに
-- 付与だけ失敗する。`music_cover` / `mv` / `original_song` は 038 が、
-- `shorts` は 039 が既に入れているので、topic の対応先として**新しく要るのは
-- `3d` だけ**。
INSERT INTO stream_tags (id, display_name, color) VALUES
    ('3d', '3D', '#795548')
ON CONFLICT (id) DO NOTHING;

-- ==========================================
-- 1-2. 020 が取りこぼした規則と、その参照先のタグ
-- ==========================================
-- 020 は規則を「参照先の stream_tags が存在するものだけ」入れるので、当時
-- タグが無かった DB では宣言だけ残って**規則が入らない**。本番は画面から
-- 手作りしたタグが先にあったため入っており、新しい DB にだけ無い＝環境で
-- 挙動が割れる。038 / 039 が同じ穴を埋めているのと同じ補完。
--
-- **`home3D` は `3d` と対で要る。** preset の 3D は `3d` かつ `home3D` でない
-- もの（§9.5、「配信には 3d と home3D の両方が付く」前提）。`3d` だけ入れると
-- 新しい DB では「おうち3D」に `3d` しか付かず、3D preset へ誤って入る。
--
-- `opening` / `relay` も同じ状態だった（020 が規則を宣言し、タグを誰も入れて
-- いない）。この migration が原因ではないが、同じ 1 か所で塞げるので併せて直す。
-- 表示名と色は本番の実データに合わせる。
INSERT INTO stream_tags (id, display_name, color) VALUES
    ('home3D',  'おうち3D', '#3F51B5'),
    ('opening', '開会式',   '#6366F1'),
    ('relay',   'リレー',   '#FF8C00')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tag_keyword_rules (tag_id, keyword) VALUES
    ('3d', '3D'),
    ('home3D', 'おうち3D'),
    ('home3D', 'お家3D'),
    ('opening', '開会式'),
    ('relay', 'リレー')
ON CONFLICT (tag_id, keyword) DO NOTHING;

-- 038 / 039 と違い、**タイトル規則の既存配信への backfill はしない**。
-- 本番には既にこの規則があるので今後の同期は揃っており、直したいのは
-- 環境差だけ。実測では本番に「タイトルに 3D を含むが 3d タグが無い」配信が
-- 8 本あるが、全部「おうち3D」で、3D preset は `3d` かつ `home3D` でないものを
-- 出す設計（§9.5）。ここに `3d` を付けるかは分類の判断なので、topic の
-- 取りこぼしを直すこの migration では触らない。

-- ==========================================
-- 2. 既存配信への後追い
-- ==========================================
-- 正規化は Go 側（TrimSpace → 小文字化）と揃える。stream_tags との JOIN は、
-- この DB に存在するタグにだけ付けるため（タグは画面から消せるので、
-- FK 違反で migration ごと落とさない）。
INSERT INTO stream_stream_tags (stream_id, tag_id)
SELECT s.id, t.id
FROM streams s
JOIN (VALUES
    ('membersonly', 'members_only'),
    ('3d_stream',   '3d'),
    ('birthday',    'birthday'),
    ('anniversary', 'anniversary')
) AS m(topic, tag_id)
  ON LOWER(BTRIM(COALESCE(s.holodex_data->>'topic_id', ''))) = m.topic
JOIN stream_tags t ON t.id = m.tag_id
ON CONFLICT (stream_id, tag_id) DO NOTHING;
