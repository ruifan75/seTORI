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

### 秘匿を入れるなら route の列挙ではなく共通層で

上の表は例示で、**これを route 単位で塞いでも漏れる**。実際この表は
2 回のレビューを経てもまだ増えた（global search と歌手配下一覧は 2 巡目で見つかった）。

配信と歌唱を返す共通の変換層（`toStreamResponse` / `queryPerformanceDetails` の側）で
判定を強制するほうが、新しい route を足したときに素通りしない。
PR #6 が解析結果でやったのと同じ形 ── 引数にして呼び出し側に選ばせれば、
足した人はコンパイルが通らないので判断を迫られる。

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
