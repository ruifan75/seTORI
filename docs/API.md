# API（端点・認可・入出力）

seTORI のバックエンドが受け付ける HTTP API。フロントエンドの画面 URL とは別のもの。
この文書はルート登録・handler・DTO・サービスの振る舞いを確認して手で記述する。
認可の監査表は `backend/internal/handler/testdata/route_permissions.json`。
実装から文書や期待値を自動生成しない。

## 共通の約束

### 呼び出しと認可

- パスはサーバーのルートからの相対パス。認証する場合は
  `Authorization: Bearer <token>` を付ける。トークンはログインまたは OAuth の引き換えで取得する。
- 表の「未ログイン可」は認証不要。「ログインのみ」は有効なセッションが必要で、追加の権限は不要。
  権限名を記した行はログインに加えてその権限が必要。`*` は全権限を満たす。
  プレイリストの所有者など、対象ごとの条件はそれぞれの節にも記す。
- 認可は ServeMux に届く前にメソッドとパスで決まる（`requiredPermission`）。
  未ログインなら 401、ログイン済みで権限不足なら 403。
  `content:edit` は `restricted:view` や `holodex:upload` を兼ねない。
- 明記しない成功応答は **200 / JSON**。`201` は作成、`202` は開始・停止要求の受理で、
  処理の完了を意味しない。JSON のキーはこの文書でも API の表記を保つ。
- `Q:` はクエリ、`B:` は JSON ボディ、`F:` は multipart/form-data の欄。
  `—` は追加の入力が無いことを表す。パスの `{id}` などは該当するレコードの ID。
  配信 ID は YouTube 動画 ID、チャンネル ID は YouTube チャンネル ID、曲・歌唱・利用者・実行 ID は UUID。
  タグ・事務所・プリセットはそれぞれの文字列キー。AI プロバイダー・キーワード規則などは整数 ID。
- ページ付き一覧の `pagination` は `{page, limit, total, total_pages}`。
  `page` は 1 から。曲・配信・チャンネル・歌唱の通常の一覧は未指定時 `page=1, limit=20`。
  端点ごとの上限・別の既定値は表に記す。`offset` を受ける一覧は 0 から。
- 検証の失敗は通常 400、対象が無ければ通常 404、競合は 409、内部の失敗は 500。
  エラーは原則 `{error: 説明}`。提案承認の競合などは補足情報を持つ。
  成功応答でも `warning`・`deferred`・`failed`・各項目の `ok` を確認する。
- ServeMux の GET 登録は HEAD も受け付ける。CORS の OPTIONS は共通 middleware が 200 を返す。
  表には明示的に登録したメソッドだけを載せる。

### 秘匿と非表示

**配信のメタデータは秘匿しても公開する。伏せるのはセットリスト・解析素材などの中身。**
`members_only` タグ → 所有チャンネルの方針 → 配信の裁定で実効判定する。
詳細は [配信の表示と秘匿](STREAM_VISIBILITY.md)。

表で **「秘匿：」** と記した端点は `restricted:view`（または `*`）で返る中身・件数・可否が変わる。
権限が無い一覧では秘匿の歌唱を行・件数の両方から除く。配信詳細は 200 のまま歌唱を伏せ、
歌唱の単件取得は 404、解析素材・解析結果は 403 になる。
解析端点は取得・解析前に検査し、権限の無い場合は応答前にも公開可否を確認する。

`content:edit` による差は別にある。配信の処理状態と詳細の解析キャッシュ、
チャンネルの方針・自動処理設定などは編集者向け。配信一覧には解析キャッシュを載せない。
`is_hidden` は発見面から外す旗で、秘匿の代わりにはならない。
チャンネルの非表示も一覧の整理であり、詳細・名前検索は公開のまま。
プレイリストは `restricted:view` があっても常に公開の視界で中身・件数を返す。

### 応答で使う名前

以下は表の省略名。全フィールドの型定義は `backend/internal/dto/dto.go`、
実行記録・管理用の行は `backend/internal/repository/` にある。

| 名前 | 要点 |
|---|---|
| 曲 | `id, name, name_reading, original_artist, artists, arts, performance_count, itunes_ids`。秘匿を見られる場合は `restricted_performance_count` もありうる |
| チャンネル | `id, name, english_name, photo_url, organization, organization_name, metadata_source, can_edit_metadata, is_hidden`。編集者向けに方針・自動処理の設定 |
| 配信 | `id, title, stream_date, duration_seconds, tags, participants, channel_owner, is_hidden, is_restricted`。編集者向けに `is_processed` |
| 歌唱 | `id, stream_id, song_id, song_name, original_artist, start_seconds, end_seconds, tags, custom_tags, singers, youtube_url, end_source, end_confirmed` と配信情報。秘匿を見られる場合は `is_restricted` |
| 提案 | `id, target_type, target_id, kind, before, after, payload, song_swap, status, conflicts, note, created_by_name, review_note` と投稿・審査時刻 |
| プレイリスト | `id, name, description, visibility, share_slug, item_count, owner_name, is_owner` と作成・更新時刻 |
| 解析結果 | `songs, raw_comments`、任意の `warning, deferred, stats`。曲には抽出・正規化・照合・時刻・タグ・変更理由が入る |
| 背景処理 | `id, kind, phase, status, total, done, succeeded, skipped, failed, params, failures[{target,reason}], message, started_by_name, started_at, finished_at` |
| 利用者 | `id, username, display_name, role_id, role, permissions, is_active` など。パスワード・セッションのハッシュは返さない |

## 認証・アカウント

### ログインと外部アカウント

OAuth の開始は URL を JSON で返す。ログイン中に Bearer を付けて開始すると既存アカウントへの連携になる。
callback はフロントエンドの `/login/oauth` へ移動し、トークンの代わりに一回限り・60 秒で失効する
引き換えコードを渡す。最後のログイン手段を失う連携解除は 409。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `POST` | `/api/auth/login` | 未ログイン可 | ユーザー名とパスワードでログインする | B: `username, password` | `{token, user}`。資格情報不正 401、試行制限 429 |
| `POST` | `/api/auth/logout` | ログインのみ | 現在のセッションを失効させる | — | `{message}` |
| `GET` | `/api/auth/me` | ログインのみ | 現在の利用者と権限を確認する | — | 利用者 |
| `GET` | `/api/auth/oauth/providers` | 未ログイン可 | 利用できる OAuth 連携先を列挙する | — | `{providers: [名前]}` |
| `POST` | `/api/auth/oauth/{provider}/start` | 未ログイン可 | ログインまたは連携追加を始める | パス: `provider` | `{auth_url}` |
| `GET` | `/api/auth/oauth/{provider}/callback` | 未ログイン可 | OAuth の戻りを受ける | Q: `code, state`、失敗時 `error` | 302 / Location: `/login/oauth?code=…` または `?error=…` |
| `POST` | `/api/auth/oauth/exchange` | 未ログイン可 | 引き換えコードをセッションに替える | B: `code` | `{token, user}`。無効・期限切れ 400 |
| `GET` | `/api/auth/oauth/identities` | ログインのみ | 自分の連携済み外部アカウントを見る | — | `{identities: [連携情報]}` |
| `DELETE` | `/api/auth/oauth/{provider}` | ログインのみ | 自分の外部アカウント連携を解除する | パス: `provider` | `{message}`。最後のログイン手段なら 409 |

### 利用者・ロール

system role の削除・使用中ロールの削除などは 400。組み込みロールの説明・権限は更新できる。自分自身の無効化・削除も 400。
権限の確認はロール名ではなく、利用者に解決された権限セットを使う。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/users` | `users:manage` | 利用者を列挙する | — | 利用者の配列 |
| `POST` | `/api/users` | `users:manage` | 利用者を作る | B: `username, display_name, password, role_id, is_active`（既定 true） | 201 / 利用者 |
| `PUT` | `/api/users/{id}` | `users:manage` | 表示名・ロール・有効状態を更新する | B: `display_name, role_id, is_active`（省略は true） | 利用者 |
| `PUT` | `/api/users/{id}/password` | `users:manage` | パスワードを変更する | B: `password` | `{message}` |
| `DELETE` | `/api/users/{id}` | `users:manage` | 利用者を削除する | — | `{message}` |
| `POST` | `/api/users/{id}/revoke-sessions` | `users:manage` | 利用者の全セッションを失効させる | — | `{message}` |
| `GET` | `/api/roles` | `users:manage` | ロールを列挙する | — | ロールの配列（`id, name, description, permissions, is_system` など） |
| `POST` | `/api/roles` | `users:manage` | ロールを作る | B: `name, description, permissions[]` | 201 / ロール |
| `PUT` | `/api/roles/{id}` | `users:manage` | ロールの説明と権限を更新する | B: `description, permissions[]` | ロール。名前は変更しない |
| `DELETE` | `/api/roles/{id}` | `users:manage` | ロールを削除する | — | `{message}` |
| `GET` | `/api/permissions` | `users:manage` | 割り当てられる権限のカタログを返す | — | `[{key, description}]` |

## 配信

一覧とタグ別一覧は非表示配信・表示中のチャンネルが参加していない配信を除く。
秘匿配信のメタデータは一覧にも残る。タグ件数は同じ母集合を使う。
`tag=singing&tag=3d` のような繰り返しは **AND**。空・重複を整理し、異なるタグは最大 20 個。
検索は非表示配信・非表示チャンネルも意図的に含めるが、歌唱を使う検索条件には秘匿の視界を通す。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/streams` | 未ログイン可 | 配信一覧を返す | Q: `page, limit, sort=title/stream_date, dir=asc/desc, tag`（繰り返し可） | `{streams: [配信], pagination}` |
| `GET` | `/api/streams/tag-counts` | 未ログイン可 | 一覧を選択中タグで絞ったときの各タグの件数を返す | Q: `tag`（繰り返し可） | `{counts: {タグID: 件数}}` |
| `GET` | `/api/streams/search` | 未ログイン可 | 配信主・参加者・歌った人・タグで配信を検索する | Q: `q, owner_id, participant_ids, vocalist_ids, tags, performance_tags, page, limit`（ID・タグは CSV。旧 `participant_id, singer_id, vocalist_id` も可） | `{streams, pagination}`。秘匿：歌った人・歌唱タグによるヒットから秘匿歌唱を除く |
| `GET` | `/api/streams/{id}` | 未ログイン可 | 配信詳細とセットリストを返す | — | 配信＋`performances`。秘匿：歌唱を伏せる。解析キャッシュは `content:edit` かつ中身を見られる場合のみ |
| `POST` | `/api/streams` | `content:edit` | 手動作成の未対応を知らせる | — | **501** / `{error}`。登録は Holodex 同期を使う |
| `PUT` | `/api/streams/{id}` | `content:edit` | 配信メタデータ・状態・公開の裁定を更新する | B: `title, stream_date, tag_ids[], participant_ids[], is_processed, is_hidden, is_restricted`（部分更新） | 配信＋`performances`。秘匿：歌唱・解析素材を制限。`is_restricted` は人の裁定を書き、検出タグは消さない。日付は RFC3339 / YYYY-MM-DD |

## チャンネル・事務所

チャンネルの API は `/api/channels`。JSON の `singers` / `singer_ids` などは変えない。
旧 `/api/singers` API は廃止済みで 404 を返す（issue #101）。
画面 URL は `/channels` / `/channels/:id`。旧 `/singers` / `/singers/:id` は
クエリとハッシュを保って新 URL へリダイレクトする（ブックマーク用の画面リダイレクトは残す）。
一覧の `include_hidden=true` は `content:edit` が無ければ無視する。
方針・会限本数・自動処理設定は編集者だけに返す。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/channels` | 未ログイン可 | チャンネルを一覧する | Q: `page, limit, sort, dir, include_hidden, group=organization` | 通常 `{singers, pagination, hidden_total?}`。事務所別は `{groups, hidden?, total}`（ページングなし） |
| `GET` | `/api/channels/search` | 未ログイン可 | 名前・英語名でチャンネルを探す | Q: `q, limit`（既定 10） | チャンネルの配列。非表示も含む。空の q は `[]` |
| `GET` | `/api/channels/{id}` | 未ログイン可 | チャンネル詳細を見る | — | チャンネル＋`stream_count, performance_count`。秘匿：歌唱数を制限。非表示でも開ける |
| `GET` | `/api/channels/{id}/streams` | 未ログイン可 | 参加した配信を一覧する | Q: `page, limit, hidden=false/true/all`（既定 false）、`processed=all/true/false`（編集者のみ適用） | `{streams, pagination}`。秘匿でも配信メタデータは残す |
| `GET` | `/api/channels/{id}/performances` | 未ログイン可 | 歌った曲を一覧する | Q: `page, limit, sort, dir` | `{singer, performances, pagination}`。秘匿：歌唱・件数を制限 |
| `POST` | `/api/channels` | `content:edit` | チャンネル情報を取得して登録する | B: `id`（チャンネル ID / @handle / URL） | 201 / `{message, id, name}`。配信は同期しない |
| `PUT` | `/api/channels/{id}` | `content:edit` | 手動管理チャンネルのメタデータを更新する | B: `name, english_name, photo_url` | チャンネル。Holodex 管理なら 403 |
| `PUT` | `/api/channels/{id}/visibility` | `content:edit` | 一覧での表示・非表示を替える | B: `is_hidden` | `{id, is_hidden}` |
| `PUT` | `/api/channels/{id}/members-policy` | `content:edit` | 会限セットリストの公開方針を設定する | B: `members_only_policy=allow/deny/空文字`（必須。空文字は未確認） | `{id, members_only_policy}`。公開可否に影響 |
| `PUT` | `/api/channels/{id}/organization` | `content:edit` | 所属を手動指定する | B: `organization`（事務所 key。空文字で上書き解除） | `{id, organization}`。Holodex 管理でも可 |
| `PUT` | `/api/channels/{id}/auto-fill` | `content:edit` | 自動処理の対象を切り替える | B: `auto_fill_enabled`（必須の bool） | `{id, auto_fill_enabled}` |
| `GET` | `/api/channels/auto-fill` | `content:edit` | 自動処理の対象チャンネルを見る | — | `{singers: [チャンネル]}` |

### 事務所

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/organizations` | 未ログイン可 | 事務所を一覧する | — | `{organizations: [{key, display_name, sort_order, is_unaffiliated, singer_count, …}]}` |
| `POST` | `/api/organizations` | `content:edit` | 事務所を追加する | B: `display_name, key`（省略時は表示名）、`sort_order, is_unaffiliated` | 201 / 事務所。重複は 409 |
| `PUT` | `/api/organizations/{key}` | `content:edit` | 事務所の表示名・順序・所属なし扱いを更新する | B: `display_name, sort_order, is_unaffiliated` | 事務所。key は変更しない |
| `DELETE` | `/api/organizations/{key}` | `content:edit` | 事務所を削除する | — | `{key}`。所属チャンネルが残る場合は 409 |

## 曲・アーティスト

曲の件数・歌唱一覧は要求者の視界で数える。原曲アーティストはチャンネルとは別の実体。
曲一覧の `sort` は `name/artist/performances`、アーティスト一覧は `name/songs`、
`dir` は `asc/desc`。個別の一覧は表にある入力だけを受ける。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/songs` | 未ログイン可 | 曲を検索・一覧する | Q: `page, limit, search, sort, dir` | `{songs: [曲], pagination}`。秘匿：歌唱数を制限 |
| `GET` | `/api/songs/{id}` | 未ログイン可 | 曲の詳細を見る | — | 曲。秘匿：歌唱数を制限 |
| `GET` | `/api/songs/{id}/performances` | 未ログイン可 | 曲の歌唱履歴を見る | Q: `page, limit` | `{song, performances, pagination}`。秘匿：歌唱・件数を制限 |
| `POST` | `/api/songs` | `content:edit` | 曲を作る | B: `name, original_artist`（必須）、`name_reading, original_artist_reading, arts, itunes_ids[{itunes_id,is_primary}]` | 201 / 曲 |
| `PUT` | `/api/songs/{id}` | `content:edit` | 曲を更新する | B: `name, original_artist`（必須）、`name_reading, original_artist_reading, arts, itunes_ids` | 曲。秘匿：返す歌唱数を制限 |
| `DELETE` | `/api/songs/{id}` | `content:edit` | 曲を削除する | — | `{message, id}`。歌唱が残る場合は 409 |
| `POST` | `/api/songs/{id}/merge` | `content:edit` | この曲を別の曲に統合する | B: `target_song_id` | `{message, source_id, target_id, target_song}`。秘匿：返す target_song の歌唱数を制限 |
| `GET` | `/api/artists` | 未ログイン可 | 原曲アーティストを検索・一覧する | Q: `page, limit`（既定 50、最大 100）、`search, sort, dir` | `{artists: [{id,name,name_reading,song_count}], pagination}` |
| `GET` | `/api/artists/{id}` | 未ログイン可 | アーティストと所属曲を見る | Q: `page, limit, sort, dir` | `{artist, songs, pagination}`。秘匿：曲の歌唱数を制限 |
| `PUT` | `/api/artists/{id}` | `content:edit` | 名前と読みを更新する | B: `name, name_reading` | アーティスト。所属曲の原曲アーティスト表記にも伝播 |
| `POST` | `/api/artists/{id}/merge` | `content:edit` | アーティストを別のアーティストへ統合する | B: `target_artist_id` | アーティスト（統合先） |
| `POST` | `/api/artists/aliases` | ログインのみ | 別名義を登録または提案する | B: `canonical, alias` | 編集権限あり `{applied:true}`、無し `{applied:false, suggestion_id}` |
| `GET` | `/api/itunes/search` | 未ログイン可 | iTunes で曲を探す | Q: `term`（必須） | `{results:[{itunes_id,track_name,artist_name,artwork_url,existing_song?,…}]}`。秘匿：existing_song の歌唱数を制限 |
| `GET` | `/api/itunes/{id}` | 未ログイン可 | iTunes ID の曲情報を取る | パス: 数値の iTunes ID | 曲情報＋`existing_song?`。秘匿：既存曲の歌唱数を制限 |

### 重複候補・否決

候補の歌唱数は非表示チャンネルを含むが、秘匿の視界には従う。統合の向きを決める材料のため。
走査は候補を追加するだけで、統合は曲の merge を人が呼ぶ。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/songs/merge-candidates` | `content:edit` | 未処理の統合候補を見る | Q: `limit` | `{candidates, total}`（total は返した件数）。秘匿：候補内の歌唱数を制限 |
| `GET` | `/api/songs/{id}/merge-candidates` | 未ログイン可 | この曲に関係する統合候補を見る | — | `{candidates}`。秘匿：候補内の歌唱数を制限 |
| `POST` | `/api/songs/merge-candidates/{id}/dismiss` | `content:edit` | 別の曲として統合候補を却下する | — | `{message}`。無い・処理済みなら 404 |
| `POST` | `/api/songs/merge-candidates/scan` | `content:edit` | 曲名キーと AI で重複候補を走査する | — | 202 / `{task_id, message}`。二重起動は 409。曲名キー→AI の順で背景処理。結果・失敗は GET /api/tasks/{id} で確認し、AI の一部失敗も status=failed |
| `POST` | `/api/songs/merge-candidates/adjudicate` | `content:edit` | 未判定候補の AI の見立てを取る | — | `{judged, message}`。1 回最大 30 件。統合はしない |
| `GET` | `/api/songs/identity-checks` | `content:edit` | 曲の照合で否決した組を見る | Q: `limit` | `{checks: [否決記録]}` |
| `POST` | `/api/songs/identity-checks/delete` | `content:edit` | 否決を取り消す | B: `pair_key`（制御文字を含むため body で渡す） | `{message}` |

候補は `id, score, reason, origin, new_song, existing_song, verdict?`。
各曲は名前・原曲アーティスト・歌唱数・iTunes ID・役割、verdict は同一曲・同一編曲の判断と理由を持つ。

### 読み仮名

アーティスト名と曲名を同じ端点で扱う。取り込みは id で照合し、片仮名は平仮名に変換する。
漢字が残る読みは採用せず、空文字は読みを消す意図として扱う。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/readings/stats` | `content:edit` | 未整備の読みの件数を見る | — | `{artists_total, artists_needs_fix, songs_total, songs_needs_fix}` |
| `GET` | `/api/readings/export` | `content:edit` | 読みを一括出力する | Q: `filter=needs_fix`、`format=csv` | 通常 `{artists:[{id,name,reading}], songs:[…]}`。csv は `text/csv` の添付（`type,id,name,reading`） |
| `POST` | `/api/readings/import` | `content:edit` | 読みを一括取り込みする | B: 書き出しと同じ JSON、または `Content-Type: text/csv` の CSV 本文 | `{artists_updated, songs_updated, skipped, errors}` |
| `POST` | `/api/ai/backfill-readings` | `content:edit` | AI で未整備の読みを補完する | — | 202 / `{task_id, message}`。二重起動は 409。各対象最大 30 件を背景で補完し、結果・失敗は GET /api/tasks/{id} で確認。一部失敗も status=failed |

## 歌唱・タグ

セットリスト全体の保存と歌唱 1 件の部分更新は別の操作。
`POST …/performances` は既存分も含むセットリストを送り直すので、1 件の修正には PUT を使う。
`singer_ids` は **歌った人**であり、配信の所有者・参加者の指定とは別。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/performances/{id}` | 未ログイン可 | 歌唱 1 件を読む | — | 歌唱の保存行＋関連情報（下記）。秘匿：権限が無ければ 404 |
| `PUT` | `/api/performances/{id}` | `content:edit` | 歌唱 1 件を部分更新する | B: `song_id, start_seconds, end_seconds, tags[], custom_tags[], singer_ids[]` | 歌唱の保存行＋関連情報（下記）。秘匿：権限が無ければ 404。重複 409、時刻不正 400 |
| `POST` | `/api/streams/{id}/performances` | `content:edit` | セットリストを保存する | B: `performances[]`（下記。1 曲以上） | `{created_count}` |
| `DELETE` | `/api/streams/{id}/performances` | `content:edit` | 配信の歌唱を全件削除する | — | `{success:true, message}` |
| `GET` | `/api/performances/random` | 未ログイン可 | 曲を重複させず歌唱をランダムに返す | Q: `limit`（既定 50、最大 100）、`exclude_song_ids`（UUID の CSV） | `{performances}`。秘匿：歌唱を制限。通常は非表示配信も除くが、restricted:view ではその条件を外す。表示中チャンネルの参加がない配信と members_only / unarchived タグは常に除外 |
| `GET` | `/api/performance-tags/{id}/performances` | 未ログイン可 | 歌唱タグが付いた歌唱を見る | Q: `page, limit` | `{performances, pagination}`。秘匿：歌唱・件数を制限 |
| `GET` | `/api/stream-tags/{id}/streams` | 未ログイン可 | 配信タグが付いた配信を見る | Q: `page, limit` | `{streams, pagination}`。通常の配信一覧と同じ表示範囲 |
| `GET` | `/api/stream-tags` | 未ログイン可 | 配信タグの定義を見る | — | `[{id, display_name, color, …}]` |
| `POST` | `/api/stream-tags` | `content:edit` | 配信タグの定義を作る | B: `id, display_name, color` | 201 / タグ |
| `DELETE` | `/api/stream-tags/{id}` | `content:edit` | 配信タグの定義を消す | — | `{message}`。予約タグ members_only は 409 |
| `GET` | `/api/performance-tags` | 未ログイン可 | 歌唱タグの定義を見る | — | `[{id, display_name, color, …}]` |
| `POST` | `/api/performance-tags` | `content:edit` | 歌唱タグの定義を作る | B: `id, display_name, color` | 201 / タグ |
| `DELETE` | `/api/performance-tags/{id}` | `content:edit` | 歌唱タグの定義を消す | — | `{message}` |

歌唱の GET / PUT 単件応答は `PerformanceWithDetails` の保存行＋関連情報で、
一覧の DTO と形が異なる。`arts, thumbnail_url` は `{String,Valid}`、`itunes_id` は `{Int64,Valid}`、
歌った人の項目にも nullable の保存形式があり、`youtube_url` は付かない。

保存する各歌唱は `name, original_artist, start_seconds, end_seconds, tags, singer_ids` と、
任意の `name_reading, original_artist_reading, art_url, itunes_id, custom_tags, end_source, end_confirmed`。
終了秒が 0 の場合は動画の最後まで扱う。終了時間の由来・確認状態は [データの補完](DATA_COMPLETION.md) を参照。

## 提案

投稿と自分の一覧はログインだけで使える。審査は `content:edit`。
一覧・件数には要求者の `restricted:view` を通す。自分の提案も対象が後から秘匿になれば伏せる。
承認・統合は、秘匿対象の中身を見られない利用者へ現在値や衝突内容を返さない。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `POST` | `/api/suggestions` | ログインのみ | 修正・未登録曲・曲の差し替えを提案する | B: `target_type, target_id, kind, fields, payload, song_swap, note`（下記） | 201 / `{message, id}`。秘匿：field / perf.meta の見られない歌唱は 404。未登録曲の報告は受け付ける。投稿制限 429 |
| `GET` | `/api/suggestions/mine` | ログインのみ | 自分の提案と審査結果を見る | Q: `status, page, limit` | `{suggestions, pagination}`。秘匿：提案・件数を制限 |
| `GET` | `/api/suggestions` | `content:edit` | 提案の審査一覧を見る | Q: `status=pending/conflict/approved/rejected, kind, page, limit, group=target` | 通常 `{suggestions, pagination}`、対象別 `{groups, pagination}`。秘匿：行・件数を制限 |
| `GET` | `/api/suggestions/count` | `content:edit` | 未処理提案の件数を見る | — | `{pending}`。秘匿：件数を制限 |
| `POST` | `/api/suggestions/{id}/approve` | `content:edit` | 提案を承認して反映する | Q: `force=1`。B: 任意の `payload`（審査時の修正） | `{message}`。競合 409 / `{error, conflicts:{欄:{expected,current}}}`。秘匿：見られない対象は 404 |
| `POST` | `/api/suggestions/{id}/reject` | `content:edit` | 提案を却下する | B: `note, not_this_song` | `{message}`。not_this_song は曲の照合候補の否決も記録 |
| `DELETE` | `/api/suggestions/{id}` | ログインのみ | 自分の未処理提案を取り下げる | — | `{message}`。他人の提案は 404（編集者は取り下げ可）、処理済みは 409 |
| `POST` | `/api/suggestions/{id}/undo-rejection` | `content:edit` | 却下とその否決記録を取り消す | — | `{message}`。状態不正は 409 |
| `POST` | `/api/suggestions/batch` | `content:edit` | 複数提案を承認・却下する | B: `ids[]`（1〜200）、`action=approve/reject, force, note` | `{succeeded, failed, results:[{id,ok,error?,conflict?}]}`。秘匿：承認の個別結果も視界に従う |
| `POST` | `/api/suggestions/merge` | `content:edit` | 同じ対象の提案を決着値に統合して反映する | B: `target_type, target_id, fields, ids[], note` | `{applied, approved, rejected}`。重複 ID は正規化後に 400。秘匿：見られない対象は 404 |
| `GET` | `/api/suggestions/settings` | `content:edit` | 時刻提案の自動適用条件を見る | — | `{enabled, min_votes, max_spread_seconds, max_delta_seconds}` |
| `PUT` | `/api/suggestions/settings` | `content:edit` | 時刻提案の自動適用条件を保存する | B: `enabled, min_votes, max_spread_seconds, max_delta_seconds` | 保存した設定。数値は範囲内に丸める |

`field`（既定）は `target_type=song/artist/performance` と `fields` の変更。
`perf.missing` は target_id 不要で、`payload` で `stream_id, song_name, original_artist, start_seconds,
end_seconds, song_id?, singer_ids?, tags?` などを渡す。
`perf.meta` は歌唱を対象に `song_swap` で `song_id`、または未登録曲の
`song_name, original_artist` と任意の読み・ジャケット・iTunes ID を渡す。
field の fields は変更する欄だけでよく、値は文字列（時刻も文字列）。
対象ごとの編集可能な欄は曲の `name, original_artist, name_reading`、
アーティストの `name, name_reading, aliases`、歌唱の `start_seconds, end_seconds, singer_ids`。
singer_ids は CSV、aliases は読点（、）区切り。原曲アーティストの読みは artist を対象に提案する。
force はスナップショット競合の解決に使う。処理済み提案を再承認するものではない。

## プレイリスト

公開範囲は `private/public/unlisted`（作成時の省略は private）。ID で他人が読めるのは public、unlisted は共有 slug が必要。
private は所有者だけに返し、他人には 404。変更・項目操作はログインだけでは足りず所有者であることが必要。
**項目・item_count は `restricted:view` があっても秘匿の歌唱を除く**（管理向けの視界へ広げない）。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/playlists/public` | 未ログイン可 | 公開プレイリストを一覧する | Q: `page, limit`（既定 50、最大 100） | `{playlists, total}`。unlisted は含まない |
| `GET` | `/api/playlists` | ログインのみ | 自分の全プレイリストを見る | — | `{playlists, total}` |
| `POST` | `/api/playlists` | ログインのみ | プレイリストを作る | B: `name, description, visibility` | 201 / プレイリスト |
| `GET` | `/api/playlists/{id}` | 未ログイン可 | プレイリスト詳細を見る | — | プレイリスト。公開範囲・所有者の判定あり |
| `PUT` | `/api/playlists/{id}` | ログインのみ | 名前・説明・公開範囲を更新する | B: `name, description, visibility` | プレイリスト。所有者のみ |
| `DELETE` | `/api/playlists/{id}` | ログインのみ | プレイリストを削除する | — | `{message}`。所有者のみ |
| `GET` | `/api/playlists/{id}/items` | 未ログイン可 | 収録歌唱を順に読む | — | `{performances}`。公開範囲・所有者の判定あり。常に公開の視界 |
| `POST` | `/api/playlists/{id}/items` | ログインのみ | 歌唱を末尾に追加する | B: `performance_ids[]`、互換の単値 `performance_id` | `{added, skipped}`。所有者のみ。既存項目は飛ばす |
| `DELETE` | `/api/playlists/{id}/items/{performanceId}` | ログインのみ | 歌唱をリストから外す | — | `{message}`。所有者のみ |
| `PUT` | `/api/playlists/{id}/order` | ログインのみ | 項目を並び替える | B: `performance_ids[]`（順序） | `{message}`。所有者のみ |
| `GET` | `/api/shared/playlists/{slug}` | 未ログイン可 | 共有リンクから詳細を見る | — | プレイリスト。private は所有者以外 404 |
| `GET` | `/api/shared/playlists/{slug}/items` | 未ログイン可 | 共有リンクから収録歌唱を見る | — | `{performances}`。private は所有者以外 404。常に公開の視界 |

### プリセット

プリセットはその時点の歌唱から作る。通常は非表示配信を除き、`restricted:view` ではその条件を外す。
どちらの視界でも、表示中のチャンネルが参加していない配信と、`members_only` / `unarchived` タグの
配信は除く（公開の裁定があっても再生できない可能性があるため）。個人プレイリストへの追加は公開の視界だけを使う。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/presets` | 未ログイン可 | プリセット一覧を見る | — | `{presets:[{key,name,description,item_count,is_following}]}`。秘匿：件数を制限 |
| `GET` | `/api/presets/followed` | ログインのみ | 自分がフォローしたプリセットを見る | — | `{presets}`。秘匿：件数を制限 |
| `GET` | `/api/presets/{key}` | 未ログイン可 | プリセット詳細を見る | — | プリセット。秘匿：件数を制限 |
| `GET` | `/api/presets/{key}/items` | 未ログイン可 | プリセットの歌唱を読む | Q: `limit` | `{performances}`。秘匿：歌唱を制限 |
| `POST` | `/api/presets/{key}/follow` | ログインのみ | プリセットをフォローする | — | `{message}` |
| `DELETE` | `/api/presets/{key}/follow` | ログインのみ | フォローを解除する | — | `{message}` |
| `POST` | `/api/presets/{key}/add` | ログインのみ | 自分のプレイリストへ現在の中身を追加する | B: 任意の `playlist_id, name`。空 body は新規作成 | 201 / `{playlist, added, skipped, created}`。既存への追加は所有者のみ |

## 同期・背景処理

### Holodex

読み取り同期は `sync:run`、外部へのセットリスト書き込みは **`holodex:upload`**。
後者は運用者の資格情報を使い、既定では admin だけが持つ。
送信した外部コピーは配信を秘匿に変えても残りうる。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `POST` | `/api/sync/holodex` | `sync:run` | チャンネルの配信を Holodex から同期する | B: `channel_id`（必須）、`limit`（既定 50）、`force_update` | `{synced_count,total_streams,processed,new_streams,updated,skipped,in_progress,message}` |
| `POST` | `/api/sync/holodex/video/{id}` | `sync:run` | 動画 1 本を同期する | — | `{synced_count,new_streams,updated,skipped,in_progress,…}`（チャンネル同期と同じ形） |
| `POST` | `/api/sync/holodex/to-holodex/{id}` | `holodex:upload` | セットリストを Holodex に送信・再送する | — | `{synced_count,updated,message,…}`（同期結果）。部分失敗は message も確認。`holodex_uploaded_at` は送信を試みた時刻で、成功の保証ではない |

### task_runs の実行記録

章節取得・拍手 end 補完・同期後の準備・読み仮名補完・重複候補走査の記録を残す。同じ種類が実行中なら開始は 409。準備は章節・拍手 end 補完とも相互排他。
終了しても実行記録と対象別の失敗理由は残る。サーバー再起動で実行中だった記録は interrupted になる。
停止 API は現在 **stream_prepare だけ**が対象。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/tasks` | `content:edit` | 背景処理の履歴を見る | Q: `limit`（既定 20、最大 100） | `{tasks:[背景処理]}` |
| `GET` | `/api/tasks/{id}` | `content:edit` | 背景処理の進捗・結果を見る | — | 背景処理。無ければ 404 |
| `POST` | `/api/tasks/{id}/cancel` | `content:edit` | 同期後の準備へ停止を要求する | — | 202 / `{message}`。対象・状態が不適切なら 409 |
| `POST` | `/api/streams/prepare` | `content:edit` | 章節取得→コメント再取得・プレ分析をまとめて始める | Q: `singer_id`（必須） | 202 / `{task_id}`。対象は所有配信のうち表示中・未処理・非秘匿 |
| `POST` | `/api/chapters/backfill` | `content:edit` | 未取得の章節を背景でまとめて取得する | Q: `concurrency`（既定 3） | 202 / `{message, concurrency, task_id}` |
| `POST` | `/api/chat-ends/backfill` | `content:edit` | コメント解析済み配信の拍手 end を背景で補完する | Q: `concurrency`（既定 3） | 202 / `{message, concurrency, task_id}` |

status は `running/done/failed/interrupted/cancelled`。件数は `done, succeeded, skipped, failed` を区別する。
開始応答の task_id を使って GET でポーリングする。失敗一覧は `{target, reason}` の配列（末尾 200 件まで）。
章節・拍手 end 補完の done は実行終了であり、全対象の成功ではない。failed の件数も見る。
読み仮名補完（kind=readings_backfill）と重複候補走査（kind=duplicate_scan）は、
AI・保存などの一部失敗も status=failed として終わる。既に保存した読み・候補は残る。
これら2種類の停止APIは無く、途中結果の件数・失敗理由は同じ実行記録で確認する。

### 一括作成・プレ分析・自動処理

一括作成は歌唱を書き、プレ分析は抽出キャッシュを整える。進捗 API と実行記録は別。
自動処理は定期同期・コメント再取得・セットリスト作成を回す。所有配信が既定で、include_collabs でゲスト参加も対象にする。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `POST` | `/api/streams/batch-fill` | `content:edit` | セットリストの一括作成を始める | B: `mode=unprocessed/force, singer_ids[], include_collabs`（旧 `singer_id` も可） | `{run_id, message}`。既に実行中なら 409 |
| `POST` | `/api/streams/batch-fill/cancel` | `content:edit` | 一括作成へ停止を要求する | — | `{message}` |
| `GET` | `/api/streams/batch-fill/status` | `content:edit` | 一括作成の進捗を見る | — | `{running,run_id,phase,total,done,created,review,gaps,skipped,skipped_ids,ai_asked,current,message,…}` |
| `GET` | `/api/streams/batch-fill/runs` | `content:edit` | 一括作成の実行履歴を見る | Q: `limit`（既定 20） | `{runs:[{id,mode,status,streams_total,streams_done,songs_created,songs_review,songs_gap,skipped_stream_ids,ai_asked,message,…}]}` |
| `POST` | `/api/streams/batch-fill/runs/{id}/revert` | `content:edit` | この実行が作った歌唱を撤回する | — | `{deleted, message}` |
| `GET` | `/api/streams/batch-fill/runs/{id}/gaps` | `content:edit` | DB にあるが入力元に無かった歌唱を見る | — | `{gaps}`。秘匿：歌唱の中身を制限 |
| `POST` | `/api/streams/batch-analyze` | `content:edit` | 一括プレ分析を始める | B: `mode=unanalyzed/unprocessed/refresh/reanalyze, singer_id, hidden=false/true/all`。空 body 可。Q: `mode, hidden` は互換用 | 202 / `{message}`。既定 unprocessed・表示中。競合 409。未知キー・null は 400 |
| `POST` | `/api/streams/batch-analyze/cancel` | `content:edit` | プレ分析へ停止を要求する | — | `{message}` |
| `GET` | `/api/streams/batch-analyze/status` | `content:edit` | プレ分析の進捗を見る | — | `{running,mode,singer_id,hidden,total,done,failed,deferred,marked_processed,failed_ids,current,message}` |
| `GET` | `/api/auto-fill/settings` | `content:edit` | 自動処理の設定と直近結果を見る | — | `{enabled,interval_hours,refresh_days,include_collabs,last_run_at,last_run_note,last_run_error,last_skipped_at,last_skip_note}` |
| `PUT` | `/api/auto-fill/settings` | `content:edit` | 自動処理の設定を更新する | B: `enabled, interval_hours, refresh_days, include_collabs`（全て必須） | 保存した設定＋直近結果 |
| `POST` | `/api/auto-fill/run` | `content:edit` | 自動処理を今すぐ 1 回走らせる | — | `{channels,synced,refreshed,fill_run_id?,failures,note?}`。設定が無効でも実行する。競合 409 |

## 管理・解析

### 配信の解析素材と結果

下表で「秘匿：403」とある端点は `content:edit` に加え、秘匿配信では `restricted:view` が必要。
開始時点で秘匿かつ権限不足なら、YouTube・Holodex・yt-dlp の取得・解析より前に 403 にする。
取得・解析中に秘匿へ変わった場合も応答前に検査して中身を伏せる（この場合は外部取得済みのことがある）。
GET でもキャッシュが無ければ外部取得するものがある。編集フォームへ取り込む解析は
[セットリストを埋める流れ](SETLIST_FLOW.md) を参照。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/streams/{id}/holodex-songs` | `content:edit` | Holodex のセットリストを読み込む | — | `{stream_id,stream_title,channel_owner,participants,songs}`。秘匿：403 |
| `POST` | `/api/streams/{id}/holodex-songs/analyze` | `content:edit` | Holodex の曲を正規化・照合・終了時刻補完する | Q: `force=true` | `{songs}`。秘匿：403 |
| `GET` | `/api/streams/{id}/comments` | `content:edit` | 生コメントを取得する | — | `{video_id, comments}`。秘匿：403 |
| `POST` | `/api/streams/{id}/comments/sync-youtube` | `content:edit` | YouTube からコメントを取り直す | — | `{video_id, comment_count}`。秘匿：403 |
| `POST` | `/api/streams/{id}/comments/analyze` | `content:edit` | コメントから曲を抽出・正規化・照合する | Q: `force=true, dry_run=true` | 解析結果。秘匿：403。入力の差し替え競合 409。dry_run は保存・学習をしない |
| `GET` | `/api/streams/{id}/chapters` | `content:edit` | 章節を取得する（未取得なら yt-dlp） | — | `{video_id, chapters}`。秘匿：403 |
| `POST` | `/api/streams/{id}/chapters/sync` | `content:edit` | yt-dlp で章節を取り直す | — | `{video_id, chapter_count, chapters}`。秘匿：403 |
| `POST` | `/api/streams/{id}/chapters/analyze` | `content:edit` | 章節から曲を抽出する | Q: `force=true` | 解析結果。秘匿：403 |
| `POST` | `/api/streams/{id}/analyze-chat-ends` | `content:edit` | 拍手から既存のコメント解析の終了時刻を補完する | — | 同期 / `{id, total, filled, changed}`。AI は呼ばない。秘匿：403 |
| `POST` | `/api/streams/{id}/chat-end-estimate` | `content:edit` | 指定した開始時刻の拍手 end を推定する | B: `starts[]`（秒、1 件以上） | `{ends: {開始秒: 終了秒}}`。秘匿：403 |
| `POST` | `/api/streams/{id}/estimate-end-times` | `content:edit` | 入力の曲の終了時刻を推定する | B: `songs[{start,end,name,artist,itunes_id?,next_start?,stream_end?}], stream_end, stream_title` | `{estimates:[{estimated_end,is_end_time_estimated,method,reason?,…}], message?}`。入力だけの推定で、配信の保存済み素材は返さない |
| `POST` | `/api/ai/normalize` | `content:edit` | 入力した曲を AI で正規化・照合する | B: `items[{name,original_artist,art_url?,itunes_id?}]`（1 曲以上） | `{suggestions:[正規化・照合結果], warning?}` |
| `POST` | `/api/comments/backfill` | `content:edit` | 生コメントがあり解析結果が無い配信を補完する | — | 同期 / `{message, count}` |
| `POST` | `/api/comments/backfill-hashes` | `content:edit` | 抽出キャッシュの状態を監査する | — | `{total,migrated,already_ok,skipped,needs_reanalysis}`。migrated/skipped は互換用の 0。書き換えない |

章節は開始・終了秒と見出しを持つ。解析結果の曲は `start, end, name, original_artist,
original_comment, normalized_name, normalized_artist, tags, matched_song_id, match_candidates, changes` など。
`changes` はどの処理が値を書き換えたかを示す。解析は歌唱の保存とは別。

### 手動取り込み

メンバー資格のある人が手元で得た yt-dlp の出力を持ち込む。
POST は素材の保存であり、歌唱を作る操作ではない。配信が未登録なら 404。
削除はキャッシュファイルだけで、既に反映した解析結果を巻き戻さない。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `POST` | `/api/streams/{id}/import/info-json` | `content:edit` | info.json のコメントを取り込む | F: `file`（リクエスト上限 32 MiB） | `{video_id,title,duration_seconds,total,top_level,saved,with_times}`。ID 不一致などは 400 |
| `POST` | `/api/streams/{id}/import/live-chat` | `content:edit` | live_chat.json をキャッシュへ取り込む | F: `file`（リクエスト上限 64 MiB） | `{messages,applause,first_at_sec,last_at_sec,bytes}` |
| `GET` | `/api/streams/{id}/import/live-chat` | `content:edit` | キャッシュ済みチャットの要約を見る | — | `{present, chat}`。秘匿：403 |
| `DELETE` | `/api/streams/{id}/import/live-chat` | `content:edit` | キャッシュ済みチャットを消す | — | `{deleted:true}` |

### 非表示・裁定の見直し

visibility-review は非表示かつ音楽系タグあり・180 秒超・「歌回でない」の判断なしを候補にする。
preview は前値・時刻の退避だけ、apply が `is_hidden` を false にする。
書き込み直前に条件・前値・更新時刻を再検査し、変わっていれば全体を 409 にする。
revert は後から編集・同期された行を飛ばす。秘匿の検出・裁定は変更しない。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/visibility-review` | `content:edit` | 表示に戻せる候補、または判断済み配信を見る | Q: `limit`（既定 100、最大 500）、`offset, dismissed=true` | `{candidates:[{id,title,stream_date,duration_seconds,tags}], total}`。配信メタデータだけ |
| `POST` | `/api/visibility-review/preview` | `content:edit` | 選んだ配信の変更前の状態を退避する | B: `stream_ids[]`（1〜500、重複不可。body 上限 64 KiB） | `{run_id, count}`。候補条件を満たさなければ 409 |
| `POST` | `/api/visibility-review/runs/{id}/apply` | `content:edit` | 選んだ候補を表示に戻す | — | `{changed}`。preview 後の変更・状態不正は 409 |
| `POST` | `/api/visibility-review/runs/{id}/revert` | `content:edit` | この実行の表示変更を取り消す | — | `{reverted, skipped}`。後の編集・同期がある行は飛ばす |
| `GET` | `/api/visibility-review/runs` | `content:edit` | 見直しの実行履歴を見る | — | `{runs:[{id,status,item_count,reverted_count,created_at,applied_at,reverted_at}]}` |
| `GET` | `/api/non-singing-candidates` | `content:edit` | 非表示だが解析で曲が出た配信を見る | Q: `limit`（既定 100）、`dismissed=true` | `{candidates:[{id,title,stream_date,song_count,analyzed_at,tags}], total}`。秘匿：行・件数を制限 |
| `POST` | `/api/non-singing-candidates/{id}/dismiss` | `content:edit` | 歌回ではないという判断を記録する | B: 任意の `note` | `{id, dismissed:true}` |
| `DELETE` | `/api/non-singing-candidates/{id}/dismiss` | `content:edit` | 歌回ではないという判断を取り消す | — | `{id, dismissed:false}` |
| `GET` | `/api/restriction-review` | `content:edit` | 公開の裁定と現在の検出が食い違う配信を見る | Q: `limit`（既定 100、最大 500） | `{items:[{id,title,stream_date,basis_unknown}], total}`。配信メタデータだけ |

### タグ漏れ・抽出の規則

タグ漏れは解析キャッシュと歌唱の差を毎回計算する。付ける操作は歌唱の PUT を使う。
配信タイトル規則の backfill はタグを追加するが、初回登録後の is_hidden は再判定しない。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/tag-gaps` | `content:edit` | タグ漏れと「付けない」とした組を見る | Q: `limit` | `{gaps, dismissed}`。秘匿：両方の行を制限 |
| `POST` | `/api/tag-gaps/dismiss` | `content:edit` | この歌唱にこのタグは付けないと記録する | B: `performance_id, tag_id` | `{message}` |
| `POST` | `/api/tag-gaps/undismiss` | `content:edit` | タグを付けないという判断を取り消す | B: `performance_id, tag_id` | `{message}` |
| `GET` | `/api/filter-keywords` | `content:edit` | コメント抽出の除外・保持キーワードを見る | — | `[{id,keyword,type,…}]` |
| `POST` | `/api/filter-keywords` | `content:edit` | 除外・保持キーワードを作る | B: `keyword, type=filter/keep` | 201 / キーワード |
| `DELETE` | `/api/filter-keywords/{id}` | `content:edit` | キーワードを削除する | パス: 整数 ID | `{message}` |
| `GET` | `/api/tag-keyword-rules` | `content:edit` | 配信タイトルのタグ付け規則を見る | — | `[{id,tag_id,keyword,…}]` |
| `POST` | `/api/tag-keyword-rules` | `content:edit` | タグ付け規則を作る | B: `tag_id, keyword` | 201 / 規則 |
| `DELETE` | `/api/tag-keyword-rules/{id}` | `content:edit` | タグ付け規則を削除する | パス: 整数 ID | `{message}` |
| `POST` | `/api/tag-rules/backfill` | `content:edit` | 全配信へタイトル規則を適用する | — | `{message, added}` |

### AI・外部サービスの設定

設定の応答は機密の値そのものを返さず、設定済みか・末尾のヒント・由来だけを返す。
連携設定の secrets の空文字は変更なし。消す場合は clear に名前を指定する。
詳細は [外部 API](EXTERNAL_APIS.md)。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/ai-providers` | `ai:manage` | AI プロバイダー設定を一覧する | — | `[{id,name,base_url,model,enabled,priority,timeout_seconds,has_key,key_hint?}]` |
| `POST` | `/api/ai-providers` | `ai:manage` | AI プロバイダーを登録する | B: `name, base_url, model, api_key`（必須）、`enabled`（既定 true）、`priority, timeout_seconds`（既定 60） | 201 / プロバイダー（api_key 自体は返さない） |
| `PUT` | `/api/ai-providers/{id}` | `ai:manage` | AI プロバイダーを部分更新する | B: `name, base_url, model, api_key, enabled, priority, timeout_seconds` | プロバイダー。api_key の空文字は変更なし |
| `DELETE` | `/api/ai-providers/{id}` | `ai:manage` | AI プロバイダーを削除する | — | `{message}` |
| `GET` | `/api/ai-providers/{id}/models` | `ai:manage` | 保存済みの鍵でモデル一覧を取る | — | `{models}`。外部側の失敗は 502 |
| `POST` | `/api/ai-providers/models/preview` | `ai:manage` | 未保存の設定でモデル一覧を取る | B: `base_url, api_key` | `{models}`。保存しない。外部側の失敗は 502 |
| `GET` | `/api/settings/integrations` | `users:manage` | 外部サービス連携の設定状態を見る | — | `{encryption_enabled,secrets:{名前:{configured,hint,from_env}},plain,plain_from_env}` |
| `PUT` | `/api/settings/integrations` | `users:manage` | 外部サービス連携の設定を更新する | B: `secrets:{名前:値}, clear:[名前], google_drive_client_id, google_signin_client_id` | 設定状態。暗号化キー無しでの機密保存は 412 |

### バックアップ

DB ダンプの作成・復元と Drive 連携。リストアは DB 全体を置き換える管理操作。
この API の Google Drive デバイスフローと、利用者ログイン用の OAuth は別。

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/api/backups` | `backup:manage` | 設定・ローカルファイル・Drive 状態を見る | — | `{settings,backups:[{name,size,modified_at}],gdrive,instance}` |
| `POST` | `/api/backups` | `backup:manage` | 今すぐバックアップを作る | — | `{name,size,drive_uploaded,drive_error?}`。Drive 失敗は別に記録 |
| `PUT` | `/api/backups/settings` | `backup:manage` | 自動バックアップ設定を保存する | B: `auto_enabled, interval_hours, retention_local, retention_drive, drive_upload` | 保存した設定 |
| `POST` | `/api/backups/restore-upload` | `backup:manage` | 添付したダンプから DB を復元する | F: `file` | `{message}` |
| `GET` | `/api/backups/{name}/download` | `backup:manage` | ローカルダンプをダウンロードする | パス: ファイル名 | `application/octet-stream` の添付（ServeFile の Range / 条件付き要求も可） |
| `POST` | `/api/backups/{name}/restore` | `backup:manage` | ローカルダンプから DB を復元する | — | `{message}` |
| `POST` | `/api/backups/{name}/upload-drive` | `backup:manage` | 既存ダンプを Drive に送る | — | `{message}` |
| `DELETE` | `/api/backups/{name}` | `backup:manage` | ローカルダンプを削除する | — | `{message}` |
| `POST` | `/api/backups/gdrive/auth/start` | `backup:manage` | Drive のデバイス認証を始める | — | `{device_code,user_code,verification_url,expires_in,interval}` |
| `POST` | `/api/backups/gdrive/auth/poll` | `backup:manage` | デバイス認証の承認を確認する | B: `device_code` | `{connected,gdrive}` |
| `DELETE` | `/api/backups/gdrive` | `backup:manage` | バックアップ用の Drive 連携を解除する | — | `{message}` |
| `GET` | `/api/backups/gdrive/files` | `backup:manage` | Drive のバックアップを一覧する | — | `{files:[{id,name,size,createdTime,mimeType}]}` |
| `DELETE` | `/api/backups/gdrive/files/{id}` | `backup:manage` | Drive のバックアップを消す | パス: Drive ファイル ID | `{message}` |
| `POST` | `/api/backups/gdrive/files/{id}/restore` | `backup:manage` | Drive のダンプから DB を復元する | パス: Drive ファイル ID | `{message}` |

### 稼働・検索・訪問記録

| メソッド | パス | 必要な権限 | 何をするか | 主な入力 | 応答の要点 |
|---|---|---|---|---|---|
| `GET` | `/health` | 未ログイン可 | バックエンド内部でプロセスの稼働を確認する（DB は確認しない） | — | `{status:"ok"}`。Caddy の公開経路では SPA に届くため外部監視には使わない |
| `GET` | `/api/health` | 未ログイン可 | DB の疎通・版・起動からの時間を確認する | — | `{status,commit,uptime_seconds}`。`SELECT 1` を1秒まで実行し、正常は200 / `status:"ok"`、失敗・タイムアウトは503 / `status:"unavailable"`。秒数は整数。認証不要で Bearer は参照せず、接続文字列・設定・エラー詳細を返さない。`Cache-Control: no-store` |
| `GET` | `/api/version` | 未ログイン可 | 稼働中のビルドを確認する | — | `{commit,built_at}` |
| `GET` | `/api/search` | 未ログイン可 | 曲・配信・チャンネル・アーティスト・タグを横断検索する | Q: `q`（必須）、`limit`（既定 5、1〜20） | `{query,songs,streams,singers,artists,stream_tags,performance_tags,video_id?,video_registered?}`。動画 ID / URL は登録確認だけ。秘匿：曲の歌唱数・歌唱タグ件数を制限 |
| `GET` | `/api/logs` | `logs:view` | 最近のサーバーログを見る | Q: `limit`（既定 100、最大 1000） | `{logs,level}` |
| `PUT` | `/api/logs/level` | `logs:view` | サーバーのログレベルを変える | B: `level=DEBUG/INFO/WARN/ERROR` | `{level}` |
| `POST` | `/api/activity/visit` | 未ログイン可 | ページ訪問を記録する | B: `path`（body 上限 2 KiB）。ブラウザの Origin は許可された host に限る | **204 / 本文なし**。記録障害でも 204 |
| `GET` | `/api/activity/policy` | 未ログイン可 | 訪問記録の保存期間を見る | — | `{retention_days}` |
| `GET` | `/api/activity` | `users:manage` | 訪問記録を検索・一覧する | Q: `days`（既定 7）、`page, limit`（既定 50、最大 100）、`kind, q` | `{activity,total,page,limit,retention_days}` |
| `GET` | `/api/activity/stats` | `users:manage` | 日別の訪問統計を見る | Q: `days`（既定 7） | `{stats,retention_days}` |
| `GET` | `/api/activity/users` | `users:manage` | 利用者別の訪問集計を見る | Q: `days`（既定 30） | `{users,retention_days}` |

## 文書の更新

端点を追加・削除・移動するときは、この文書の表と認可監査表を同じ変更で更新する。
表は **メソッド・パス・必要な権限・何をするか・主な入力・応答の要点の 6 列**。
メソッドとパスをバッククォートで囲み、端点ごとに 1 行を書く。表の行頭は `|` に揃え、
本文の表として置く（コード例・HTMLコメントの中には置かない）。入力が無ければ `—` と書く。

`TestAPIDocumentation` はこの文書を期待値として読み、handler の全ファイルにある
ServeMux 登録とメソッド・パスを双方向に突き合わせる。
登録だけ足す・文書だけ残す・重複する・必須の列を空にする場合は落ちる。
記載したルートが実際の ServeMux でも選ばれることと、認可関数の結果が権限欄と完全一致することも検査する。
コードブロック・HTMLコメント内の表は文書の一覧として数えない。
読み仮名補完・重複候補走査は、文書の成功コード・JSONキー・二重起動コードを実際のhandler応答とも照合する。
それ以外のパラメータ・応答の説明の意味までは検証しないので、handler・DTO・呼び出すサービスを確認して更新する。

```sh
cd backend
go test ./internal/handler -run '^TestAPIDocumentation$'
```
