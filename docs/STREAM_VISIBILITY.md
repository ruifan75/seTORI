# 配信の表示判定

歌枠一覧の `streams.is_hidden` は、Holodex の topic だけで決めない。
Holodex の分類もタイトルキーワード規則も自動判定であり、片方だけを正解として扱うと
短い Shorts を歌枠として出したり、逆に長い歌枠を隠したりするためである。

## 自動判定の優先順位

同期は Holodex topic のタグとタイトルキーワードのタグをすべて付けたあと、次の順で判定する。

1. `shorts` の訊号があり、動画長が **180 秒以下**なら非表示
2. `shorts` の訊号があるが動画長を取得できない場合も、保守的に非表示
3. それ以外で音楽系タグを 1 つでも持てば表示
4. どれにも該当しなければ非表示

`shorts` の訊号とは、Holodex の `topic_id = shorts` または seTORI の `shorts` タグを指す。
音楽系タグは次の 6 種である。

- `concert`（ライブ）
- `karaoke`（カラオケ）
- `music_cover`（歌ってみた）
- `mv`（MV）
- `original_song`（オリジナル曲）
- `singing`（歌枠）

短い MV がすべて Shorts とは限らないため、**動画長だけでは隠さない**。
反対に `shorts` という文字だけで隠すと、タイトルに `#shorts` を含む長時間の歌枠まで
消えるため、shorts の訊号と動画長を組み合わせる。

| Video ID | 自動訊号 | 長さ | 結果 | 理由 |
|---|---|---:|---|---|
| `Slm8v4XYzy4` | `shorts`, `original_song` | 25 秒 | 非表示 | 短尺判定が音楽タグより優先 |
| `CKppP9S5ZPA` | `shorts`, `singing` | 5,773 秒 | 表示 | shorts の文字はあるが長い歌枠 |
| `94ogLRe7dwM` | `shorts`, `singing` | 30 秒 | 非表示 | タイトルから singing が付いても実体は短尺 |

判定本体は `internal/service/stream_visibility.go` に置き、Holodex 同期と編集 API が
同じ関数を使う。SQL migration の閾値も同じ 180 秒に揃えること。

## 人工修正を同期から守る

`is_hidden` は一覧検索で使う**実効値**であり、自動判定そのものではない。
人が直した状態を次回同期で戻さないため、`streams.visibility_override` を別に持つ。

| `visibility_override` | 意味 | 実効 `is_hidden` |
|---|---|---|
| `NULL` | 自動 | 現在の自動判定 |
| `FALSE` | 表示に固定 | `FALSE` |
| `TRUE` | 非表示に固定 | `TRUE` |

実効値は常に `COALESCE(visibility_override, auto_hidden)` で決める。
Holodex 同期は外部データと自動タグを更新して自動判定をやり直すが、
`visibility_override` は書き換えない。したがって人工修正は何度同期しても残る。

編集 API `PUT /api/streams/{id}` は `visibility_mode` に
`auto` / `visible` / `hidden` のいずれかを受ける。既存クライアントの `is_hidden` も
互換性のため受けるが、その値は手動固定として保存する。画面で「自動」に戻すと
override を `NULL` にし、その時点の topic・動画長・タグで直ちに再判定する。

## migration は一度だけ実行される

起動時の migration runner は、実行したファイル名を `schema_migrations.version` に記録する。
同じファイル名は通常 **1 回だけ**実行され、サーバー再起動や同期のたびには走らない。
実行済み SQL を編集しても再実行されないため、リリース済み migration は変更せず、
修正は次の番号の新しい migration として追加する。

`039_fix_shorts_visibility.sql` は次を行う。

- nullable な `visibility_override` を追加
- 新しい DB でも `shorts` タグとタイトルキーワード規則が必ず存在するよう補完
- `038` が表示した既存の短尺 Shorts のうち、override が無い行だけを再び非表示にする
- 180 秒を超える `CKppP9S5ZPA` のような長い歌枠は変更しない

なお、039 より前の `is_hidden` には「自動で決まったか、人が直したか」の履歴が無い。
過去の人工修正を機械的に識別して override へ移すことはできないため、039 適用後に
画面から固定した値を、以後の同期で保護する。
