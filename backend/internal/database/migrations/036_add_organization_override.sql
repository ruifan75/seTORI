-- 事務所の分類について「Holodex の意見」と「こちらの判断」を分けて持つ。
--
-- 背景：
-- singers.organization は Holodex 同期の Upsert が毎回上書きする
-- （organization = EXCLUDED.organization）。しかも上書きはチャンネル同期だけでなく
-- mention 経由でも起きるので、無関係な歌枠を同期した拍子に静かに戻る。
-- つまり分類を手で直しても残らなかった。
--
-- かといって is_hidden のように同期対象から外すのは誤り。事務所の所属は
-- 実際に変わる（転籍・卒業・事務所の統廃合）ので、凍結すると Holodex が
-- 正しく更新しても永久に受け取れなくなる。
--
-- そこで end_source / end_confirmed と同じ形にする。2 つは直交する事実なので
-- 2 列に分け、読むときに override を優先する。override を消せば Holodex の値に戻る。
ALTER TABLE singers
    ADD COLUMN IF NOT EXISTS organization_override VARCHAR(100);

ALTER TABLE singers
    DROP CONSTRAINT IF EXISTS singers_organization_override_fkey;
ALTER TABLE singers
    ADD CONSTRAINT singers_organization_override_fkey
    FOREIGN KEY (organization_override) REFERENCES organizations(key)
    ON UPDATE CASCADE ON DELETE RESTRICT;

COMMENT ON COLUMN singers.organization IS
    'Holodex が返した所属（同期のたびに更新される）。表示に使うのは organization_override との COALESCE';
COMMENT ON COLUMN singers.organization_override IS
    'Holodex の分類が誤っているときの手動指定。NULL なら organization をそのまま使う';

-- 「所属なし」を意味する事務所。
--
-- Holodex は個人勢を org "Independents" で返すが、これは事務所名ではなく
-- 「事務所に所属していない」という意味なので、organization が NULL のものと
-- 同じ組に見せたい。ただし 2 つは別の事実である：
--   NULL          … 所属の情報が無い（YouTube 経由で入ったチャンネルなど）
--   Independents  … Holodex が「無所属」と明示している
-- なので値は潰さず、表示のときだけ束ねる。
--
-- チャンネル単位の override ではなく事務所側の旗にしたのは、これが分類そのものへの
-- 判断だから。override だと該当 58 件を手で直したうえ、新しく増えるたびに直す必要がある。
-- 旗なら 1 度で済み、Holodex が後から実在の事務所へ変えたチャンネルは自動的に外れる。
ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS is_unaffiliated BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN organizations.is_unaffiliated IS
    '「所属なし」を意味する分類。一覧では organization が NULL のものと同じ組にまとめ、バッジも出さない';

UPDATE organizations SET is_unaffiliated = TRUE WHERE key = 'Independents';

-- 一覧の既定クエリは COALESCE(organization_override, organization) で引くので、
-- 034 で張った (organization, name) の部分索引は効かない。式索引に置き換える。
DROP INDEX IF EXISTS idx_singers_visible;
CREATE INDEX IF NOT EXISTS idx_singers_visible
    ON singers (COALESCE(organization_override, organization), name)
    WHERE is_hidden = FALSE;
