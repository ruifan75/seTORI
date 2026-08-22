# 配信の表示判定

歌枠一覧の `streams.is_hidden` は、Holodex の topic だけで決めない。
Holodex の分類もタイトルキーワード規則も自動判定であり、片方だけを正解として扱うと
短い Shorts を歌枠として出したり、逆に長い歌枠を隠したりするためである。

## `is_hidden` が実際に何を止めるか（認可境界ではない）

このドキュメントの残りは「初回にどちらへ倒すか」の話だが、その前に
**この列が何を止めて何を止めないか**を書いておく。2026-08-22 に実測して確認した。

### 止めるもの — 「発見面」から消える

一覧から外すだけではない。非表示配信の歌唱は**通常の発見面から消える**：
曲ページ、歌手ページ、タグページ、ランダム再生、プリセット。
`performance_repository` に `is_hidden = FALSE` が 8 か所あり、これらはすべて
「曲・タグ・歌手からの逆引き」「ランダム」「プリセット」を濾すもの。
ほかに `song_repository` / `artist_repository` / `stream_repository` /
`singer_repository`（配信数・歌唱数の統計）/ `tag_repository`（タグ件数）も見ている。

### 止めないもの — **認可境界ではない**

**秘匿が要るものをこの列で守ろうとしないこと。** 濾していない経路がある
（**網羅ではない。例示**）：

| 経路 | 何が漏れるか |
|---|---|
| `GET /api/streams/search` | 非表示行を**意図的に含める**（未ログインで 100 本中 51 本） |
| `GET /api/search?q=<タイトル>` | `SearchByTitle` に `is_hidden` 条件が無い。ID・タイトル・日付を返す |
| `GET /api/singers/{id}/streams?hidden=true` | **非表示だけ**を返す。この GET 自体は公開 |
| `GET /api/streams/{id}` | 非表示でも 200。`performances` も含む |
| `GET /api/performances/{id}` | `FindByID` は `WHERE p.id = $1` のみ。公開 |
| `GET /api/playlists/{id}/items`（公開プレイリスト） | `ListItems` は非表示配信の歌唱を**意図的に残す** |
| `GET /api/shared/playlists/{slug}/items` | 同上（限定公開の共有リンク） |

（プレイリストは可視性しだい。private は所有者だけなので、`ListItems` が
常に未ログインで読めるわけではない。）

つまり **ID を知らなくても非表示配信を列挙でき、そこから歌唱 1 件も読める**。
これは `singers.is_hidden` の「隠すのは一覧に載る場所だけ」と同じ設計思想で、それ自体は妥当。

> ⚠️ **「非表示のまま作れば伏せられる」は成り立たない。**
> 2026-08-22 時点で非表示配信の歌唱が 0 件なので今は見えないが、それはデータの偶然であって
> 保証ではない。**歌唱を作った瞬間に上の経路から読める。**
> 会限のように内容の公開可否が決まっていないものを、この列を頼りに保存しないこと。

## 秘匿の軸（`streams.is_restricted`）

**中身を公開してよいか未確認の配信に立てる旗**（issue #4）。`is_hidden` とは別の列にする。

| | 何を止めるか | 誰に効くか |
|---|---|---|
| `is_hidden` | 一覧・発見面から外す | 誰にも中身は読める（認可境界ではない） |
| `is_restricted` | **歌唱と解析結果そのもの** | 未ログイン・一般利用者。`content:edit` は読める |

**軸を分ける理由は実測できる。** 本番相当のデータに Holodex の `topic_id = membersonly` は
86 本あり、**そのうち 1 本は `is_hidden = FALSE`**（`SEHFB5EiKZo`「メンバーシップ限定歌枠」）。
`is_hidden` から秘匿を導いていたら、この 1 本は素通りしていた。

### 秘匿するのは中身だけで、配信そのものは隠さない

`GET /api/streams/{id}` は秘匿でも **200 のまま**でタイトルも返す。
会限配信のタイトルは YouTube 側でも会限バッジ付きで公開されているので秘密ではない。
伏せたいのは**こちらが作ったセットリスト**であって、配信の存在ではない。

画面では「登録されていない」と「見せていない」を**別の文言にする**
── 同じにすると「まだ誰も作っていない」に見え、二重に作業させることになる。

### 自動判定と人の裁定は別の列に持つ

**1 列だと人の判断が消える。** 会限は chapters / live chat / availability backfill から
繰り返し取り直されるので、次の順で必ず戻っていた：

1. `availability` が `subscriber_only` → 秘匿が立つ
2. 編集者が「公開してよい」と判断して外す
3. 何かの取得で同じ動画をもう一度読む
4. `is_restricted OR subscriber_only` で **また立つ**

そこで `organization` / `organization_override` と同じ形にする（CLAUDE.md §3.5）。

| 列 | 誰が書くか | 意味 |
|---|---|---|
| `is_restricted` | 自動（同期の候補判定・`SaveAvailability`） | 会限らしいという**検出** |
| `restriction_override` | 人だけ（`PUT /api/streams/{id}`） | NULL＝未裁定 / TRUE＝伏せる / FALSE＝公開してよい |

読むときは `COALESCE(restriction_override, is_restricted)`（SQL は `repository.NotRestricted`、
Go は `effectiveRestricted`。**片方だけ変えないこと**）。

**自動判定の側は凍結しない。** `singers.is_hidden` のように固めると、後から会限化した
配信を検出できなくなる。人の裁定が勝つので、検出が立ち続けていても表示は変わらない。

**自動では外れない。** `availability = public` は「反証が無かった」という弱い結論なので
（issue #3）、それで秘匿を解くと誤って公開する。`SaveAvailability` は立てる方向にしか動かない。

### 候補は 3 つの材料から、全部の時点で倒す

| いつ | 材料 |
|---|---|
| migration 052 / 053（既存行） | `availability = subscriber_only` / Holodex `topic_id = membersonly` / `members_only` タグ |
| 初回同期（新しい行） | Holodex `topic_id` / `members_only` タグ（`initialRestrictionCandidate`） |
| yt-dlp を呼んだとき | `availability = subscriber_only` |

**初回同期の判定が要る理由**：`StreamRepository.Upsert` は `is_restricted` を INSERT 列に
持たないので既定の `false` で入る。`availability` は yt-dlp を呼ぶまで埋まらないので、
それを待つ間、新しく同期された会限配信は公開側に置かれてしまう。

Holodex の `topic_id` は単値で `singing` と排他になるため取りこぼすが、
**倒す方向にしか使わない**ので害はない（取りこぼしは `members_only` タグと
`availability` が拾う）。

### 落とす場所（**次に監査するときのチェックリスト。網羅である**）

歌唱を返すクエリすべてに `ViewerAccess` を必須の引数で通す
（`PublicAccess` / `EditorAccess`）。**新しい読み取りを足した人が決めずには
コンパイルできない**ようにするため。

**① 歌唱の行を返す（11）**

`PerformanceRepository` の `FindByStreamID` / `FindBySongID` / `FindByTagID` / `FindByID` /
`FindBySingerID` / `FindRandom` / `FindByPreset` / `FindIDsByPreset` / `CountByPreset` /
**`FindOverlapping`**、`PlaylistRepository.ListItems`

**② 歌唱を*参照する*が行は返さない（8）** ── ここが 2 回とも見落とした場所

| 場所 | 何が漏れるか |
|---|---|
| `SongRepository.GetPerformanceCount` / **`GetPerformanceCounts`**（複数形） | 曲詳細・曲一覧の件数 |
| `SongRepository` の **`songListOrder`**（`sort=performances`） | 並び順から件数が推測できる |
| `SingerRepository.GetPerformanceCount` | 歌手の歌唱数 |
| **`ArtistRepository.FindSongsByArtist`** | アーティスト詳細の各曲件数と既定順 |
| `TagRepository.SearchPerformanceTags` | 歌唱タグの使用件数 |
| `StreamRepository.SearchStreams` の vocalist / 歌唱タグのサブクエリ | 「この配信でこの人が歌った」 |
| **`mergeCandidateSelect`** の `perf_count_new` / `perf_count_old` | 統合候補の件数（streams を JOIN すらしていなかった） |
| `playlistSelectWithMeta` の `item_count` | 見えていない項目の件数 |

> ①は `CountByPreset` を含む。あれは行ではなく件数を返すが、
> `ViewerAccess` を引数に取る同じ仲間なのでここに置く。②は引数を持たず、
> SQL に条件を直書きしている場所。

**③ 保存済みのコピー（2）** ── 対象が**あとから**秘匿になっても失効しない

**a. 提案（`SuggestionRepository.ListByCreator`）**

提案は投稿時の値を非正規化して持つ（`target_label` / `before_data` / `after_data` /
`current_song_name`）ので、クエリに条件を足すだけでは足りず、**保存済みの提案そのものを
落とす**必要がある。migration 052/053 は既存 86 本を一括で秘匿にしたので、
そこに提案があれば同じことが起きる（`GET /api/suggestions/mine` は**ログインだけ**で通る）。

**条件は肯定形で書くこと。** 「秘匿の対象が `EXISTS` なら落とす」という否定形は、
対象が削除されるとサブクエリが 0 行になって条件が true に化け、**提案が通ってしまう**。
`edit_suggestions.target_id` に FK は無く、歌唱は編集保存や一括の撤回で消えるので実際に起きる。
「今も存在して、かつ秘匿でない対象がある」ときだけ通す（対象が消えていれば落ちる）。

**一覧と COUNT の両方**に掛けること ── DTO で落とすと「何件伏せたか」が残る。

**b. Holodex へ送ったセットリスト（外部のコピー）**

`SyncSetoriToHolodex` は運用者の名義で **Holodex のデータベースへ実際に書き込む**
（`PUT https://holodex.net/api/v2/songs`）。秘匿された配信は送らないが、
**送ったあとに秘匿へ変わった場合、向こうのコピーは残る。seTORI からは取り消せない**
（そもそも Holodex が公開している API ではない）。

自動で撤回はしない ── 外部への書き込みは運用者の判断で行うもので、
秘匿の旗が立った拍子に seTORI が勝手に外部 API を叩くべきではない。
代わりに記録して編集画面で警告を出す。**警告の材料は 2 つ要る**：

| 材料 | 何を意味するか |
|---|---|
| `streams.holodex_uploaded_at`（migration 054） | seTORI から**送信を試みた**時刻 |
| `holodex_data` に曲がある | Holodex 側にコピーがある（理由は問わない） |

**記録は PUT の前に書き、書けなければ送らない。** 外部 API と DB は同じ transaction に
入れられないので、どちらかへ倒すしかない。送ってから記録すると、PUT の直後に落ちた場合に
「Holodex にはあるが記録は無い」が残り、**警告が出ないことを安全の根拠にできなくなる**。
先に書けば、送信に失敗しても記録だけが残る（＝過剰な警告）で済む。
そのため値の意味は「送信済み」ではなく**「送信を試みた」**で、画面の文言も断定しない。

**`holodex_uploaded_at` が NULL は「送っていない」ではなく「分からない」。**
送信の口は migration 054 より前から動いていたので、過去の送信はここに無い。
既存行を backfill する手もあるが、Holodex に曲がある理由が seTORI からの送信とは限らず
（元から向こうにあった曲も多い）、機械的には区別できない。だから 2 つ目の材料が要る
── どちらが理由でも、確認しに行く先は同じ Holodex のページ。

**再送しても向こうが最新になるとは限らない。** 既に iTunes ID がある曲は PUT せず飛ばし、
削除の呼び出しも無いので、こちらで直した内容が向こうに古いまま残りうる。

- **count も通す**（`FindBySongID` などは COUNT を別に撃つ）。DTO で落とすと
  件数から存在が漏れ、ページングも合わなくなる
- **「歌唱の行を返すクエリ」だけでは足りない。** 上の②の一覧を見ること。
  実測で、曲の件数は 11 と出るのに一覧には 10 件しか返らず、`vocalist_id=` で検索すると
  秘匿配信が 1 件ヒットしていた（＝「この配信でこの人が歌った」が判定できた）
- **提案の経路も塞ぐ。** `FindOverlapping` は 10 個目の reader で、
  `POST /api/suggestions` と `GET /api/suggestions/mine` は**ログインだけで通る**。
  実測：権限の無い利用者が `end_seconds=0`（＝全既存歌唱に重なる）の提案を投げ、
  自分の提案一覧を開くだけで、秘匿された配信の曲名と開始・終了秒が `overlaps` に返っていた。
  `TargetEditor.GetEditableFields` にも access を通し、**見えないものは提案の対象にもできない**
  ようにしてある
- **プレイリストは秘匿だけ落とし、非表示は残す**。非表示は利用者が自分で入れたものなので
  黙って消さないが、公開・限定公開のプレイリストは未ログインから読めるので秘匿は通せない
- **Holodex への書き戻しも `PublicAccess`**。あれは運用者の名義で外部に残る公開行為なので、
  秘匿された配信の中身を出さない。公開してよいと決まってから秘匿を外して実行する
- 削除（`DeleteByStreamID`）は `EditorAccess`。取りこぼすと消したつもりの歌唱が残る

### 濾せているかの確かめ方（**陰性だけでは足りない**）

「秘匿された配信の歌唱が出ない」を確認しても、**濾せているのか、クエリが落ちているのかは
区別できない**。実際に一度、`restrictClause()` を `ORDER BY` の**後ろ**へ連結してしまい、

```
ORDER BY p.start_seconds AND NOT COALESCE(...)
→ ERROR: argument of AND must be type boolean, not type integer
```

呼び出し側がそのエラーを握りつぶしていたため**常に空**が返り、
「秘匿が効いている」ように見えていた。公開配信でも同じく空だったのに気付けなかった。

確かめるときは必ず 3 つ揃える：

1. **陰性** … 秘匿された配信の歌唱が出ないこと
2. **陽性対照** … **秘匿でない配信では出ること**（＝クエリが実際に走っている）
3. **ログ** … SQL エラーが 0 件であること

実測（秘匿された配信に歌唱を 1 件作って確認、2026-08-23）：

| 経路 | 未ログイン | 編集者 |
|---|---|---|
| 配信詳細の `performances` | 出ない（配信自体は 200） | 出る |
| `GET /api/performances/{id}` | **404** | 200 |
| 曲ページ / 歌手ページ / ランダム | 出ない | 出ない（発見面なので編集者にも出さない） |
| 公開プレイリスト / 共有リンク | 出ない | 出ない |
| `PUT` で秘匿を外す | 401 | 外せる（外すと未ログインにも出る） |

### 秘匿を入れるなら route の列挙では足りない

上の表は例示で、**これを route 単位で塞いでも漏れる**。実際この表は
2 回のレビューを経てもまだ増えた（global search と歌手配下一覧は 2 巡目で見つかった）。

ただし **route 単位が正しい場所もある。** `/comments` `/chapters` `/holodex-songs` のように
**endpoint 全体が編集者専用**なら、いまの `requiredPermission` が正しい。
route の列挙が不適切なのは、**同じ endpoint の中で行ごとに秘匿を分ける**場合。
`availability` やチャンネル同意は配信ごとに違うので、path 文字列しか見ない
`requiredPermission` では原理的に判定できない。

行ごとの秘匿を入れるなら、**viewer の文脈を受け取る visibility policy を 1 つ作り、
それを明示的に通す**設計にすること。「共通の変換層で落とす」だけでは足りない：

- `StreamService.GetByID` は `toStreamResponse` を呼んだ**後**に
  `FindByStreamID` の結果を `Performances` へ詰める。変換層で落としても歌唱は残る
- `GET /api/search` は `SearchStreamItem` を直接組み立て、歌手配下一覧は
  `SingerService` の別の converter を使う。`StreamService.toStreamResponse` は共通点ではない
- `PerformanceRepository` も、`queryPerformanceDetails` を通るのは `FindByID` /
  overlap / playlist / random / preset だけ。`FindByStreamID` `FindBySongID`
  `FindByTagID` `FindBySingerID` はそれぞれ独自の query を持つ
- DTO 変換の時点で落とすと、**`total` とページングを計算した後**なので、
  件数から存在が漏れ、空行も残る

つまり一覧 query（**count を含む**）・詳細 service・DTO の全部に access mode を通す必要がある。
**新しい呼び出し元が access mode 無しではコンパイルできない形にすること** ──
PR #6 が解析結果でやったのと同じ考え方だが、通す場所はこちらのほうが多い。

### 歌唱を作るなら公開まで含めて設計する

非表示配信に歌唱を作る機能を足すときは、次の両方を決めること。

- **発見面に出したいなら** `is_hidden` の解除まで含める。解除しないと
  曲ページ・歌手統計・プリセットに出ないので、「登録済みなのに探しても見つからない」になる
- **伏せたままにしたいなら** `is_hidden` では足りない。上の表の経路を塞ぐ別の仕組みが要る（issue #4）

### 解析結果は編集者向けなので載せない

一方、次の 6 つは編集画面だけが読む中間生成物なので、閲覧向けの応答からは外している
（`toStreamResponse` の `includeAnalysis`）。

`holodex_timeline_songs` / `comment_timeline_songs` / `chapter_timeline_songs` /
`comment_songs_analyzed_at` / `has_comment_raw` / `chapter_count`

- 一覧・検索では**常に載せない**
- 詳細は `content:edit` を持つ利用者にだけ載せる

載せていた頃は、コメントのタイムラインが持つ**元コメントの原文**（`original_comment`）が
検索経由で列挙できた。`has_comment_raw` と `chapter_count` はポインタにして省略する
── `false` / `0` を返すのは「非開示」ではなく**別の事実の主張**で、特に
`chapter_count: 0` は「調べたが章節が無い」を意味するため、未調査（-1）の配信について嘘になる。

解析素材を返す GET（`/comments` `/chapters` `/holodex-songs`）も `content:edit` にした。
どれもキャッシュが無いと外部へ取りに行き、**chapters は yt-dlp を起動する**
（実測 3.76 秒、結果を保存して返す）。cookie が付くのは
**設定済みかつ一時ファイルの準備に成功したとき**だけで、`prepareCookies()` は
未設定・作成失敗・書き込み失敗のいずれでも空パスを返し `--cookies` を付けない。
ただし外部プロセスを起動する以上、cookie の有無にかかわらず保護は要る。
未ログインから叩けると、匿名のリクエストに運用者の資格情報と API 枠を使わせることになる。

### この列は 3 つの意味を兼ねている

1. **音楽ではない**（雑談・ゲーム・ラジオ。本番で約 700 本）── 秘匿の必要なし
2. **誤判定**（本当は歌枠）── 見つけたら表に出したい
3. **会限**（歌はあるが埋め込み再生できない）── 内容の公開可否は配信者本人の意思

秘匿が要るのは 3 だけ。1 と 2 に秘匿を課すと上記の設計が壊れるので、
**秘匿は `is_hidden` とは別の軸で持つ**。判定材料は yt-dlp の `availability`
（`subscriber_only`）と、チャンネル単位の同意フラグ。設計は issue #4。

### 再生可否（`availability` / `playable_in_embed`）

判定材料のうち、**yt-dlp 側は実装済み**（issue #3）。`streams` の 3 列で持つ。

| 列 | 意味 |
|---|---|
| `availability` | yt-dlp の生の値（`public` / `subscriber_only` / `unlisted` …） |
| `playable_in_embed` | 埋め込み再生の可否。公開でも所有者が切っていると false |
| `availability_checked_at` | いつ調べたか。**NULL＝未調査**で、「調べたが公開だった」とは別 |

閲覧者へは導出した `playability` だけを返す（`unknown` / `playable` /
`members_only` / `embed_disabled` / `unavailable`）。生の 3 列は `content:edit` のみ。

**`availability` 単独では判定できない。** 本番の live chat / チャプター取得は
どちらも `--ignore-no-formats-error` を付けて yt-dlp を呼んでおり
（そうしないとフォーマット一覧が空のときに落ちる）、**このフラグが付いていると
視聴できない動画でも `availability` が `public` で返る**。実測：

| 動画 | フラグあり | フラグなし |
|---|---|---|
| `hVfDBfreYNI`（実際は視聴不可） | `public` + `playable_in_embed=NA` | `ERROR: Video unavailable` |
| `wB3qGgT1XIQ`（公開） | `public` + `playable_in_embed=True` | 同じ |

そのため導出は両方を見る。`playable_in_embed` が取れていなければ
「動画情報を最後まで取れなかった」＝ `unavailable`。

**会限の判定を先に置くこと。** 実測（`1GlkSFdnCcc`、cookie 有り）では会限が
`subscriber_only` かつ **`playable_in_embed = true`** を返す。yt-dlp は
「埋め込み可」と言うが、IFrame API は同じ動画で `onError: 150` を返す。
`playable_in_embed` を先に見ると会限が `playable` に分類され、
必ず失敗するプレイヤーを描くことになる。

**「動画が無い」と「実行が失敗した」を分ける。** 記録すると
`availability_checked_at` が立ち、`playabilityOf` は `unavailable` を返して
プレイヤーを消し、`FindIDsWithoutAvailability` は二度と拾わない。
740 件の backfill 中に 429 や通信障害が起きれば、残り全部がまとめて誤分類される。
実測：到達できない proxy を挟むと、公開動画の `wB3qGgT1XIQ` が exit=1・stdout 空で返る
── 削除済みと**同じ形**である。

**`unavailable` を記録できるのは専用の `Fetch` だけ。相乗り経路は記録しない。**
相乗りは `--ignore-no-formats-error` を付けているので、YouTube が再生を断っても
yt-dlp は警告だけ出して**終了コード 0 で続行し**（`raise_no_formats(expected=True)` →
`report_warning`）、部分的なメタデータから `availability = public` を出す。
実測でも視聴不可の `hVfDBfreYNI` が `public|NA` を返した。
そこで相乗りは **`availability` と `playable_in_embed` の両方が揃ったときだけ**保存する
（`Resolved()`）。**片方では足りない ── 2 つは別の入力から来ていて、別々に落ちる**：

| 値 | 出どころ | 落ち方 |
|---|---|---|
| `availability` | initial_data の badge | initial_data は **`fatal=False`** で取るので、一時失敗すると `NA` |
| `playable_in_embed` | player response | 上が落ちても `True` のまま |

つまり **cookie 有りの会限で initial_data だけ一時失敗すると `NA<TAB>True`** が
終了コード 0 で返る。`playable_in_embed` だけを目印にしていると、これを信用して保存し、
`subscriber_only` が無いので **`playable` と判定して必ず失敗するプレイヤーを描く**。
しかも保存済みなので二度と調べ直さない。逆に動画そのものを取れないときは
`public<TAB>NA` になる（実測：`hVfDBfreYNI`）── 落ちる側が場合によって違う。

専用の `Fetch` はフラグを付けないので、取れなければ非ゼロで終わる。そのうえで：

1. 終了コード 0 かつ値が取れた → そのまま保存
2. 非ゼロ ＋ cookie を実際に渡した ＋ **一時障害ではない** ＋ stderr が「動画が無い」と読める
   → `unavailable` として保存
3. それ以外 → **保存しない**（未調査のまま残り、次の backfill が拾い直す）

#### `public` は「反証が無かった」であって、公開だと確かめた証拠ではない

これは**どの経路でも 塞ぐこと できない**。`_availability`（`common.py`）は 5 つの材料が
全部 non-None なら `public` を返す。会限かどうかを決めるのは initial_data の badge だが、

- initial_data の完全性は **`contents` の有無だけ**で判定される（`_video.py:3871-3875`）
- `contents` はあるが `videoPrimaryInfoRenderer`（＝badge の入れ物）が欠けた応答は通る
- そのとき `needs_subscription` は `_has_badge(...) or False` ＝ **False** になり、
  「材料は揃った」と数えられる

つまり **会限の配信が `public<TAB>True` で返りうる**。`availability` と
`playable_in_embed` の両方が埋まっているので、値の有無を見るどんな条件でも弾けない。
**専用の `Fetch` も同じ**（これは formats の話ではないので、フラグの有無に関係しない）。

そこで**弱さを消そうとせず、弱さの帰結を消す**：

| 判定 | 性格 | 調べ直す |
|---|---|---|
| `members_only` / `embed_disabled` / `unavailable` | 積極的な発見 | しない |
| `playable`（`public` かつ埋め込み可） | **反証が無かっただけ** | `?recheck=1` で対象に戻る |

帰結の塞ぎ方は 2 つ。

**① 描画時に実測へ倒す。** `YoutubePlayer` の `onError` を配信詳細で拾い、
コード 100 なら「動画が無い」、101/150 なら「埋め込み再生できません」の案内へ切り替える。
**保存済みの判定より新しい事実なので優先する。** これは弱い `playable` だけでなく、
未調査（`unknown`）と、判定が古くなった場合（アーカイブが後から会限化する、
権利で降ろされる）も同時に拾う。

> コードだけでは**会限と埋め込み無効を区別できない**（どちらも 101/150）ので、
> 推測して片方の文言を出さず、両方の可能性を書く。2 や 5 のような
> 一時的・環境依存のコードでは切り替えない。
>
> `PlayerBar` の `onError`（キューを次へ送る）は**別のプレイヤー実体**で、
> 配信詳細の `YoutubePlayer` には効かない。詳細側には長らく退避が無かった。

**② 記録済みでも調べ直せるようにする。** `POST /api/availability/backfill?recheck=1`
で弱い判定の行を対象に戻す。①は見た人にだけ効く一時的なもので、DB は直らないため。

なお①の結果を DB へ書き戻すことはしない ── 未ログインの閲覧者が
書き込めることになるため。直すのは②の運用操作で行う。

**レート制限は `Video unavailable` で始まる。** YouTube の reason が `Video unavailable`、
subreason が `This content isn't available, try again later` で、yt-dlp はこれを連結してから
rate-limited の案内を足す（`extractor/youtube/_video.py`）。
つまり **`Video unavailable` だけで消失と判定すると、レート制限に当たった公開配信を
恒久的に `unavailable` として記録する**。backfill は 700 件超を並列で回すので、これは
起きにくい事故ではなく起こしにいく事故になる。判定は `classifyFetchFailure` に閉じ込めてある
── 述語を 2 つ順に呼ぶ形にすると、順序を入れ替えても型は通りテストも書きにくい。
`availability_failure_test.go` は**決定そのもの**を検査するので、順序を逆にすると落ちる
（実際に入れ替えて確認済み）。

文言に依存するのは避けたかったが、ここでは**外したときに安全側へ倒れる** ──
一致しなければ未調査のまま残るだけ。会限を文字列で見分けるのは逆に危険なので、
そちらは cookie を入れて `subscriber_only` という構造化された値で受け取る。

cookie の判定は `HasCookies()` ではなく**実際に渡せたか**で見る
（`prepareCookies()` は一時ファイルの作成・書き込みに失敗すると空パスを返すので、
設定済みでも cookie 無しで走ることがある）。cookie 無しでは「削除された」と
「会限で見えない」が同じ失敗になり、会限の歌枠に「削除された可能性があります」と
出してしまう。

埋める経路は 3 つ。**相乗りだけでは埋まらない**ので専用の backfill がある：

| 経路 | 追加リクエスト | 埋まらない場合 |
|---|---|---|
| live chat の取得に相乗り | なし | ファイルキャッシュがあると yt-dlp 自体を呼ばない |
| チャプター取得に相乗り | なし | backfill が `is_hidden = FALSE` 限定 |
| `POST /api/availability/backfill` | あり（未調査のみ） | ─ |

**backfill は非表示を除かない。** 会限の歌枠はどれも非表示側にあるので、
除くと判定したい配信がまるごと落ちる（チャプター側は「表示中の配信の
セットリストを埋める」のが目的なので除いて正しい）。

`--print` を既存の実行へ足すときの注意は `internal/service/ytdlp.go` に書いてある
（**`--print` は `--simulate` を含む**ので、ファイルを書く実行には `--no-simulate` が要る。
足しただけだと live chat が黙って落ちてこなくなる）。

## 初回判定の優先順位

配信を初めて登録するときだけ、Holodex topic のタグとタイトルキーワードのタグを
すべて付けたあと、次の順で判定する。

1. `shorts` のシグナルがあり、動画長が **180 秒以下**なら非表示
2. `shorts` のシグナルがあるが動画長を取得できない場合も、保守的に非表示
3. それ以外で音楽系タグを 1 つでも持てば表示
4. どれにも該当しなければ非表示

`shorts` のシグナルとは、Holodex の `topic_id = shorts` または seTORI の `shorts` タグを指す。
音楽系タグは次の 6 種である。

- `concert`（ライブ）
- `karaoke`（カラオケ）
- `music_cover`（歌ってみた）
- `mv`（MV）
- `original_song`（オリジナル曲）
- `singing`（歌枠）

短い MV がすべて Shorts とは限らないため、**動画長だけでは隠さない**。
反対に `shorts` という文字だけで隠すと、タイトルに `#shorts` を含む長時間の歌枠まで
消えるため、shorts のシグナルと動画長を組み合わせる。

| Video ID | 自動シグナル | 長さ | 結果 | 理由 |
|---|---|---:|---|---|
| `Slm8v4XYzy4` | `shorts`, `original_song` | 25 秒 | 非表示 | 短尺判定が音楽タグより優先 |
| `CKppP9S5ZPA` | `shorts`, `singing` | 5,773 秒 | 表示 | shorts の文字はあるが長い歌枠 |
| `94ogLRe7dwM` | `shorts`, `singing` | 30 秒 | 非表示 | タイトルから singing が付いても実体は短尺 |

判定本体は `internal/service/stream_visibility.go` に置く。SQL migration の閾値も
同じ 180 秒に揃えること。

## 初回登録後は再判定しない

初回判定で `streams.is_hidden` を確定したあとは「自動」という状態を持たない。
表示か非表示の二態だけで、画面の「非表示」checkbox と
`PUT /api/streams/{id}` の `is_hidden` がこの列を直接編集する。

既存配信を Holodex から再同期すると、外部 metadata とタグは更新するが
`is_hidden` は更新せず、初回判定も呼ばない。タイトルやタグを手で編集したときも
再判定しない。これにより、人が直した表示状態は追加の override 列なしで維持される。

`StreamRepository.Upsert` の conflict 更新に `is_hidden` を含めないこと、
`HolodexService` が初回登録 (`existing == nil`) のときだけ `SetInitialVisibility` を
呼ぶことの二段で、この仕様を守る。

## migration は一度だけ実行される

起動時の migration runner は、実行したファイル名を `schema_migrations.version` に記録する。
同じファイル名は通常 **1 回だけ**実行され、サーバー再起動や同期のたびには走らない。
実行済み SQL を編集しても再実行されないため、リリース済み migration は変更せず、
修正は次の番号の新しい migration として追加する。

`039_fix_shorts_visibility.sql` は次を行う。

- 3 状態案のため `visibility_override` を一時的に追加（最終的には 041 で削除）
- 新しい DB でも `shorts` タグとタイトルキーワード規則が必ず存在するよう補完
- `038` が表示した既存の短尺 Shorts を一度だけ再び非表示にする
- 180 秒を超える `CKppP9S5ZPA` のような長い歌枠は変更しない

039 は一度 `visibility_override` も追加したが、3 状態の設計を採用しないことにしたため、
`041_remove_stream_visibility_override.sql` がこの列を削除する。039 の期間中に手動設定が
あった場合も、その実効値は既に `is_hidden` に書かれているので表示状態は失われない。
041 適用後は `is_hidden` だけが正となる。
