-- チャンネルの非表示フラグと、事務所名の表記ゆれの正規化。
--
-- 背景：
-- 1曲だけ歌ったゲストや、活動終了・移籍で追いかけていないチャンネルが
-- チャンネル一覧に混ざると、常時追っているチャンネルが埋もれる。
-- streams.is_hidden と同じ考え方で、一覧から外すだけのフラグを持たせる。
--
-- streams.is_hidden との違いは「誰から隠すか」。歌枠の非表示はフィルタ（誰でも
-- hidden=all で見られる）だが、チャンネルの非表示は閲覧者に対する既定の絞り込みで、
-- content:edit を持つレビュー担当だけが一覧に出せる。ただし**チャンネルページ自体は
-- 隠さない**：非表示チャンネルの歌唱も既存の歌枠・楽曲ページからリンクされており、
-- そこから飛んだ先が 404 になると、隠したいのは一覧の場所だけなのにデータが
-- 消えたように見えてしまう。
ALTER TABLE singers
    ADD COLUMN IF NOT EXISTS is_hidden BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN singers.is_hidden IS
    'チャンネル一覧から外す。詳細ページは非表示でも誰でも閲覧できる（隠すのは一覧の場所だけ）';

-- 一覧の既定クエリ（WHERE is_hidden = FALSE）用の部分索引。
CREATE INDEX IF NOT EXISTS idx_singers_visible
    ON singers (organization, name)
    WHERE is_hidden = FALSE;

