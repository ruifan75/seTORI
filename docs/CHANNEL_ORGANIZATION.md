# チャンネルの整理（非表示と事務所）

チャンネル一覧をどう見せるかの部分。実装は `internal/repository/singer_repository.go`、
`internal/repository/organization_repository.go`、`internal/service/organization_service.go`、
画面は `frontend/src/pages/SingersPage.tsx` と `frontend/src/pages/admin/OrganizationsPage.tsx`。

## 何が問題だったか

チャンネル一覧は全 148 件の素の羅列で、並び順は名前の五十音順だけだった。

1. **常時追っているチャンネルが埋もれる。** 一度ゲスト参加しただけのチャンネル、
   活動終了したチャンネル、コラボで一瞬 mention されただけのチャンネルが、
   毎週の歌枠を追っているチャンネルと同列に並ぶ。Holodex 同期は mention された
   チャンネルも自動で登録するので、放っておくと増え続ける。

2. **事務所が文字列でしか存在しない。** `singers.organization` に Holodex が返した
   `org` がそのまま入っているだけで、実体が無い。そのため事務所ごとにまとめて見ることも、
   並び順を決めることも、表示名を直すこともできなかった。

2 つ目が特に効いていて、`organization` の 1 列が
**「取り込み時の値」と「画面に出す名前」を兼ねていた**。
Holodex は Re:AcT を `ReAcT`（コロン無し）で返すが、公式表記は `Re:AcT` である。
表示を直そうとすると、取り込み時の値を書き換えるしか手が無かった。

## 非表示（`singers.is_hidden`）

### 隠すのは一覧に載る場所だけ

**チャンネルページは非表示でも未ログインで開ける。** これは仕様であって漏れではない。

非表示チャンネルの歌唱は、既存の歌枠ページ・楽曲ページ・プレイリストから既にリンクされている。
ページごと塞ぐと、そこから飛んだ先が 404 になる。利用者から見える現象は
「隠されている」ではなく「データが消えた」で、実際にはデータは全部残っているのに
壊れたように見える。同じ理由で名前検索（`/api/singers/search`）にも残す
── 名前で探すのは「そのチャンネルを見に行く」意図の操作で、
そこで隠すと辿り着く唯一の手段を塞ぐことになる。

つまりこのフラグは秘匿ではなく**整理**である。秘匿が要る場面が出てきたら、
それは別の仕組みとして設計しなおすこと（このフラグを流用しないこと）。

### `streams.is_hidden` との違い

| | 誰から隠すか | 出し方 |
|---|---|---|
| `streams.is_hidden` | 誰からも隠さない | 誰でも `hidden=all` で見られる、ただのフィルタ |
| `singers.is_hidden` | 閲覧者から隠す | `content:edit` を持つ場合のみ `include_hidden=true` が効く |

権限の無い相手が `include_hidden=true` を送っても**黙って無視する**（エラーにしない）。
一覧の見え方の話なので、拒否して知らせる価値が無い。

### 既定は「人が名指ししたか」で決まる

同期は 2 種類のチャンネルを作る。**名指しされた本人**と、**その副産物**（コラボの主催、
mention 先）である。後者は追いたくて登録したわけではないので、一覧に出す理由が無い。

既定が一律 `false` だった頃の本番は **148 件中 147 件を手で隠していた**。
147 回同じ操作を繰り返しているなら、既定のほうが間違っている。

そこで `Upsert` は `SingerOrigin` を必須の引数で受け取る。

| origin | 経路 | 新規行の `is_hidden` |
|---|---|---|
| `SingerRequested` | `POST /api/singers`（人がチャンネルを追加）、`SyncChannel`（同期対象として名指し） | `false` |
| `SingerDiscovered` | `syncVideo` の所有者・mention、`SyncVideo` の動画所有者 | `true` |

bool ではなく型にしてあるのは、`Upsert(singer, true)` ではどちらの意味か読めないため。
引数を省略可能にしていないのは、**新しい呼び出し元が origin を決めずには
コンパイルできないようにする**ため（`docs/STREAM_VISIBILITY.md` の access mode と同じ考え方）。

### 同期で戻らないこと

**`is_hidden` を書くのは INSERT のときだけで、`DO UPDATE SET` には入れない。**
Holodex 同期は繰り返し走るので、ここで書き戻すと手動で非表示にしたチャンネルが
次の同期で一覧に戻ってくる。**同期のたびに設定が消える**のは、
気づくまでに時間がかかり原因も分かりにくい種類の壊れ方なので、明示的に避けてある。

origin を足したあともこれは変わらない。既存行の `is_hidden` は**どちらの origin でも触らない**。
実測（2026-08-22、149 件）：

- 手動追加で表示にした行に `SingerDiscovered` の同期を当てても表示のまま
- 手で非表示にした行に `SingerRequested` の手動追加を当てても非表示のまま
- 配信 1 本を同期し直しても、削除して作り直した 1 件以外の 148 行は変化なし

**この非対称は意図的である。** 「一覧に出したい」は人が明示する操作
（一覧のカードの目のアイコン、`PUT /api/singers/{id}/visibility`）であって、
同じ ID をもう一度追加することではない。既に非表示で存在するチャンネルを
追加し直しても表示に戻らないが、`content:edit` を持つ利用者の一覧には
`include_hidden` で**常に**含まれている（薄字＋「非表示」バッジ）ので、
そのカードから切り替えられる。「登録したのに一覧に出ない」で行き止まりにはならない。

## 事務所（`organizations`）

### key と display_name を分ける

```
organizations
  key          PK   取り込み時の生の値（Holodex の org）
  display_name      画面に出す名前
  sort_order        一覧の並び順（小さいほど先、同値なら display_name の五十音順）
```

`singers.organization` はこの `key` への FK（`ON UPDATE CASCADE ON DELETE RESTRICT`）。

分けた理由は `song_match_keys` が計算元テキストを控えるのと同じで、
**由来を消すと後から辿り直せなくなる**から。1 列で兼ねていると、

- Holodex が返した値と手入力の値が区別できない
- Holodex が org 名を変えたとき、どの行が対応するのか分からない
- 表示名を変えるたびに全行を書き換える必要がある（そして書き換えた瞬間に元の値を失う）

分けたことで、次が全部できるようになる。

| したいこと | 1 列だったとき | 分けたあと |
|---|---|---|
| `ReAcT` を `Re:AcT` と表示 | 全 39 行を書き換え（元の値を失う） | `display_name` を直すだけ |
| `hololive` を `ホロライブ` と表示 | 同上 | 同上 |
| 一覧の並び順を決める | 文字列順のみ（`.LIVE` が先頭に来る） | `sort_order` |
| 手動追加のチャンネルで既存事務所を選ぶ | 自由入力（表記ゆれの発生源） | プルダウン |

### 未知の事務所は自動で作る

Holodex が今まで見たことのない org を返したとき、
`display_name = key` で `organizations` に行を作ってから `singers` を書く。

**「知らない事務所だから取り込まない」は選ばない。** `song_merge_candidates` と同じ態度で、
人の確認が要るものは残しておくが、登録そのものは止めない。表示名は後から直せる。

実装は `SingerRepository.ensureOrganization` で、事務所を書きうる 4 経路
（`Create` / `Update` / `Upsert` / `SetOrganizationOverride`）の先頭で呼ぶ。
**service 層ではなく repository 側に置いてある**のは意図的で、呼び忘れた場合の
壊れ方が悪いから ── ローカルでも CI でも通ってしまい、Holodex が新しい org を
返した日に本番で初めて FK 違反で落ちる。書き込みと同じ場所に置けば忘れようがない。

FK が実際に効いていることは確認済み：

```sql
UPDATE singers SET organization='BrandNewAgency' WHERE id=(SELECT id FROM singers LIMIT 1);
ERROR:  insert or update on table "singers" violates foreign key constraint "singers_organization_fkey"
```

### Holodex の分類を上書きする（`organization_override`）

Holodex の分類が誤っていると思ったとき用。**`organization` は触らず、別列に自分の判断を書く。**
読むときは `COALESCE(organization_override, organization)`。

なぜ上書きが要るか：`Upsert` は `organization = EXCLUDED.organization` なので、
`singers.organization` を直接直しても同期で戻る。しかも戻るのはチャンネル同期のときだけでなく、
**mention 経由でも起きる**（`holodex_service.go` の mentions ループ）。
無関係な歌枠を同期した拍子に静かに戻るので、原因を突き止めるのがとても難しい。

`is_hidden` のように同期対象から外す手もあるが、ここでは誤り。
**事務所の所属は実際に変わる**（転籍・卒業・事務所の統廃合）ので、凍結すると
Holodex が正しく更新しても永久に受け取れなくなる。2 列に分ければ、
同期は今後も Holodex の最新を運び続け、上書きを外した瞬間にそれが反映される。

形は `end_source` / `end_confirmed` と同じ。外部が言っていることと人が決めたことは
直交する事実なので、1 列に潰さない。

書き込み口は**意図的に 2 つだけ**に保っている。

| 列 | 書く人 |
|---|---|
| `organization` | Holodex 同期だけ（外部の事実） |
| `organization_override` | `PUT /api/singers/{id}/organization` だけ（こちらの判断） |

そのため `UpdateManualMetadata`（チャンネル情報の編集モーダル）は
**organization を書かない**。同じ列を 2 経路が別の意味で更新すると、
「同期で戻る値」と「戻らない値」が混ざって追えなくなる。

画面では上書き中に「（手動）」と出す。Holodex と違う値が出ている理由が分からないと、
同期が壊れているのか人が変えたのか判別できないため。

### 「所属なし」を意味する分類（`is_unaffiliated`）

Holodex は個人勢を org `Independents` で返す。これは事務所名ではなく
「事務所に所属していない」という意味なので、`organization` が NULL のものと
同じ組に見せたい（そうしないと同じ意味の組が 2 つ並ぶ）。

ただし 2 つは**別の事実**である。

| 状態 | 意味 |
|---|---|
| `organization IS NULL` | 所属の情報が無い（YouTube 経由で入ったチャンネルなど） |
| `Independents` | Holodex が「無所属」と明示している |

なので値は潰さず、`organizations.is_unaffiliated` を立てて**表示のときだけ束ねる**。
バッジも出さない（見出しが「所属なし」なのにバッジは別名、という矛盾を避ける）。

**チャンネル単位の override ではなく事務所側の旗にしたのが要点。**
これは分類そのものに対する判断なので、override だと該当 58 件を手で直したうえ、
新しく増えるたびに直す必要がある。旗なら 1 度で済み、Holodex が後から実在の事務所へ
変えたチャンネルは自動的にこの組から外れる。

### 削除は RESTRICT

所属チャンネルが残っている事務所は消せない（FK が弾き、API は 409 を返す）。

`ON DELETE SET NULL` にすると所属を黙って外すことになり、
**どのチャンネルが影響を受けたか分からなくなる**。まずチャンネルを移すか外す、
という順序を強制したほうが、後から追える。

### 並び順

```
所属なしは常に最後
  → sort_order（既定 0）
    → display_name の五十音順（nameSortOrder：数字 → 英字 → その他）
```

`key` の文字列順にしていないのは、表示名を直したのに並びが変わらないと直感に反するため。
なお五十音順の規則により `.LIVE` は「その他」扱いで日本語名と同じ末尾グループに入る。
先頭に置きたい場合は `sort_order` を負の値にする。

## 採らなかった設計：ハードコードの正規化表

最初は `NormalizeOrganization(org string) string` という関数を書いて、
`{"react": "Re:AcT"}` という表で取り込み時に書き換えていた。**これは誤りだった。**

理由は 1 つで、**この関数は来た値を上書きするため由来が消える**。
39 行の `ReAcT` が `Re:AcT` になったあと、それが Holodex 由来なのか人が入力したのかは
もう区別できない。表示の都合で取り込みデータを壊しているのに、
表を書き換えても既存行は直らない（別途 migration が要る）という一貫性の無さもあった。

設定画面から編集できるようにする案も検討したが、同じ理由で見送った。
mapping は**書き込み時**に効くので、設定を直しても既存行は変わらない。
利用者は当然変わると期待するので、backfill まで作る羽目になり、
そこまで作るなら key と display_name を分けたほうが素直で安い。

この関数と付随するテストは削除済み。表示表記は `organizations.display_name` が持つ。

### migration の扱い

`034` は一度 `UPDATE singers SET organization='Re:AcT'` を含んでいたが、
未リリースのうちに削除した。`035` に冪等な巻き戻しを入れてある。

```sql
-- 034 を先に流した開発 DB のための行。新規構築では no-op
UPDATE singers SET organization = 'ReAcT' WHERE organization = 'Re:AcT';
```

`singers.organization` は Holodex の生の値を持つのが正しい状態で、
`Re:AcT` は `organizations.display_name` にだけ存在する。

## API

| メソッド | パス | 権限 | 備考 |
|---|---|---|---|
| GET | `/api/singers?group=organization` | 公開 | 事務所別。**ページングなし**（グループを跨ぐページ送りは意味を成さない） |
| GET | `/api/singers?include_hidden=true` | `content:edit` で有効 | 権限が無ければ黙って無視 |
| PUT | `/api/singers/{id}/visibility` | `content:edit` | メタデータ更新と分離（Holodex 管理チャンネルでも切り替えられる必要があるため） |
| PUT | `/api/singers/{id}/organization` | `content:edit` | Holodex の分類の上書き。空文字で解除。同じ理由でメタデータ更新と分離 |
| GET | `/api/organizations` | 公開 | 一覧の見出しと編集画面の選択肢に要る |
| POST | `/api/organizations` | `content:edit` | key 省略時は display_name を key にする |
| PUT | `/api/organizations/{key}` | `content:edit` | 表示名と並び順のみ。**key は変更不可** |
| DELETE | `/api/organizations/{key}` | `content:edit` | 所属チャンネルがあれば 409 |

`PUT /api/singers/{id}/visibility` を `PUT /api/singers/{id}` と分けたのは、
後者が Holodex 管理チャンネルの編集を拒否するため。表示・非表示は Holodex の
メタデータではなく seTORI 側の都合なので、どのチャンネルでも切り替えられる必要がある。

`key` を変更不可にしているのは、変えると Holodex からの取り込みと結びつかなくなり、
次の同期で同じ事務所がもう 1 つ作られるため。

## 画面

- `/singers` … 既定は**事務所別**。名前順の通し一覧は「一覧」に切り替える（`?view=list`）。
  一覧側は従来どおり並び替えとページングが効く
- `/singers/:id` … 非表示チャンネルには「一覧で非表示」バッジ。閲覧者にも出す
  （ページが開けている理由を説明するため）。切り替えは目のアイコン
- `/admin/organizations` … 表示名と並び順の編集、手動追加、削除。
  `display_name === key` の行には「未設定」と出す（自動作成されたまま
  人が触っていないことが一目で分かる）

チャンネル編集の「所属」は自由入力をやめてプルダウンにした。表記ゆれはここから生まれる。

## 残っているもの

- **上書きの一覧が無い。** どのチャンネルを手動で直したかは 1 件ずつ開かないと分からない。
  `organization_override IS NOT NULL` を管理画面に出せば、Holodex 側が正しくなった分を
  まとめて外せる
- **`sort_order` の UI が数値入力のまま。** 事務所が増えたらドラッグで並べ替えたい。
  今は 17 件なので数値で足りている
- **2 つの key を同じ事務所に寄せられない。** 事務所が改名して Holodex 側の org も
  変わった場合、旧 key と新 key が別グループになる。実際に起きたら
  `organization_aliases`（source key → organization）を足すのが素直で、
  移行先の表が既にあるので後付けは難しくない。起きていないうちは作らない
- **表示名の初期値を人手でしか直せない。** MusicBrainz や Wikidata に事務所の
  正式名称があるので、`display_name === key` の行だけ引いて候補を出せる余地はある
- **チャンネルの非表示は完全に手動。** 「1 年以上配信が無い」「歌枠が 0 件」など、
  非表示候補を提案する導線があると初期整理が楽になる
