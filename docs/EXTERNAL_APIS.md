# 外部 API 用途對照

> 部署模型：**方案 A — 單租戶，全部使用擁有者(營運者)的 key**。
> 危險/計費的操作應鎖在 `API_AUTH_TOKEN` 認證之後（見最後一節）。
> 本文件依 2026-06-28 程式碼現況整理。

## 總覽

| API | 環境變數 | 方向 | 主要用途 | 計費 / 限額 | 上線敏感度（方案 A） |
|-----|----------|:---:|----------|------------|----------------------|
| Holodex（讀） | `HOLODEX_API_KEY` | 讀 | 拉頻道/影片/歌單/留言 | 免費，~80 req/2min | 🟡 共用 OK，靠 rate limit |
| Holodex Editor | `HOLODEX_EDITOR_TOKEN` | **寫** | 把 seTORI 歌單上傳回 Holodex | 綁你的 Holodex 帳號身分 | 🔴 **務必限管理員** |
| Groq | `GROQ_API_KEY` | LLM | 歌名正規化、留言 hybrid 解析 | 依 token 計費 / 限額 | 🔴 **限管理員或配額** |
| YouTube Data | `YOUTUBE_API_KEY` | 讀 | 取頻道高解析頭像 | 10,000 units/日/專案 | 🟡 共用 OK |
| iTunes | （免 key） | 讀 | 歌曲元資料、時長、封面 | ~20 req/min（Apple） | 🟢 無 key，免煩惱 |

---

## 1. Holodex API（讀） — `HOLODEX_API_KEY`

- **程式位置**：`pkg/holodex/client.go`（auth header `X-APIKEY`），client 端 rate limiter 設 75 req/2min。
- **實際呼叫的端點**：
  - `GET /channels/{id}` — 頻道資訊（名稱、英文名、頭像、事務所）
  - `GET /videos/{id}` — 影片詳情；`?c=1` 時含留言；不帶參數時含 `songs` + `channel` + `mentions`
  - `GET /videos?channel_id=&type=stream&status=past&include=songs,mentions` — 分頁（50/頁）抓頻道所有歌回
- **觸發路由**：
  - `POST /api/sync/holodex`（整個頻道同步）
  - `POST /api/sync/holodex/video/{id}`（單一影片同步）
  - `GET /api/streams/{id}/holodex-songs`（即時載入歌單）
  - `GET /api/streams/{id}/comments`、`POST /api/streams/{id}/comments/analyze`（抓留言）
  - `POST /api/singers`（只同步頻道資訊）
- **備註**：`GetChannelVideos`、`SearchVideos` 已定義但未使用。

## 2. Holodex Editor Token（寫） — `HOLODEX_EDITOR_TOKEN`

- **程式位置**：`internal/service/holodex_service.go` 的 `SyncSetoriToHolodex`。
- **動作**：對每筆 performance 發 `PUT https://holodex.net/api/v2/songs`，帶 `Authorization: Bearer <editor_token>` 與 `Origin: https://holodex.net`，把歌曲（含 iTunes 元資料）上傳回 Holodex；會先比對 Holodex 既有歌曲避免重複。
- **觸發路由**：`POST /api/sync/holodex/to-holodex/{id}`
- **⚠️ 風險**：這是用**你的 Holodex 身分**寫入。所有上傳都掛在你名下 → 公開情境下必須限管理員。未設定或為佔位字串時會直接回錯誤、不執行。

## 3. Groq API（LLM） — `GROQ_API_KEY`

- **多 provider failover**：可在**管理介面（設定頁 → AIプロバイダー管理）**登記多個 OpenAI 相容 provider（Groq / Gemini / Cerebras / OpenRouter / Ollama…），`internal/service/ai_service.go` 以 **嚴格優先序 + failover** 呼叫：永遠先用 priority 最小（最前）的 provider，遇到 429/5xx 失敗時換下一個，並把出錯的 provider 短暫冷卻 60 秒跳過（避免反覆戳到 usage limit）。未登記任何 provider 時，退回環境變數 `GROQ_API_KEY`（向後相容）。provider 設定存於 `ai_providers` 表，API key 不回傳前端（僅末四碼）。
- **程式位置**：`pkg/ai/groq.go` — OpenAI 相容 `POST {base_url}/chat/completions`，預設 Groq（model `llama-3.3-70b-versatile`），temperature 0.1，max_tokens 2048。
- **用途與觸發路由**：
  1. **歌名正規化**：`internal/service/normalization_service.go` 的 `BatchAINormalization` — 批次將原始歌名正規化（去除演出版本標記、產生平假名讀音、偵測版本標籤），失敗時降級為純 DB 比對。觸發：`POST /api/ai/normalize`。
  2. **留言 hybrid 解析**：`internal/service/comment_service.go` 的 `AnalyzeComments` → `pkg/comment/ai_parser.go` 的 `ParseCommentsWithAI` — **AI 負責判斷哪些行是歌曲 + start/end 秒數，正則負責抽取歌名/歌手文字**（避免 LLM 竄改日文歌名）。於 edit-time（使用者點「分析」）執行，未設定 key 或 AI 失敗時自動退回純正則。觸發：`POST /api/streams/{id}/comments/analyze`。
- **⚠️ 風險**：按 token 計費，公開且用你的 key 時一個使用者就能燒爆帳單 / 用光額度。未設定 key 時 AI 步驟失敗會被容錯（仍回傳原始資料 / 純正則結果）。
- **未來加強**：可讓 AI 一併回傳歌名/歌手，但需加上「verbatim 子字串驗證」護欄才採用，否則退回正則切分。

## 4. YouTube Data API（讀，選用） — `YOUTUBE_API_KEY`

- **程式位置**：`pkg/youtube/client.go` — `GET /youtube/v3/channels?part=snippet&id=`，取頻道頭像（優先 high → medium → default）。
- **用途**：`HolodexService.getChannelPhotoURL` 在同步頻道/歌手時，**優先**用 YouTube 高解析頭像；取不到或未設定 key 時 fallback 到 Holodex 的 `photo`，再 fallback 到 Holodex 靜態圖。
- **觸發路由**（間接，於頻道同步時）：`POST /api/sync/holodex`、`POST /api/sync/holodex/video/{id}`、`POST /api/singers`。
- **限額**：預設每專案 10,000 units/日（`channels.list` 約 1 unit/次）。

## 5. iTunes Search / Lookup API（免 key）

- **程式位置**：`pkg/itunes/client.go` — `GET itunes.apple.com/search`（`entity=song&country=JP&limit=10`）與 `GET .../lookup?id=`（`country=JP&lang=ja_jp`）。已加 30s 逾時。
- **用途與觸發路由**：
  - 綁定 iTunes Track、取封面/專輯：`GET /api/itunes/search`、`GET /api/itunes/{id}`
  - 用時長（`trackTimeMillis`）估算演唱結束時間：`POST /api/streams/{id}/estimate-end-times`（`end_time_estimate_service.go`）
  - 上傳回 Holodex 前補齊歌曲元資料：`POST /api/sync/holodex/to-holodex/{id}`（`holodex_service.go`）
- **限額**：Apple 無 key 但有 rate limit（一般約 20 req/min）。

---

## 方案 A 上線待辦（建議）

把下列「會花錢 / 會以你身分寫出去」的路由鎖在 `API_AUTH_TOKEN`（已實作的 Bearer 閘門）之後，只開放管理員：

- 🔴 `POST /api/sync/holodex/to-holodex/{id}`（Holodex 寫入）
- 🔴 `POST /api/ai/normalize`（Groq 計費）
- 🔴 `POST /api/streams/{id}/comments/analyze`（Groq 計費 — 留言 hybrid 解析）
- 🟡（可選）`POST /api/sync/holodex`、`POST /api/sync/holodex/video/{id}`（吃 Holodex/YouTube 額度）

> 目前的 `authorized()` 中介層已對「所有非 GET 請求」要求 token（設定 `API_AUTH_TOKEN` 後生效），因此上述 POST 端點預設就會被保護；若日後想做「公開唯讀 + 管理員寫入」以外更細的分級（例如部分 POST 公開、部分限管理員），再於 middleware 加白名單即可。
