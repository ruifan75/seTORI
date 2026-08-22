# 外部 API 用途一覧

> デプロイモデル：**シングルテナント、運用者の key をすべて使用**。
> 課金や書き込みが発生する操作は RBAC の権限で保護されています（最終セクション参照）。
> 本ドキュメントは 2026-08-09 時点のコードに基づきます。
>
> ⚠️ **key の置き場所は `.env` ではなく DB です。** `管理 → 設定 → 外部サービス連携`
> から登録すると `app_settings` に AES-256-GCM で暗号化して保存され、再起動なしで
> 各クライアントへ反映されます。下表の環境変数は**未設定時のフォールバック**でしかなく、
> 本番では使っていません（`internal/service/settings_service.go`）。

## 概要

| API | 環境変数（フォールバック） | 方向 | 主な用途 | 課金 / 制限 | 運用時の注意 |
|-----|----------|:---:|----------|------------|----------------------|
| Holodex（読） | `HOLODEX_API_KEY` | 読 | チャンネル/動画/セットリスト、コメント fallback | 無料、~80 req/2min | 🟡 共有可（rate limit 依存） |
| Holodex Editor | `HOLODEX_EDITOR_TOKEN` | **書** | seTORI セットリストを Holodex にアップロード | あなたの Holodex アカウントに紐づく | 🔴 **管理者限定必須** |
| Groq | `GROQ_API_KEY` | LLM | 曲名正規化、コメントのハイブリッド解析 | トークン課金 / 制限あり | 🔴 **管理者またはクォータ管理** |
| YouTube Data | `YOUTUBE_API_KEY` | 読 | 公開コメント、チャンネル高解像度アバター取得 | 10,000 units/日/プロジェクト | 🟡 共有可 |
| iTunes | （key 不要） | 読 | 楽曲メタデータ、再生時間、カバー | ~20 req/min（Apple） | 🟢 key 不要で安心 |

---

## 1. Holodex API（読） — `HOLODEX_API_KEY`

- **コード位置**：`pkg/holodex/client.go`（認証ヘッダ `X-APIKEY`）、クライアント側 rate limiter は 75 req/2min に設定。
- **実際に呼び出すエンドポイント**：
  - `GET /channels/{id}` — チャンネル情報（名前、英語名、アバター、所属）
  - `GET /videos/{id}` — 動画詳細。`?c=1` は YouTube コメントが未設定・取得失敗・空の場合の fallback。パラメータなし時は `songs` + `channel` + `mentions` を含む
  - `GET /videos?channel_id=&type=stream&status=past&include=songs,mentions` — ページング（50/ページ）でチャンネルの過去歌枠を取得
- **呼び出しトリガー**：
  - `POST /api/sync/holodex`（チャンネル全体同期）
  - `POST /api/sync/holodex/video/{id}`（単一動画同期）
  - `GET /api/streams/{id}/holodex-songs`（セットリスト即時読込）
  - `GET /api/streams/{id}/comments`、`POST /api/streams/{id}/comments/analyze`（コメント取得）
  - `POST /api/singers`（チャンネル情報のみ同期）
- **topic と表示状態**：Holodex の topic ID と seTORI の tag ID は同一とは限らない。
  `Original_Song` は `original_song`、`Music_Cover` は `music_cover` へ正規化する。
  topic とタイトルキーワード規則を両方タグへ反映したあと、`concert` / `karaoke` /
  `music_cover` / `mv` / `original_song` / `singing` のどれかを持つ配信は既定で表示する。
  ただし topic またはタグが `shorts` で動画長が 180 秒以下（長さ不明を含む）なら、
  音楽タグより短尺判定を優先して非表示にする。長い `#shorts` 付き歌枠は表示する。
  この判定は初回登録時だけ行い、以後は `is_hidden` を手動編集する。再同期は表示状態を
  上書きせず、タグが変わっても再判定しない。
  詳細な優先順位と実例は [`STREAM_VISIBILITY.md`](./STREAM_VISIBILITY.md)
- **備考**：`GetChannelVideos`、`SearchVideos` は定義済みだが未使用。

## 2. Holodex Editor Token（書） — `HOLODEX_EDITOR_TOKEN`

- **コード位置**：`internal/service/holodex_service.go` の `SyncSetoriToHolodex`。
- **動作**：各 performance に対して `PUT https://holodex.net/api/v2/songs` を発行。`Authorization: Bearer <editor_token>` と `Origin: https://holodex.net` を付与し、楽曲（iTunes メタデータ含む）を Holodex にアップロード。既存曲と照合して重複を避ける。
- **呼び出しトリガー**：`POST /api/sync/holodex/to-holodex/{id}`
- **🔴 これは Holodex が公開している API ではありません。**
  `PUT /api/v2/songs` は Holodex のウェブ UI が内部で叩いている経路で、
  仕様が公表されているものではありません。リクエストに
  `Origin: https://holodex.net` を付けているのがその証拠で、
  ブラウザからの操作に見せかけて呼んでいます。したがって：
  - **予告なく変わる・消える。** 動かなくなっても直す手段はこちら側にありません
  - **Holodex 側の規約に反する可能性があります。** 咎められた場合に
    「知らなかった」は通りません
  - **アカウントが制限される可能性があります。** 書き込みは
    `HOLODEX_EDITOR_TOKEN` の持ち主、つまり**運用者本人の名義**で残ります
  - サポートも rate limit の保証もありません

- **⚠️ 運用方針**：**管理者だけが使う機能**として扱ってください。
  そして**仕組みと上のリスクを理解しないうちは実行しないでください**。
  seTORI 側から取り消す手段はありません（Holodex 上で個別に直すことになります）。
  未設定またはプレースホルダー文字列の場合はエラーを返して実行しません。

- 🔴 **権限が足りていません（TODO 35、優先度：高）**：この書き込みは読み取り側の
  同期と同じ `sync:run` で通り、そして **`sync:run` は system role の `editor` に
  既定で入っています**。つまり誰かを editor にした時点で、その人は運用者の名義で
  Holodex へ書き込めるようになります。**誰かを editor にする前に権限を分けること。**

## 3. Groq API（LLM） — `GROQ_API_KEY`

- **複数 provider failover**：**管理画面（設定ページ → AIプロバイダー管理）** で複数の OpenAI 互換プロバイダー（Groq / Gemini / Cerebras / OpenRouter / Ollama…）を登録可能。`internal/service/ai_service.go` は **厳密な優先順 + failover** で呼び出し：常に priority が最小（先頭）の provider を優先し、429/5xx 失敗時は次に切り替え。失敗した provider は 60 秒間クールダウンしてスキップします（使用制限に何度も当たらないため）。プロバイダーが未登録の場合は環境変数 `GROQ_API_KEY` にフォールバック（後方互換）。provider 設定は `ai_providers` テーブルに保存され、API キーはフロントに返しません（末尾4桁のみ）。
- **コード位置**：`pkg/ai/groq.go` — OpenAI 互換 `POST {base_url}/chat/completions`。デフォルトは Groq（model `llama-3.3-70b-versatile`）、temperature 0.1、max_tokens 2048。
- **用途と呼び出しトリガー**：
  1. **曲名正規化**：`internal/service/normalization_service.go` の `BatchAINormalization` — バッチで元の曲名を正規化（演出版表記除去、読み仮名生成、バージョンタグ検出）。失敗時は純粋な DB 照合に降格。トリガー：`POST /api/ai/normalize`。
  2. **コメント hybrid 解析**：`internal/service/comment_service.go` の `AnalyzeComments` → `pkg/comment/ai_parser.go` の `ParseCommentsWithAI` — **AI がどの行が楽曲で start/end 秒数を担当**、正規表現が曲名/アーティスト文字列を抽出（LLM が日本語曲名を書き換えないように）。edit 時（ユーザーが「分析」を押したとき）に実行。key 未設定または AI 失敗時は自動で純粋正規表現に戻る。トリガー：`POST /api/streams/{id}/comments/analyze`。
- **⚠️ リスク**：トークン課金。公開環境であなたの key を使うと、1 ユーザーで請求を爆発させたりクォータを消費し尽くす可能性があります。key 未設定時は AI ステップがフォールトトレラント（生データ / 純粋正規表現結果を返却）。
- **将来の強化**：AI に曲名/アーティストも返してもらうことは可能ですが、「verbatim 部分文字列検証」のガードレールを付けてから採用し、さもなくば正規表現分割に戻してください。

## 4. YouTube Data API（読、任意） — `YOUTUBE_API_KEY`

- **コード位置**：`pkg/youtube/client.go`。
  - `GET /youtube/v3/commentThreads?part=snippet&videoId=...` — 公開トップレベルコメントを `maxResults=100`、`textFormat=plainText` で全ページ取得。
  - `GET /youtube/v3/channels?part=snippet&id=` または `forHandle=` — チャンネル情報を取得（アバターは high → medium → default 優先）。
- **用途**：一般動画コメントは YouTube を優先し、未設定・取得失敗・空の場合に Holodex へ fallback する。編集ページの手動同期は YouTube のみを使用し、Holodex へ fallback しない。空の `comment_raw` は有効な永続キャッシュと見なさず、次回アクセス時に再取得する。チャンネル/歌手同期では YouTube 高解像度アバターを優先し、Holodex 未登録チャンネルの追加にも利用する。
- **呼び出しトリガー**：`POST /api/sync/holodex`、`POST /api/sync/holodex/video/{id}`、`GET /api/streams/{id}/comments`、`POST /api/streams/{id}/comments/sync-youtube`（YouTube 限定の手動同期）、`POST /api/streams/{id}/comments/analyze`、`POST /api/singers`。
- **制限**：デフォルトでプロジェクトあたり 10,000 units/日。`commentThreads.list` と `channels.list` は 1 request あたり 1 unit（コメントは 100 件ごとに 1 request）。取得するのはトップレベルコメントのみで、live chat replay は従来どおり yt-dlp を使用する
（拍手による終了時間の推定。BOT 判定を避けるための cookie 設定は `docs/DATA_COMPLETION.md`）。

### 会限（メンバー限定）は Data API では判別できない

実測（2026-08-21）。`part=status,contentDetails` の応答が**会限と公開で完全に同一**：

```
会限: {"embeddable":true, ..., "privacyStatus":"public", ...}
公開: {"embeddable":true, ..., "privacyStatus":"public", ...}
```

`embeddable: true` と言っているが、実際には IFrame API が `onError: 150`
（所有者が埋め込みを許可していない）で弾く。**この用途に Data API は使えない。**

判別できるのは **yt-dlp の `availability`**。cookie を付けて実行すると：

| 動画 | cookie なし | cookie あり | 状態 |
|---|---|---|---|
| 会限 | ERROR（members on level …） | **`subscriber_only`** | 会限 |
| 公開 | `public` | `public` | 正常 |
| 削除・非公開 | `Video unavailable` | `Video unavailable` | 取得不可 |

live chat とチャプターで yt-dlp は元から叩くので、**追加の呼び出しは発生しない**。
取得不可は「不確実」なので、公開しない側へ倒すこと。

Holodex の `topic_id = "membersonly"` は候補の絞り込みに使えるが、**単値**なので
`singing` などと排他になる（実測 `singing` 409 / `membersonly` 85）。バックエンドは非公開で
指定ロジックは読めない。Holodex 自身、TLdex が会限配信で表示される不具合を出しており、
その修正は「topic を正しく見る」ではなく **「実際に再生できるかを見る」** だった
（`Holodex/src/components/watch/WatchLiveChat.vue` の `currentTime > 0`、および CHANGELOG）。

→ **Holodex は候補抽出に、`availability` は判定に**、と役割を分ける。設計は issue #3。

## 5. iTunes Search / Lookup API（key 不要）

- **コード位置**：`pkg/itunes/client.go` — `GET itunes.apple.com/search`（`entity=song&country=JP&limit=10`）と `GET .../lookup?id=`（`country=JP&lang=ja_jp`）。30s タイムアウト付き。
- **用途と呼び出しトリガー**：
  - iTunes Track 紐付け、ジャケット/アルバム取得：`GET /api/itunes/search`、`GET /api/itunes/{id}`
  - 再生時間（`trackTimeMillis`）を使った歌唱終了時間推定：`POST /api/streams/{id}/estimate-end-times`（`end_time_estimate_service.go`）
  - Holodex アップロード前に楽曲メタデータを補完：`POST /api/sync/holodex/to-holodex/{id}`（`holodex_service.go`）
- **制限**：Apple は key 不要ですが rate limit あり（通常約 20 req/min）。

---

## 課金・書き込みが発生するルートの保護

**すでに RBAC で保護されています。** 以前あった `API_AUTH_TOKEN`（非 GET を
まとめて Bearer トークンで塞ぐ方式）と `authorized()` ミドルウェアは廃止され、
DB セッション + `roles.permissions` に置き換わりました。判定は
`internal/handler/router.go` の `requiredPermission` 一箇所にあります。

| ルート | 何が起きるか | 必要な権限 |
|---|---|---|
| `POST /api/sync/holodex/to-holodex/{id}` | **あなたの Holodex アカウント名義**で書き込み | `sync:run` |
| `POST /api/sync/holodex`、`.../video/{id}` | Holodex / YouTube のクォータを消費 | `sync:run` |
| `POST /api/ai/normalize` | AI プロバイダーの課金 | `content:edit` |
| `POST /api/streams/{id}/comments/analyze` | AI プロバイダーの課金 | `content:edit` |
| `/api/ai-providers/*` | API キーの登録・変更 | `ai:manage` |
| `/api/backups/*` | ダウンロード（GET）含む全操作 | `backup:manage` |
| `GET /api/streams/{id}/comments` | Holodex / YouTube のクォータを消費（キャッシュが無いとき） | `content:edit` |
| `GET /api/streams/{id}/chapters` | **運用者のメンバー cookie 付きで yt-dlp を起動**（同上） | `content:edit` |
| `GET /api/streams/{id}/holodex-songs` | Holodex を叩く（**キャッシュを見ずに毎回**） | `content:edit` |
| `/api/streams/batch-analyze/*` | AI プロバイダーの課金。status に非表示配信の題名と ID | `content:edit` |

方針は「読み取りは基本公開、書き込みはログイン + 権限、管理系リソースは
読み取りも専用権限」。匿名で通るのは閲覧と、限定公開プレイリストの共有リンクだけです。

> ⚠️ **GET だからといって安全とは限りません。** 上の 3 つの GET はどれも
> キャッシュが無いと外部へ取りに行き、運用者の API キーやメンバー cookie を使います。
> 2026-08-22 まで既定の「安全メソッドは公開」に落ちており、未ログインの
> `GET /api/streams/{id}/chapters` が会限配信に対して yt-dlp を起動していました
> （実測 3.76 秒、結果を保存して返す）。**副作用や資格情報の利用がある GET を足すときは、
> `requiredPermission` に明示的な行を書くこと。**
>
> 判定はルートの形で行います（`isRouteOrSubpath` / `isStreamSubresource`）。
> 単純な prefix / suffix にすると、将来 `/api/streams/batch-analyze-report` のような
> 別ルートを巻き込んだり、`/api/streams/{id}/comments/raw` のようなサブリソースを
> 素通りさせたりします。認可は ServeMux より前に path 文字列だけで決まるためです。

> ⚠️ 新しいロールに権限を配るときは `sync:run` に注意してください。
> Holodex への書き込みは**運用者本人の名義**で残り、取り消せません。
