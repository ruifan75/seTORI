# seTORI 🎤

> VTuber 歌回（歌枠 / 唱歌直播）歌曲資料庫與追蹤系統

seTORI 用來蒐集、辨識並管理 VTuber 歌回中演唱的每一首歌。它整合 **Holodex** 雙向同步、**YouTube / iTunes** 元資料，以及 **Groq (Llama 3.3 70B)** AI 輔助，把零散的直播留言時間軸整理成結構化的「演唱記錄」資料庫。

---

## 目錄

- [核心功能](#核心功能)
- [系統架構](#系統架構)
- [專案結構](#專案結構)
- [快速開始](#快速開始)
- [環境變數](#環境變數)
- [API 一覽](#api-一覽)
- [資料模型](#資料模型)
- [留言分析流程](#留言分析流程)
- [已知問題與改進方向](#已知問題與改進方向)
- [安全性注意事項](#安全性注意事項-)

---

## 核心功能

| # | 功能 | 說明 |
|---|------|------|
| 1 | **歌曲管理 (Songs)** | CRUD、GIN trigram 模糊搜尋、重複歌曲合併、全歷史演唱記錄。唯一鍵 `(name + original_artist)` |
| 2 | **直播管理 (Streams)** | 以 YouTube 影片 ID 為主鍵；`is_processed` / `is_hidden` 狀態；JSONB 欄位 `holodex_data` / `comment_raw` / `comment_songs` |
| 3 | **歌手管理 (Singers)** | 以 YouTube 頻道 ID 為主鍵；事務所、頭像、英文名；支援多人聯動歌回 |
| 4 | **演唱記錄 (Performances)** | 開始/結束秒數、演唱標籤、多歌手聯動。唯一鍵 `(stream_id + song_id + start_seconds)` |
| 5 | **Holodex 雙向同步** | 拉取頻道直播與歌單、上傳 seTORI 歌單回 Holodex、SHA256 hash 快取避免重複處理 |
| 6 | **留言分析** | 從 YouTube/Holodex 抓留言，正則解析時間戳（HH:MM:SS / MM:SS）+ 分隔符拆解歌名/歌手 |
| 7 | **AI 辨識與正規化** | 批次歌名正規化、版本標籤偵測、留言 hybrid 解析；**支援多個 OpenAI 相容 provider（Groq/Gemini/Cerebras…）於管理介面設定、依優先序 failover**，撞到 usage limit 自動換下一家 |
| 8 | **iTunes 整合** | 搜尋並綁定 Track ID，取得時長/專輯資訊，並用時長估算演唱結束時間 |

---

## 系統架構

```
┌────────────────────────────┐      HTTP / Axios     ┌────────────────────────────┐
│  Frontend (React/Vite)     │ ───────────────────►  │  Backend (Go, net/http)    │
│  :5173                     │ ◄───────────────────  │  :8080                     │
│  React Query + Zustand     │        JSON           │  service / repository      │
└────────────────────────────┘                       └─────────────┬──────────────┘
                                                                    │ SQL (lib/pq)
                          外部服務                                   ▼
        ┌───────────────┬───────────────┬──────────────┐   ┌────────────────────┐
        │ Holodex API   │ YouTube API   │ iTunes API   │   │ PostgreSQL 16      │
        │ Groq API      │               │              │   │ :5432 (Docker)     │
        └───────────────┴───────────────┴──────────────┘   └────────────────────┘
```

| 層級 | 技術 |
|------|------|
| 前端 | React 19 · TypeScript · Vite 7 · Tailwind CSS 3 · React Router 7 |
| 狀態 | Zustand（UI）· TanStack React Query（伺服器狀態） |
| 後端 | Go 1.24（標準庫 `net/http`，service / repository 模式，建構式 DI） |
| 資料庫 | PostgreSQL 16（`uuid-ossp`、`pg_trgm` 擴充） |
| AI | Groq API — Llama 3.3 70B |
| 外部 | Holodex API · YouTube Data API v3 · iTunes Search/Lookup API |

後端是**純標準庫**實作：沒有 web 框架，路由用 Go 1.22+ 的 `http.ServeMux`（`GET /api/songs/{id}` 樣式），migration 透過 `embed.FS` 內嵌、啟動時自動執行。

---

## 專案結構

```
seTORI/
├── backend/                      # Go API server
│   ├── cmd/server/main.go        # 進入點：載入 .env → 連 DB → migrate → 啟動 router
│   ├── internal/
│   │   ├── config/               # 從環境變數載入 Config
│   │   ├── database/             # 連線池 + migration runner + migrations/*.sql
│   │   ├── handler/router.go     # 所有 HTTP handler（+ CORS / logging）
│   │   ├── dto/                  # 請求/回應結構
│   │   ├── models/               # DB model
│   │   ├── repository/           # SQL 存取層
│   │   └── service/              # 商業邏輯層
│   └── pkg/
│       ├── ai/                   # Groq client
│       ├── comment/              # 留言解析 / 去重 / 過濾 / 結束時間估算
│       ├── holodex/ itunes/ youtube/   # 外部 API client
│       ├── ratelimit/            # Holodex 速率限制
│       └── util/                 # Levenshtein 相似度 + NFKC 正規化
├── frontend/                     # React SPA
│   └── src/
│       ├── api/                  # axios client + TypeScript 型別
│       ├── components/           # Layout、YoutubePlayer、ui/*
│       └── pages/                # 各頁面（StreamDetailPage 為主編輯介面）
├── docker/docker-compose.yml     # PostgreSQL（目前僅 DB 容器化）
└── utawaku-timestamp/            # 輔助 Python 工具（音訊/留言時間軸偵測，獨立 repo）
```

---

## 快速開始

### 先決條件

- Go **1.24+**
- Node.js **18+**（建議 20+）與 npm
- Docker / Docker Compose（用於 PostgreSQL）

### 1. 啟動資料庫

```bash
cd docker
docker compose up -d        # PostgreSQL 16 on :5432 (postgres/postgres/setori)
```

### 2. 啟動後端

```bash
cd backend
cp .env.example .env        # 填入你的 API keys（見下方環境變數）
go run ./cmd/server/main.go # 啟動時自動執行 migration，監聽 :8080
```

健康檢查：`curl http://localhost:8080/health` → `{"status":"ok"}`

### 3. 啟動前端

```bash
cd frontend
npm install
npm run dev                 # Vite dev server on :5173
```

前端預設打 `http://localhost:8080`，可用 `VITE_API_URL` 覆寫（建立 `frontend/.env`）。

### 建構正式版

```bash
cd backend  && go build -o bin/server ./cmd/server   # bin/ 已被 .gitignore 忽略
cd frontend && npm run build                          # 輸出到 dist/
```

---

## 環境變數

`backend/.env`（參考 `backend/.env.example`）：

| 變數 | 必填 | 說明 |
|------|:---:|------|
| `DATABASE_URL` | ✓ | PostgreSQL 連線字串，預設 `postgres://postgres:postgres@localhost:5432/setori?sslmode=disable` |
| `HOLODEX_API_KEY` | ✓ | Holodex API key（同步直播/歌單） |
| `HOLODEX_EDITOR_TOKEN` | — | 上傳歌單回 Holodex 用的編輯 token |
| `GROQ_API_KEY` | — | AI 正規化/留言篩選，未設定時自動降級為純正則 |
| `YOUTUBE_API_KEY` | — | 取得頻道高解析度頭像，未設定時 fallback 到 Holodex |
| `JWT_SECRET` | — | 預留欄位，目前未使用 |
| `API_AUTH_TOKEN` | — | 若設定，寫入操作（POST/PUT/DELETE）需帶 `Authorization: Bearer <token>`；留空則公開（見 [安全性](#安全性注意事項-)） |
| `ENVIRONMENT` | — | `development` / `production` |
| `PORT` | — | 後端埠號，預設 `8080` |

前端：`VITE_API_URL`（選填，預設 `http://localhost:8080`）、`VITE_API_TOKEN`（選填，後端啟用 `API_AUTH_TOKEN` 時用來附帶 Bearer token）。

> 各外部 API（Holodex / Groq / YouTube / iTunes）的實際用途、觸發路由與上線敏感度，整理於 **[`docs/EXTERNAL_APIS.md`](./docs/EXTERNAL_APIS.md)**。

---

## API 一覽

所有 API 以 `/api` 為前綴，回傳 JSON，錯誤格式為 `{"error": "..."}`。

| Method | Path | 說明 |
|--------|------|------|
| GET | `/health` | 健康檢查 |
| **Songs** | | |
| GET | `/api/songs` | 列表（`page` / `limit` / `search`） |
| GET | `/api/songs/{id}` | 單首歌曲 |
| GET | `/api/songs/{id}/performances` | 該歌曲所有演唱記錄 |
| POST | `/api/songs` | 建立 |
| PUT | `/api/songs/{id}` | 更新 |
| DELETE | `/api/songs/{id}` | 刪除 |
| POST | `/api/songs/{id}/merge` | 合併到目標歌曲 |
| **Streams** | | |
| GET | `/api/streams` | 列表（預設不含隱藏） |
| GET | `/api/streams/{id}` | 詳情（含演唱清單、Holodex/留言時間軸） |
| POST | `/api/streams` | ⚠️ 未實作（回 501），請改用 Holodex 同步 |
| PUT | `/api/streams/{id}` | 更新標題/日期/標籤/參與者/狀態 |
| GET | `/api/streams/{id}/holodex-songs` | 即時載入 Holodex 歌單 |
| POST | `/api/streams/{id}/estimate-end-times` | 估算結束時間 |
| POST/DELETE | `/api/streams/{id}/performances` | 批次建立 / 全部刪除演唱記錄 |
| GET | `/api/streams/{id}/comments` | 取得原始留言 |
| POST | `/api/streams/{id}/comments/analyze` | 分析留言為歌曲 |
| POST | `/api/comments/backfill` | 補填 comment_songs |
| **Singers** | | |
| GET | `/api/singers` · `/search` · `/{id}` · `/{id}/streams` · `/{id}/performances` | 列表/搜尋/詳情/直播/演唱 |
| POST | `/api/singers` | 透過 Holodex 同步頻道資訊新增 |
| **Holodex Sync** | | |
| POST | `/api/sync/holodex` | 同步整個頻道 |
| POST | `/api/sync/holodex/video/{id}` | 同步單一影片 |
| POST | `/api/sync/holodex/to-holodex/{id}` | 上傳 seTORI 歌單回 Holodex |
| **其他** | | |
| GET/POST/DELETE | `/api/filter-keywords` | 留言過濾關鍵字管理 |
| GET/POST/DELETE | `/api/stream-tags` · `/api/performance-tags` | 標籤管理 |
| POST | `/api/ai/normalize` | 批次 AI 歌名正規化 |
| GET | `/api/itunes/search` · `/api/itunes/{id}` | iTunes 搜尋 / 查詢 |

---

## 資料模型

主要資料表（完整定義見 `backend/internal/database/migrations/`）：

- `singers` — VTuber，PK 為 YouTube 頻道 ID
- `songs` — 歌曲 master，唯一鍵 `(name, original_artist)`；`song_itunes` 一對多綁定 iTunes Track
- `streams` — 歌回，PK 為 YouTube 影片 ID；`holodex_data` / `comment_raw` / `comment_songs` 為 JSONB
- `performances` — 演唱記錄，唯一鍵 `(stream_id, song_id, start_seconds)`
- 多對多：`stream_singers`（含 `is_owner`）、`performance_singers`、`stream_stream_tags`、`performance_performance_tags`
- `performance_tags` / `stream_tags` — 預載入內建標籤；`filter_keywords` — 留言過濾/保留關鍵字（含 seed）

Migration 命名為 `NNN_description.sql`，啟動時依檔名排序、用 `schema_migrations` 表追蹤、冪等執行。

---

## 留言分析流程

```
原始留言 (comment_raw)
   │  regex 解析時間戳 + 分隔符        pkg/comment/parser.go
   ▼
ParsedSong[]  (comment_songs，未去重)
   │  關鍵字過濾（filter/keep）        pkg/comment/filter.go
   │  時間戳±30s & 歌名相似度≥0.8 去重   pkg/comment/dedup.go
   │  合理性驗證（無歌名/負時間剔除）    pkg/comment/estimator.go
   ▼
分析結果回傳前端 → 使用者編輯 → 建立 Performance
```

- 結束時間估算優先序：留言已標註 → iTunes 時長 → 下一首開始時間 → 預設 240 秒（`service/end_time_estimate_service.go`）。
- Groq AI 在純正則失敗時作為 fallback，並負責歌名正規化與版本標籤（acoustic/piano/short…）偵測。

---

## 已知問題與改進方向

詳細的程式碼審查結果見 **[`CODE_REVIEW.md`](./CODE_REVIEW.md)**。重點：

**架構**
- `StreamDetailPage.tsx` 超過 2300 行，亟需拆分元件
- 認證為選用（`API_AUTH_TOKEN`）的 Bearer token 閘門，僅保護寫入操作；尚無完整的使用者/角色機制（`JWT_SECRET` 仍未使用）
- 後端有少量未使用的死碼（`pkg/comment` 估算函式、部分 client 方法）
- 無自動化測試、無 API 文件（Swagger）

**功能 / 維運**
- 缺少批次操作、匯出（CSV/JSON）、統計 Dashboard
- 前後端尚未容器化（Docker 僅有 PostgreSQL）、無 CI/CD、無結構化日誌

---

## 安全性注意事項 ⚠️

- 歷史 commit 曾將 **`backend/.env`（含真實 API keys）與編譯產物 `backend/bin/server`** 提交進版控。已從追蹤移除（`.gitignore` 本就涵蓋），但金鑰已存在於 git 歷史中——**請務必輪替 Holodex / Groq / YouTube / Holodex Editor token，並考慮清理 git 歷史（如 `git filter-repo`）**。
- **寫入認證**：設定 `API_AUTH_TOKEN` 後，所有 POST/PUT/DELETE 需帶 `Authorization: Bearer <token>`（讀取 API 與 `/health` 維持公開）；前端設定 `VITE_API_TOKEN` 即會自動附帶。**未設定時寫入 API 為公開**，啟動時會印出警告。
- CORS 目前為 `Access-Control-Allow-Origin: *`。正式部署前請限制來源，並考慮導入完整的使用者/角色認證。
