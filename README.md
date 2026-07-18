# seTORI 🎤

> VTuber 歌枠（歌回 / 歌唱配信）楽曲データベースとトラッキングシステム

seTORI は VTuber の歌枠で歌われた楽曲を収集・識別・管理するためのシステムです。**Holodex** の双方向同期、**YouTube / iTunes** メタデータ、そして **Groq (Llama 3.3 70B)** AI 支援を統合し、散在する配信コメントのタイムスタンプを構造化された「歌唱記録」データベースへと整理します。

---

## 目次

- [コア機能](#コア機能)
- [システム構成](#システム構成)
- [プロジェクト構造](#プロジェクト構造)
- [クイックスタート](#クイックスタート)
- [環境変数](#環境変数)
- [API 一覧](#api-一覧)
- [データモデル](#データモデル)
- [コメント解析フロー](#コメント解析フロー)
- [既知の問題と改善点](#既知の問題と改善点)
- [セキュリティ注意事項](#セキュリティ注意事項)

---

## コア機能

| # | 機能 | 説明 |
|---|------|------|
| 1 | **楽曲管理 (Songs)** | CRUD、GIN trigram あいまい検索、重複楽曲のマージ、全履歴の歌唱記録。一意キー `(name + original_artist)` |
| 2 | **配信管理 (Streams)** | YouTube 動画 ID を主キー；`is_processed` / `is_hidden` ステータス；JSONB カラム `holodex_data` / `comment_raw` / `comment_songs` |
| 3 | **歌手管理 (Singers)** | YouTube チャンネル ID を主キー；所属事務所、アバター、英語名；複数人コラボ歌枠対応 |
| 4 | **歌唱記録 (Performances)** | 開始/終了秒数、歌唱タグ、複数歌手コラボ。一意キー `(stream_id + song_id + start_seconds)` |
| 5 | **Holodex 双方向同期** | チャンネルの配信とセットリストの取得、seTORI セットリストの Holodex へのアップロード、SHA256 ハッシュキャッシュによる重複防止 |
| 6 | **コメント分析** | YouTube/Holodex からコメントを取得、正規表現でタイムスタンプ（HH:MM:SS / MM:SS）を解析 + 区切り文字で曲名/アーティストを分解 |
| 7 | **AI 識別と正規化** | バッチでの曲名正規化、バージョンタグ検出、コメントのハイブリッド解析；**管理画面で複数の OpenAI 互換プロバイダー（Groq/Gemini/Cerebras…）を設定し優先度順に failover**、使用制限に達したら自動で次に切り替え |
| 8 | **iTunes 連携** | 検索と Track ID 紐付けで再生時間/アルバム情報を取得し、再生時間から歌唱終了時間を推定 |

---

## システム構成

```
┌────────────────────────────┐      HTTP / Axios     ┌────────────────────────────┐
│  Frontend (React/Vite)     │ ───────────────────►  │  Backend (Go, net/http)    │
│  :5173                     │ ◄───────────────────  │  :8080                     │
│  React Query + Zustand     │        JSON           │  service / repository      │
└────────────────────────────┘                       └─────────────┬──────────────┘
                                                                    │ SQL (lib/pq)
                          外部サービス                                   ▼
        ┌───────────────┬───────────────┬──────────────┐   ┌────────────────────┐
        │ Holodex API   │ YouTube API   │ iTunes API   │   │ PostgreSQL 16      │
        │ Groq API      │               │              │   │ :5432 (Docker)     │
        └───────────────┴───────────────┴──────────────┘   └────────────────────┘
```

| 層 | 技術 |
|------|------|
| フロントエンド | React 19 · TypeScript · Vite 7 · Tailwind CSS 3 · React Router 7 |
| 状態 | Zustand（UI）· TanStack React Query（サーバー状態） |
| バックエンド | Go 1.24（標準ライブラリ `net/http`、service / repository パターン、コンストラクタ DI） |
| データベース | PostgreSQL 16（`uuid-ossp`、`pg_trgm` 拡張） |
| AI | Groq API — Llama 3.3 70B |
| 外部 | Holodex API · YouTube Data API v3 · iTunes Search/Lookup API |

バックエンドは**純粋な標準ライブラリ**で実装されています：Web フレームワークは使用せず、ルーティングは Go 1.22+ の `http.ServeMux`（`GET /api/songs/{id}` スタイル）、マイグレーションは `embed.FS` で埋め込み、起動時に自動実行します。

---

## プロジェクト構造

```
seTORI/
├── backend/                      # Go API server
│   ├── cmd/server/main.go        # エントリポイント：.env 読み込み → DB 接続 → migrate → router 起動
│   ├── internal/
│   │   ├── config/               # 環境変数から Config を読み込み
│   │   ├── database/             # コネクションプール + migration runner + migrations/*.sql
│   │   ├── handler/router.go     # すべての HTTP handler（+ CORS / logging）
│   │   ├── dto/                  # リクエスト/レスポンス構造体
│   │   ├── models/               # DB model
│   │   ├── repository/           # SQL アクセス層
│   │   └── service/              # ビジネスロジック層
│   └── pkg/
│       ├── ai/                   # Groq client
│       ├── comment/              # コメント解析 / 重複排除 / フィルタ / 終了時間推定
│       ├── holodex/ itunes/ youtube/   # 外部 API client
│       ├── ratelimit/            # Holodex レートリミット
│       └── util/                 # Levenshtein 類似度 + NFKC 正規化
├── frontend/                     # React SPA
│   └── src/
│       ├── api/                  # axios client + TypeScript 型定義
│       ├── components/           # Layout、YoutubePlayer、ui/*
│       └── pages/                # 各ページ（StreamDetailPage が主編集画面）
├── docker/docker-compose.yml     # PostgreSQL（現在は DB のみコンテナ化）
└── utawaku-timestamp/            # 補助 Python ツール（音声/コメントタイムスタンプ検出、独立リポジトリ）
```

---

## クイックスタート

### 前提条件

- Go **1.24+**
- Node.js **18+**（推奨 20+）と npm
- Docker / Docker Compose（PostgreSQL 用）

### 1. データベースの起動

```bash
cd docker
docker compose up -d        # PostgreSQL 16 on :5432 (postgres/postgres/setori)
```

### 2. バックエンドの起動

```bash
cd backend
cp .env.example .env        # API キーを記入（下記の環境変数を参照）
go run ./cmd/server/main.go # 起動時に自動でマイグレーションを実行、0.0.0.0:8080 でリッスン
```

ヘルスチェック：`curl http://localhost:8080/health` → `{"status":"ok"}`。同一 LAN からは `http://<server-ip>:8080/health`。

### 3. フロントエンドの起動

```bash
cd frontend
npm install
npm run dev                 # Vite dev server on 0.0.0.0:5173
```

フロントエンドはデフォルトで現在の hostname の `:8080` に接続します。同一 LAN から `http://<server-ip>:5173` を開いた場合、API は自動で `http://<server-ip>:8080` になります。`VITE_API_URL` で上書き可能です（`frontend/.env` を作成）。

### 本番ビルド

```bash
cd backend  && go build -o bin/server ./cmd/server   # bin/ は .gitignore 対象
cd frontend && npm run build                          # dist/ に出力
```

---

## 環境変数

`backend/.env`（`backend/.env.example` を参照）：

| 変数 | 必須 | 説明 |
|------|:---:|------|
| `DATABASE_URL` | ✓ | PostgreSQL 接続文字列。デフォルト `postgres://postgres:postgres@localhost:5432/setori?sslmode=disable` |
| `HOLODEX_API_KEY` | ✓ | Holodex API キー（配信/セットリスト同期用） |
| `HOLODEX_EDITOR_TOKEN` | — | セットリストを Holodex にアップロードする際の編集者トークン |
| `GROQ_API_KEY` | — | AI 正規化/コメントフィルタ。未設定時は純粋な正規表現にフォールバック |
| `YOUTUBE_API_KEY` | — | 公開動画コメント（YouTube 優先・Holodex fallback）、チャンネルの高解像度アバター取得、および Holodex 未登録チャンネル追加用 |
| `JWT_SECRET` | — | 予約フィールド（未使用） |
| `API_AUTH_TOKEN` | — | 設定時は書き込み操作（POST/PUT/DELETE）に `Authorization: Bearer <token>` が必要。空欄時は公開（[セキュリティ](#セキュリティ注意事項)参照） |
| `ENVIRONMENT` | — | `development` / `production` |
| `HOST` | — | バックエンドの bind address。デフォルト `0.0.0.0` |
| `PORT` | — | バックエンドのポート。デフォルト `8080` |

フロントエンド：`VITE_API_URL`（任意、デフォルトは現在の hostname の `:8080`）、`VITE_API_TOKEN`（任意、バックエンドで `API_AUTH_TOKEN` 有効時に Bearer トークンを付与）。

> 各外部 API（Holodex / Groq / YouTube / iTunes）の実際の用途、トリガールート、課金感度は **[`docs/EXTERNAL_APIS.md`](./docs/EXTERNAL_APIS.md)** にまとめています。

---

## API 一覧

すべての API は `/api` をプレフィックスとし、JSON を返却します。エラー形式は `{"error": "..."}` です。

| Method | Path | 説明 |
|--------|------|------|
| GET | `/health` | ヘルスチェック |
| **Songs** | | |
| GET | `/api/songs` | 一覧（`page` / `limit` / `search`） |
| GET | `/api/songs/{id}` | 単一楽曲 |
| GET | `/api/songs/{id}/performances` | その楽曲の全歌唱記録 |
| POST | `/api/songs` | 作成 |
| PUT | `/api/songs/{id}` | 更新 |
| DELETE | `/api/songs/{id}` | 削除 |
| POST | `/api/songs/{id}/merge` | 対象楽曲へマージ |
| **Streams** | | |
| GET | `/api/streams` | 一覧（デフォルトは非表示を除く） |
| GET | `/api/streams/{id}` | 詳細（歌唱リスト、Holodex/コメントタイムライン含む） |
| POST | `/api/streams` | ⚠️ 未実装（501 を返す）。Holodex 同期を使用してください |
| PUT | `/api/streams/{id}` | タイトル/日付/タグ/参加者/ステータスの更新 |
| GET | `/api/streams/{id}/holodex-songs` | Holodex セットリストを即時読み込み |
| POST | `/api/streams/{id}/estimate-end-times` | 終了時間の推定 |
| POST/DELETE | `/api/streams/{id}/performances` | 歌唱記録の一括作成 / 全削除 |
| GET | `/api/streams/{id}/comments` | 生コメントの取得 |
| POST | `/api/streams/{id}/comments/sync-youtube` | YouTube Data API から生コメントを手動同期 |
| POST | `/api/streams/{id}/comments/analyze` | コメントを楽曲に解析 |
| POST | `/api/comments/backfill` | comment_songs を補完 |
| **Singers** | | |
| GET | `/api/singers` · `/search` · `/{id}` · `/{id}/streams` · `/{id}/performances` | 一覧/検索/詳細/配信/歌唱 |
| POST | `/api/singers` | Holodex 同期によりチャンネル情報から新規追加。Holodex 未登録時は YouTube fallback |
| PUT | `/api/singers/{id}` | Holodex 未登録（YouTube fallback）チャンネルの手動メタデータ更新 |
| **Holodex Sync** | | |
| POST | `/api/sync/holodex` | チャンネル全体を同期 |
| POST | `/api/sync/holodex/video/{id}` | 単一動画を同期 |
| POST | `/api/sync/holodex/to-holodex/{id}` | seTORI セットリストを Holodex へアップロード |
| **その他** | | |
| GET/POST/DELETE | `/api/filter-keywords` | コメントフィルタキーワード管理 |
| GET/POST/DELETE | `/api/stream-tags` · `/api/performance-tags` | タグ管理 |
| POST | `/api/ai/normalize` | AI による曲名一括正規化 |
| GET | `/api/itunes/search` · `/api/itunes/{id}` | iTunes 検索 / 照会 |

---

## データモデル

主なテーブル（完全な定義は `backend/internal/database/migrations/` を参照）：

- `singers` — VTuber、PK は YouTube チャンネル ID。`metadata_source=holodex` は Holodex 管理、`youtube` は手動編集可能な fallback 登録
- `songs` — 楽曲マスター、一意キー `(name, original_artist)`；`song_itunes` で iTunes Track と 1:N 紐付け
- `streams` — 歌枠、PK は YouTube 動画 ID；`holodex_data` / `comment_raw` / `comment_songs` は JSONB
- `performances` — 歌唱記録、一意キー `(stream_id, song_id, start_seconds)`
- 多対多：`stream_singers`（`is_owner` 含む）、`performance_singers`、`stream_stream_tags`、`performance_performance_tags`
- `performance_tags` / `stream_tags` — ビルトインタグをプリロード；`filter_keywords` — コメントの除外/保持キーワード（seed 含む）

マイグレーションのファイル名は `NNN_description.sql`。起動時にファイル名順で実行し、`schema_migrations` テーブルで追跡。冪等に動作します。

---

## コメント解析フロー

```
YouTube 公開コメント（Holodex fallback）→ 生コメント (comment_raw)
   │  regex でタイムスタンプ + 区切り文字を解析   pkg/comment/parser.go
   ▼
ParsedSong[]  (comment_songs、未重複排除)
   │  キーワードフィルタ（filter/keep）           pkg/comment/filter.go
   │  タイムスタンプ±30s & 曲名類似度≥0.8 で重複排除  pkg/comment/dedup.go
   │  妥当性検証（曲名なし/負時間は除外）         pkg/comment/estimator.go
   ▼
解析結果をフロントへ返却 → ユーザーが編集 → Performance を作成
```

- 終了時間の推定優先順位：コメントに記載あり → iTunes 再生時間 → 次の曲の開始時間 → デフォルト 240 秒（`service/end_time_estimate_service.go`）。
- Groq AI は純粋な正規表現が失敗したときのフォールバックとして機能し、曲名正規化とバージョンタグ（acoustic/piano/short…）の検出を担当。

---

## 既知の問題と改善点

詳細なコードレビュー結果は **[`CODE_REVIEW.md`](./CODE_REVIEW.md)** を参照。主なポイント：

**アーキテクチャ**
- `StreamDetailPage.tsx` が 2300 行を超えており、コンポーネント分割が急務
- 認証は任意（`API_AUTH_TOKEN`）の Bearer トークンゲートで書き込み操作のみ保護。完全なユーザー/ロール機構は未実装（`JWT_SECRET` は未使用）
- バックエンドに未使用のデッドコードが若干存在（`pkg/comment` の推定関数、一部 client メソッド）
- 自動テストなし、API ドキュメントなし（Swagger）

**機能 / 運用**
- バッチ操作、CSV/JSON エクスポート、統計ダッシュボードが不足
- フロント/バックエンドのコンテナ化未実施（Docker は PostgreSQL のみ）、CI/CD なし、構造化ログなし

---

## セキュリティ注意事項 ⚠️

- 過去の commit で **`backend/.env`（実在の API キー入り）とビルド成果物 `backend/bin/server`** がリポジトリにコミットされたことがあります。`.gitignore` でカバー済みですが、すでに git 履歴に残っています。**Holodex / Groq / YouTube / Holodex Editor トークンを必ずローテーションし、必要に応じて git 履歴のクリーンアップ（`git filter-repo` など）を検討してください**。
- **書き込み認証**：`API_AUTH_TOKEN` を設定すると、すべての POST/PUT/DELETE で `Authorization: Bearer <token>` が必要になります（読み取り API と `/health` は公開のまま）。フロントエンドで `VITE_API_TOKEN` を設定すると自動付与されます。**未設定時は書き込み API が公開** となり、起動時に警告を表示します。
- CORS は現在 `Access-Control-Allow-Origin: *` です。本番デプロイ前にオリジンを制限し、完全なユーザー/ロール認証の導入を検討してください。
