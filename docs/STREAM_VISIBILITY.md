# 配信の表示判定

歌枠一覧の `streams.is_hidden` は、Holodex の topic だけで決めない。
Holodex の分類もタイトルキーワード規則も自動判定であり、片方だけを正解として扱うと
短い Shorts を歌枠として出したり、逆に長い歌枠を隠したりするためである。

## `is_hidden` が実際に何を止めるか（認可境界ではない）

このドキュメントの残りは「初回にどちらへ倒すか」の話だが、その前に
**この列が何を止めて何を止めないか**を書いておく。2026-08-22 に実測して確認した。

### 止めるもの

一覧から外すだけではない。`performance_repository` の 8 か所と
`song_repository` / `artist_repository` が `is_hidden = FALSE` で濾すため、
**非表示配信の歌唱は曲ページ・歌手統計・プリセットのすべてから消える**。

その帰結として、**非表示のまま歌唱を登録すると「登録済みだがどこにも出ない」データになる**。
作らないより悪い（完了したように見えて、実際にはどこにも現れない）。
非表示配信に歌唱を作る機能を足すなら、承認時に `is_hidden` を解除するところまで含める。

### 止めないもの

**これは認可境界ではない。** 秘匿が要るものをこの列で守ろうとしないこと。

- `GET /api/streams/search` は非表示行を**意図的に含める**（`SearchStreams`）。
  未ログインで 100 本取ると、実測で非表示 51 本が返る
- `GET /api/streams/{id}` も非表示配信について 200 を返す。タイトル・日付・
  サムネイル・タグ・参加者・歌唱がそのまま読める

つまり **ID を知らなくても非表示配信を列挙できる**。これは設計どおりで、
`singers.is_hidden` の「隠すのは一覧に載る場所だけで、チャンネルページは開ける」と同じ考え方。

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
どれもキャッシュが無いと外部へ取りに行き、**chapters は yt-dlp を運用者のメンバー cookie 付きで
起動する**（実測 3.76 秒、結果を保存して返す）。未ログインから叩けると、匿名のリクエストに
運用者の資格情報と API 枠を使わせることになる。

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
