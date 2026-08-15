-- プリセットプレイリスト（運営が用意した歌単）のフォロー
--
-- プリセットの定義（コラボ・睡眠導入など）は internal/service/preset_service.go に持ち、
-- DB には「誰がどれをフォローしたか」だけを置く。中身（playlist_items）は写さない。
-- 写すと条件を書き換えたときにフォロー済みの利用者だけ古い中身のまま取り残されるうえ、
-- 新しく登録された歌唱が永久に入らない。中身は毎回計算する。
--
-- 中身を固定したい利用者には「コピー」を用意してある。そちらは通常の playlists へ
-- 複製されるので、以後は本人が自由に編集でき、プリセット側の変更とは無関係になる。
--
-- preset_key に FK は張れない（参照先がコード上の定義）。定義から消えたキーの行は
-- 一覧を組み立てる時点で無視する（消さないのは、定義を戻したときに復活させたいため）。
CREATE TABLE IF NOT EXISTS playlist_follows (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    preset_key  VARCHAR(64) NOT NULL,
    followed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    PRIMARY KEY (user_id, preset_key)
);

CREATE INDEX IF NOT EXISTS idx_playlist_follows_user ON playlist_follows(user_id, followed_at DESC);
