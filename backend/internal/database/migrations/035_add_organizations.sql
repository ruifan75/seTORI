-- 事務所を実体として持つ。
--
-- 背景：
-- これまで事務所は singers.organization に入った素の文字列でしかなく、
-- 「取り込み時の値」と「画面に出す名前」が同じ列を兼ねていた。そのため
-- 表示を直そうとすると取り込み時の値を壊すしかなかった
-- （Holodex は Re:AcT を org "ReAcT" で返すので、表示を直すには全行を書き換えていた）。
--
-- この構造だと次が全部できない：
--   * Holodex の値と手入力の値を区別する
--   * 表示名だけ後から変える（hololive → ホロライブ）
--   * 一覧の並び順を決める（文字列順なので .LIVE が先頭に来る）
--   * 手動でチャンネルを足すとき、既存の事務所から選ぶ
--
-- key は**取り込み時の生の値**（Holodex の org）で、表示名は display_name が持つ。
-- 分けたのは song_match_keys が計算元テキストを控えるのと同じ理由で、
-- 由来を消してしまうと後から辿り直せなくなるため。
CREATE TABLE IF NOT EXISTS organizations (
    key          VARCHAR(100) PRIMARY KEY,
    display_name VARCHAR(100) NOT NULL,
    sort_order   INTEGER      NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE organizations IS
    '事務所。key は取り込み時の生の値（Holodex の org）で、表示は display_name を使う';
COMMENT ON COLUMN organizations.key IS
    'Holodex の org。取り込み時に未知の値が来たら display_name = key で自動作成する（取り込みは止めない）';
COMMENT ON COLUMN organizations.sort_order IS
    'チャンネル一覧での並び順。小さいほど先。同値なら display_name 順';

-- 034（未リリース）で singers.organization を表示表記へ書き換えていた。
-- 表示名は organizations.display_name が持つことにしたので、生の値へ戻す。
-- リリース済みの環境は無いため、これは 034 を先に流した開発 DB のためだけの行で、
-- 新規構築では何もしない。
UPDATE singers SET organization = 'ReAcT' WHERE organization = 'Re:AcT';

-- 既存データから事務所を起こす。この時点では表示名を機械的に決める根拠が無いので
-- key をそのまま入れ、あとは管理画面（/admin/organizations）で人が直す。
INSERT INTO organizations (key, display_name)
SELECT DISTINCT organization, organization
FROM singers
WHERE organization IS NOT NULL AND organization <> ''
ON CONFLICT (key) DO NOTHING;

-- 空文字は「所属なし」と同じ意味なので NULL に寄せる（FK を張る前に潰しておく）。
UPDATE singers SET organization = NULL WHERE organization = '';

-- 既知の表示名。ここに書くのは「取り込み時の値と公式表記が食い違う」ものだけで、
-- 一致しているものは触らない（key がそのまま表示名として妥当なため）。
UPDATE organizations SET display_name = 'Re:AcT' WHERE key = 'ReAcT';

-- 事務所を消すときに所属チャンネルを黙って孤児にしないよう RESTRICT。
-- 「まずチャンネルを移すか、事務所を空にしてから消す」を強制する。
ALTER TABLE singers
    DROP CONSTRAINT IF EXISTS singers_organization_fkey;
ALTER TABLE singers
    ADD CONSTRAINT singers_organization_fkey
    FOREIGN KEY (organization) REFERENCES organizations(key)
    ON UPDATE CASCADE ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_organizations_sort
    ON organizations (sort_order, display_name);
