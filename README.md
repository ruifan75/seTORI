# seTORI 🎤

> VTuber 歌枠（歌回 / 歌唱配信）楽曲データベースとトラッキングシステム

seTORI は VTuber の歌枠で歌われた楽曲を収集・識別・管理するためのシステムです。**Holodex** の双方向同期、**YouTube / iTunes** メタデータ、そして **AI 支援**（OpenAI 互換の複数プロバイダーを failover）を統合し、散在する配信コメントのタイムスタンプを構造化された「歌唱記録」データベースへと整理します。

閲覧は誰でも、編集はログインと権限が必要です。権限を持たない利用者は**修正提案**として投稿でき、レビューを経て反映されます。

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
| 9 | **認証・認可** | DB セッション（Bearer）+ ロールごとの権限キー。匿名は閲覧のみ、編集はログイン + `content:edit`。管理画面でユーザー/ロールを調整 |
| 10 | **修正提案** | 権限のない利用者が曲・アーティスト・歌唱の修正を提案。対象ごとに集約してレビュー、値が割れたら「まとめて反映」。条件を満たす timing 提案は自動適用 |
| 11 | **プレイリスト** | 利用者ごと。公開範囲は 非公開 / 限定公開（共有リンク）/ 公開。行単位の認可で、参照できないものは存在を伏せて 404 |
| 12 | **外部アカウント連携** | Google ログイン（`pkg/oauth` の `Provider` を実装すれば追加可能）。メール一致の自動紐付けは双方が確認済みの場合のみ |
| 13 | **DB バックアップ** | pg_dump による手動/自動バックアップ、世代保持、リストア（復元前に安全バックアップを自動作成）、Google Drive 自動アップロード |
| 14 | **設定の一元管理** | 外部サービスの API キーを管理画面から登録。DB 上で AES-256-GCM 暗号化、再起動なしで反映。`.env` は未設定時のフォールバック |

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
| AI | OpenAI 互換の複数プロバイダー（Groq / Gemini / Cerebras / OpenRouter…）を管理画面で登録し優先度順に failover |
| 外部 | Holodex API · YouTube Data API v3 · iTunes Search/Lookup API · Google OAuth · Google Drive |
| 本番 | Docker Compose（Postgres + バックエンド + Caddy）。Caddy が静的配信・`/api` の代理・HTTPS 証明書を担当 |

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
│       ├── ai/                   # OpenAI 互換 client（複数プロバイダーを failover）
│       ├── auth/                 # パスワードハッシュ + セッショントークン
│       ├── chatend/              # live chat の拍手から曲の終了時間を検出
│       ├── comment/              # コメント解析 / 重複排除 / フィルタ / 終了時間推定
│       ├── gdrive/               # Google Drive アップロード（標準ライブラリのみ）
│       ├── holodex/ itunes/ youtube/   # 外部 API client
│       ├── oauth/                # OAuth Provider インターフェース + Google 実装
│       ├── ratelimit/            # Holodex レートリミット
│       ├── secrets/              # 設定値の AES-256-GCM 暗号化
│       └── util/                 # Levenshtein 類似度 + NFKC 正規化
├── frontend/                     # React SPA
│   └── src/
│       ├── api/                  # axios client + TypeScript 型定義
│       ├── components/           # Layout、YoutubePlayer、ui/*
│       ├── store/                # Zustand（認証・プレイヤー）
│       └── pages/                # 各ページ（StreamDetailPage が主編集画面）
│           └── admin/            # 管理画面（ユーザー / 設定 / バックアップ / 提案レビュー / ログ / 同期）
├── docker/docker-compose.yml     # 開発用 PostgreSQL
└── deploy/                       # 本番デプロイ（compose + Caddy）。deploy/README.md 参照
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

### 本番デプロイ

VPS 1 台に Postgres・バックエンド・Caddy をまとめて置く構成を用意しています。
Caddy が静的ファイルと `/api` を同一オリジンで配信するため CORS の設定は不要で、HTTPS 証明書も自動取得されます。

```bash
cd deploy
cp .env.example .env    # DOMAIN・パスワード・暗号化鍵などを記入
docker compose up -d --build
```

手順・運用・設計上の制約は **[`deploy/README.md`](./deploy/README.md)** を参照してください。

バイナリだけ作る場合：

```bash
cd backend  && go build -o bin/server ./cmd/server   # bin/ は .gitignore 対象
cd frontend && npm run build                          # dist/ に出力
```

---

## 環境変数

外部サービスの API キーは**管理画面（管理 → 設定 → 外部サービス連携）から登録**します。DB 上で暗号化され、
再起動なしで反映されます。`.env` に書けるのは未設定時のフォールバックとして残しているためで、新規に設定するなら管理画面を推奨します。

`.env` に残るのは「DB を読む前に必要なもの」と「DB に置けないもの」だけです（`backend/.env.example` を参照）：

| 変数 | 必須 | 説明 |
|------|:---:|------|
| `DATABASE_URL` | ✓ | PostgreSQL 接続文字列。DB 自身の接続先なので DB には置けない |
| `SETTINGS_ENCRYPTION_KEY` | — | 管理画面から保存する API キーを DB 上で暗号化する鍵。未設定でも起動はするが機密の保存はできない。**バックアップは Drive へ自動アップロードされるため、この鍵だけは DB に置かない**。任意の長さの文字列でよい（内部で SHA-256 により 32 バイトへ伸長） |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | — | ユーザーが 0 件のときだけ作成される初期管理者。UI が使えない時点で必要なのでここに残る |
| `OAUTH_REDIRECT_BASE_URL` | — | OAuth のコールバック先。デフォルト `http://localhost:8080`。間違えるとログインできなくなるため UI からは変更不可 |
| `FRONTEND_BASE_URL` | — | 認証後の戻り先。デフォルト `http://localhost:5173` |
| `HOST` / `PORT` | — | listen 開始前に必要。デフォルト `0.0.0.0` / `8080` |
| `BACKUP_DIR` | — | バックアップの保存先。デフォルト `./backups` |
| `BACKUP_PG_CONTAINER` | — | ホストに `pg_dump` が無い場合に `docker exec` で使う PostgreSQL コンテナ名 |
| `ENVIRONMENT` / `LOG_LEVEL` | — | `development` / `production`、`DEBUG` / `INFO` / `WARN` / `ERROR` |

管理画面から設定できる（`.env` はフォールバック）：`HOLODEX_API_KEY`、`HOLODEX_EDITOR_TOKEN`、`YOUTUBE_API_KEY`、
`GROQ_API_KEY`、`GOOGLE_OAUTH_CLIENT_ID` / `_SECRET`（Drive バックアップ用・デバイスフロー型）、
`GOOGLE_SIGNIN_CLIENT_ID` / `_SECRET`（Google ログイン用・**ウェブアプリケーション型**。Drive 用とは型が違うため流用不可）。

フロントエンド：`VITE_API_URL`（任意、デフォルトは現在の hostname の `:8080`）。
本番のように同一オリジンで配信する場合は空文字を指定します（空文字は「同一オリジン」の意味を持つため `??` で受けています）。

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

上記以外にも 100 以上のエンドポイントがあります。全体は `internal/handler/router.go` の
`setupRoutes()`（登録）と `requiredPermission()`（必要な権限）を参照してください。主なグループ：

| プレフィックス | 概要 | 認可 |
|---|---|---|
| `/api/auth/*` | ログイン / ログアウト / 自分の情報 / OAuth 連携 | 公開（一部は要ログイン） |
| `/api/playlists/*` | プレイリストの CRUD・並び替え・公開範囲 | 要ログイン（権限は不要）＋行単位で所有者判定 |
| `/api/shared/playlists/{slug}` | 限定公開の共有リンク | 公開 |
| `/api/suggestions/*` | 修正提案の投稿・取り下げ・レビュー・一括反映 | 投稿は要ログイン、レビューは `content:edit` |
| `/api/backups/*` | バックアップ・リストア・Google Drive 連携 | `backup:manage` |
| `/api/settings/integrations` | 外部サービスの API キー | `users:manage` |
| `/api/users` · `/api/roles` · `/api/permissions` | ユーザー・ロール・権限 | `users:manage` |
| `/api/ai-providers/*` | OpenAI 互換プロバイダーの登録と failover 順 | `users:manage` |
| `/api/logs` | ログ閲覧・レベル変更 | `logs:view` |

---

## データモデル

主なテーブル（完全な定義は `backend/internal/database/migrations/` を参照）：

- `singers` — VTuber、PK は YouTube チャンネル ID。`metadata_source=holodex` は Holodex 管理、`youtube` は手動編集可能な fallback 登録
- `songs` — 楽曲マスター、一意キー `(name, original_artist)`；`song_itunes` で iTunes Track と 1:N 紐付け
- `streams` — 歌枠、PK は YouTube 動画 ID；`holodex_data` / `comment_raw` / `comment_songs` は JSONB
- `performances` — 歌唱記録、一意キー `(stream_id, song_id, start_seconds)`
- 多対多：`stream_singers`（`is_owner` 含む）、`performance_singers`、`stream_stream_tags`、`performance_performance_tags`
- `performance_tags` / `stream_tags` — ビルトインタグをプリロード；`filter_keywords` — コメントの除外/保持キーワード（seed 含む）
- `users` / `roles` / `sessions` / `oauth_identities` — 認証。セッションはトークンの SHA-256 ハッシュのみ保存
- `playlists` / `playlist_items` — プレイリスト。項目は `performances(id)` を参照（`ON DELETE CASCADE`）
- `edit_suggestions` — 修正提案。承認時に「提案時点のスナップショット vs 現在値」を突き合わせ、ズレていれば `conflict` で停止
- `app_settings` — 汎用 KV（JSONB）。バックアップ設定・自動反映の条件・外部サービスの API キー（暗号化）

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
- 上記とは別に、**live chat の拍手から終了時間を検出する経路**があります（`pkg/chatend`）。
  歌が終わると観客が「純粋な拍手」（888 / 拍手 / :clapping_hands: / 👏）を流すので、
  各曲の `(start+MinSong, next_start)` 区間で最大の拍手群を探し、その起点 − ReactionLag を終了とみなします。
  1445 首の実測で MAE 2.18 秒、99% が ±10 秒以内。
  入口は `/api/streams/{id}/chat-end-estimate`（試算）と `/api/streams/{id}/analyze-chat-ends`（保存）。
- AI は純粋な正規表現が失敗したときのフォールバックとして機能し、曲名正規化とバージョンタグ（acoustic/piano/short…）の検出を担当。
  プロバイダーは管理画面で複数登録でき、優先度順に failover します。

> AI 呼び出しは 1 配信あたり 2 回（抽出・正規化）。実際に送っている prompt と返ってくる JSON の
> 形式、実採取した入出力の例、トークン量の実測、既知の問題は
> **[`docs/AI_PIPELINE.md`](./docs/AI_PIPELINE.md)** にまとめています。

---

## 既知の問題と改善点

**アーキテクチャ**
- `StreamDetailPage.tsx` が 2900 行を超えており、コンポーネント分割が急務
- API ドキュメントなし（Swagger / OpenAPI）
- テストは `pkg` 配下と一部 service にとどまり、カバレッジは低い
- middleware フレームワークなし（CORS / logging は手動処理）

**運用**
- OAuth の state と引き換えコードをメモリに保持しているため、**バックエンドは 1 インスタンスまで**。
  水平スケールする前に DB へ移す必要がある
- 常駐 goroutine（セッション掃除・自動バックアップ）に依存しているため、
  スケール・トゥ・ゼロする環境には載らない
- 構造化ログ・監視/アラートなし

**機能**
- CSV/JSON エクスポート、統計ダッシュボードが不足
- X / Discord の OAuth プロバイダーは未実装（`pkg/oauth` の `Provider` を実装すれば追加可能）

---

## セキュリティ注意事項 ⚠️

- 秘密情報はリポジトリに置きません。`backend/.gitignore` は `.env*` + `!.env.example`、
  本番用は `/deploy/.env*` をルートの `.gitignore` で除外しています。
- **`SETTINGS_ENCRYPTION_KEY` をバックアップと同じ場所に置かないでください。**
  バックアップは Google Drive へ自動アップロードされるため、鍵が同梱されると暗号化の意味が無くなります。
  また、この鍵を失うと DB に保存済みの API キーは復号できません。
- **`ADMIN_PASSWORD` を既定値のまま公開しないでください。** 新しい DB の初回起動時に、その値で管理者が作成されます。
- CORS は `Access-Control-Allow-Origin: *` のままです。セッショントークンは localStorage 上にあり
  Cookie ではないため他オリジンから付与されませんが、本番は Caddy が同一オリジンで配信するため
  そもそもクロスオリジンになりません。
