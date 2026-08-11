# 配信の表示判定

歌枠一覧の `streams.is_hidden` は、Holodex の topic だけで決めない。
Holodex の分類もタイトルキーワード規則も自動判定であり、片方だけを正解として扱うと
短い Shorts を歌枠として出したり、逆に長い歌枠を隠したりするためである。

## 初回判定の優先順位

配信を初めて登録するときだけ、Holodex topic のタグとタイトルキーワードのタグを
すべて付けたあと、次の順で判定する。

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

- 三態案のため `visibility_override` を一時的に追加（最終的には 041 で削除）
- 新しい DB でも `shorts` タグとタイトルキーワード規則が必ず存在するよう補完
- `038` が表示した既存の短尺 Shorts を一度だけ再び非表示にする
- 180 秒を超える `CKppP9S5ZPA` のような長い歌枠は変更しない

039 は一度 `visibility_override` も追加したが、三態設計を採用しないことにしたため、
`041_remove_stream_visibility_override.sql` がこの列を削除する。039 の期間中に人工設定が
あった場合も、その実効値は既に `is_hidden` に書かれているので表示状態は失われない。
041 適用後は `is_hidden` だけが正となる。
