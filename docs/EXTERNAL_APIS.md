# 外部 API 用途一覧

> デプロイモデル：**案 A — シングルテナント、運用者の key をすべて使用**。
> 危険/課金が発生する操作は `API_AUTH_TOKEN` 認証の後ろにロックしてください（最終セクション参照）。
> 本ドキュメントは 2026-06-28 時点のコードに基づきます。

## 概要

| API | 環境変数 | 方向 | 主な用途 | 課金 / 制限 | 運用時の注意（案 A） |
|-----|----------|:---:|----------|------------|----------------------|
| Holodex（読） | `HOLODEX_API_KEY` | 読 | チャンネル/動画/セットリスト/コメントの取得 | 無料、~80 req/2min | 🟡 共有可（rate limit 依存） |
| Holodex Editor | `HOLODEX_EDITOR_TOKEN` | **書** | seTORI セットリストを Holodex にアップロード | あなたの Holodex アカウントに紐づく | 🔴 **管理者限定必須** |
| Groq | `GROQ_API_KEY` | LLM | 曲名正規化、コメントのハイブリッド解析 | トークン課金 / 制限あり | 🔴 **管理者またはクォータ管理** |
| YouTube Data | `YOUTUBE_API_KEY` | 読 | チャンネル高解像度アバター取得 | 10,000 units/日/プロジェクト | 🟡 共有可 |
| iTunes | （key 不要） | 読 | 楽曲メタデータ、再生時間、カバー | ~20 req/min（Apple） | 🟢 key 不要で安心 |

---

## 1. Holodex API（読） — `HOLODEX_API_KEY`

- **コード位置**：`pkg/holodex/client.go`（認証ヘッダ `X-APIKEY`）、クライアント側 rate limiter は 75 req/2min に設定。
- **実際に呼び出すエンドポイント**：
  - `GET /channels/{id}` — チャンネル情報（名前、英語名、アバター、所属）
  - `GET /videos/{id}` — 動画詳細；`?c=1` 時はコメントを含む。パラメータなし時は `songs` + `channel` + `mentions` を含む
  - `GET /videos?channel_id=&type=stream&status=past&include=songs,mentions` — ページング（50/ページ）でチャンネルの過去歌枠を取得
- **呼び出しトリガー**：
  - `POST /api/sync/holodex`（チャンネル全体同期）
  - `POST /api/sync/holodex/video/{id}`（単一動画同期）
  - `GET /api/streams/{id}/holodex-songs`（セットリスト即時読込）
  - `GET /api/streams/{id}/comments`、`POST /api/streams/{id}/comments/analyze`（コメント取得）
  - `POST /api/singers`（チャンネル情報のみ同期）
- **備考**：`GetChannelVideos`、`SearchVideos` は定義済みだが未使用。

## 2. Holodex Editor Token（書） — `HOLODEX_EDITOR_TOKEN`

- **コード位置**：`internal/service/holodex_service.go` の `SyncSetoriToHolodex`。
- **動作**：各 performance に対して `PUT https://holodex.net/api/v2/songs` を発行。`Authorization: Bearer <editor_token>` と `Origin: https://holodex.net` を付与し、楽曲（iTunes メタデータ含む）を Holodex にアップロード。既存曲と照合して重複を避ける。
- **呼び出しトリガー**：`POST /api/sync/holodex/to-holodex/{id}`
- **⚠️ リスク**：**あなたの Holodex アカウント**で書き込みを行います。すべてのアップロードがあなた名義になります → 公開環境では管理者限定にしてください。未設定またはプレースホルダー文字列の場合はエラーを返して実行しません。

## 3. Groq API（LLM） — `GROQ_API_KEY`

- **複数 provider failover**：**管理画面（設定ページ → AIプロバイダー管理）** で複数の OpenAI 互換プロバイダー（Groq / Gemini / Cerebras / OpenRouter / Ollama…）を登録可能。`internal/service/ai_service.go` は **厳密な優先順 + failover** で呼び出し：常に priority が最小（先頭）の provider を優先し、429/5xx 失敗時は次に切り替え。失敗した provider は 60 秒間クールダウンしてスキップします（使用制限に何度も当たらないため）。プロバイダーが未登録の場合は環境変数 `GROQ_API_KEY` にフォールバック（後方互換）。provider 設定は `ai_providers` テーブルに保存され、API キーはフロントに返しません（末尾4桁のみ）。
- **コード位置**：`pkg/ai/groq.go` — OpenAI 互換 `POST {base_url}/chat/completions`。デフォルトは Groq（model `llama-3.3-70b-versatile`）、temperature 0.1、max_tokens 2048。
- **用途と呼び出しトリガー**：
  1. **曲名正規化**：`internal/service/normalization_service.go` の `BatchAINormalization` — バッチで元の曲名を正規化（演出版表記除去、読み仮名生成、バージョンタグ検出）。失敗時は純粋な DB 照合に降格。トリガー：`POST /api/ai/normalize`。
  2. **コメント hybrid 解析**：`internal/service/comment_service.go` の `AnalyzeComments` → `pkg/comment/ai_parser.go` の `ParseCommentsWithAI` — **AI がどの行が楽曲で start/end 秒数を担当**、正規表現が曲名/アーティスト文字列を抽出（LLM が日本語曲名を書き換えないように）。edit 時（ユーザーが「分析」を押したとき）に実行。key 未設定または AI 失敗時は自動で純粋正規表現に戻る。トリガー：`POST /api/streams/{id}/comments/analyze`。
- **⚠️ リスク**：トークン課金。公開環境であなたの key を使うと、1 ユーザーで請求を爆発させたりクォータを消費し尽くす可能性があります。key 未設定時は AI ステップがフォールトトレラント（生データ / 純粋正規表現結果を返却）。
- **将来の強化**：AI に曲名/アーティストも返してもらうことは可能ですが、「verbatim 部分文字列検証」のガードレールを付けてから採用し、さもなくば正規表現分割に戻してください。

## 4. YouTube Data API（読、任意） — `YOUTUBE_API_KEY`

- **コード位置**：`pkg/youtube/client.go` — `GET /youtube/v3/channels?part=snippet&id=` でチャンネルアバターを取得（high → medium → default 優先）。
- **用途**：`HolodexService.getChannelPhotoURL` がチャンネル/歌手同期時に **優先的に** YouTube 高解像度アバターを使用。取得できないか key 未設定時は Holodex の `photo`、さらに Holodex 静的画像にフォールバック。
- **呼び出しトリガー**（間接、チャンネル同期時）：`POST /api/sync/holodex`、`POST /api/sync/holodex/video/{id}`、`POST /api/singers`。
- **制限**：デフォルトでプロジェクトあたり 10,000 units/日（`channels.list` は約 1 unit/回）。

## 5. iTunes Search / Lookup API（key 不要）

- **コード位置**：`pkg/itunes/client.go` — `GET itunes.apple.com/search`（`entity=song&country=JP&limit=10`）と `GET .../lookup?id=`（`country=JP&lang=ja_jp`）。30s タイムアウト付き。
- **用途と呼び出しトリガー**：
  - iTunes Track 紐付け、ジャケット/アルバム取得：`GET /api/itunes/search`、`GET /api/itunes/{id}`
  - 再生時間（`trackTimeMillis`）を使った歌唱終了時間推定：`POST /api/streams/{id}/estimate-end-times`（`end_time_estimate_service.go`）
  - Holodex アップロード前に楽曲メタデータを補完：`POST /api/sync/holodex/to-holodex/{id}`（`holodex_service.go`）
- **制限**：Apple は key 不要ですが rate limit あり（通常約 20 req/min）。

---

## 案 A 運用時のチェックリスト（推奨）

以下の「課金が発生 / あなた名義で書き込みが発生する」ルートを `API_AUTH_TOKEN`（実装済みの Bearer ゲート）の後ろに置き、管理者のみに開放してください：

- 🔴 `POST /api/sync/holodex/to-holodex/{id}`（Holodex 書き込み）
- 🔴 `POST /api/ai/normalize`（Groq 課金）
- 🔴 `POST /api/streams/{id}/comments/analyze`（Groq 課金 — コメント hybrid 解析）
- 🟡（任意）`POST /api/sync/holodex`、`POST /api/sync/holodex/video/{id}`（Holodex/YouTube クォータ消費）

> 現在の `authorized()` ミドルウェアは「すべての非 GET リクエスト」にトークンを要求します（`API_AUTH_TOKEN` 設定後有効）。したがって上記の POST エンドポイントはデフォルトで保護されます。以降で「公開読み取り + 管理者書き込み」以外のより細かい権限（一部 POST を公開、一部を管理者限定など）を行いたい場合は、ミドルウェアにホワイトリストを追加してください。
