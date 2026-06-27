# seTORI 程式碼審查報告

> 審查日期：2026-06-28 · 範圍：`backend/`（Go）、`frontend/`（React/TS）、`docker/`、CI 設定
> 編譯狀態：`go vet ./...` ✅ · `go build ./...` ✅ · `tsc -b --noEmit` ✅（皆通過）

本文件記錄一次完整 review 的發現。標示 **[已修復]** 者已於本次提交處理；其餘為記錄供後續處理。

---

## 1. 嚴重 — 安全性

### 1.1 真實 API 金鑰被提交進版控 **[已修復追蹤，需手動輪替]**
`backend/.env` 含真實的 `HOLODEX_API_KEY`、`HOLODEX_EDITOR_TOKEN`、`GROQ_API_KEY`、`YOUTUBE_API_KEY`，且 `backend/bin/server`（10 MB 編譯產物）也被提交。兩者其實都已在 `backend/.gitignore` 內，是在加入 gitignore 前就先 commit 進去的。

- **已做**：`git rm --cached backend/.env backend/bin/server`（檔案保留在磁碟，僅移出索引）。
- **仍須處理**：金鑰已存在 git 歷史 → **輪替所有 token**；如 repo 會外流，使用 `git filter-repo` / BFG 清除歷史中的 `.env`。

### 1.2 無認證且 CORS 全開 **[部分修復]**
`internal/handler/router.go` 對所有來源回 `Access-Control-Allow-Origin: *`，且原本所有寫入端點皆無認證。

- **已做**：新增選用的 Bearer token 閘門。設定 `API_AUTH_TOKEN` 後，POST/PUT/DELETE 需帶 `Authorization: Bearer <token>`（GET 與 `/health` 維持公開）；未設定則維持公開並於啟動印出警告。前端 `VITE_API_TOKEN` 會自動附帶。此為非破壞性、預設不影響本機開發。
- **仍須處理**：CORS 來源白名單；若需多使用者/角色，再導入完整 JWT 登入（`config.JWTSecret`、`models.User` 仍為預留）。

---

## 2. 中 — 正確性 / 資源

### 2.1 iTunes client 沒有逾時 **[已修復]**
`pkg/itunes/client.go` 的 `NewClient()` 用 `&http.Client{}`（無 `Timeout`），對 Apple 端點的請求可能無限期掛住。其他 client（holodex/youtube/groq）都有 30~60s 逾時。已補上 `Timeout: 30 * time.Second`。

### 2.2 迴圈內 `defer resp.Body.Close()` **[已修復]**
`service/holodex_service.go` 的 `SyncSetoriToHolodex` 在「逐首 performance PUT 到 Holodex」的迴圈內用 `defer resp.Body.Close()`，所有回應 body 會累積到整個函式結束才關閉（連線洩漏）。已改為迴圈內讀取後立即 `resp.Body.Close()`。
> 相同 pattern 也存在於 `pkg/holodex/client.go` 的 `AddSongs()`，但該函式目前無呼叫者（死碼），未動。

### 2.3 `POST /api/streams` 是會誤導的 stub **[已修復]**
`handleCreateStream` 原本回 `200 OK` + `{"message":"TODO: Create stream"}`，會讓呼叫端誤以為建立成功（前端 `streamApi` 並未呼叫它）。已改為回 `501 Not Implemented` 並提示改用 Holodex 同步。

---

## 3. 低 — 效能（架構）

### 3.1 列表查詢的 N+1 **[已修復]**
原本：
- `SongService.GetAll`：對每首歌各跑一次 `GetPerformanceCount` 與 `FindBySongID`（iTunes）。
- `StreamService.GetAll`：對每筆 stream 各跑一次 `GetTags` / `GetSingers` / `GetChannelOwner`。

已改為批次查詢（`= ANY($1)`）：新增 `SongRepository.GetPerformanceCounts`、`SongItunesRepository.FindBySongIDs`、`StreamRepository.GetTagsForStreams`、`StreamRepository.GetSingersForStreams`，每個列表端點固定 3 次查詢（list + 2 批次），不再隨筆數線性增加。`toSongResponse` 拆出 `buildSongResponse` 供批次與單筆共用。

### 3.2 RateLimiter 在持鎖期間 `Sleep`
`pkg/ratelimit/ratelimit.go` 的 `Wait()` 在達到上限時於持有 `mu` 的情況下 `time.Sleep`（最長可達整個 window）。對目前「同步一次一個」的使用情境可接受（實際上達成了序列化的效果），但屬反模式，高併發時所有 goroutine 會被卡住。

---

## 4. 低 — 可維護性 / 死碼

未被任何呼叫者使用的程式（僅定義）：
- `service/end_time_estimate_service.go`：`addRateLimit()`、`ParseTimestamp()`（與 `pkg/comment` 內的時間戳解析重複）
- `pkg/comment/estimator.go`：`EstimateEndTimes()`、`AssignOrderIndex()`（結束時間估算實際走 service 層）
- `pkg/comment/dedup.go`：`MergeSongs()`
- `pkg/ratelimit`：`CanRequest()`
- `pkg/holodex/client.go`：`AddSongs()`、`GetChannelVideos()`、`SearchVideos()`
- `internal/repository/stream_repository.go`：`FindByDateRange()`、`CheckHashChanged()`、`UpdateStatus()`

其他：
- `dto.AnalyzeCommentsResponse.RawComments` / 前端 `AnalyzeCommentsResponse.raw_comments` 從未由後端填值。
- `config.Load()` 簽章回傳 `error` 但永不出錯。
- `handleItunesSearch` / `handleItunesQueryByID` 每次請求都 `itunes.NewClient()` 並重建 `songRepo`，可改為注入既有實例。
- `models.User` / `dto.LoginRequest`/`LoginResponse` 為未實作認證預留。

---

## 5. 前端

整體狀況良好：`tsc` 通過、`any` 使用極少（集中在 `YoutubePlayer` 對 YT IFrame API 與少數 axios error handler）。

- **`pages/StreamDetailPage.tsx`（2329 行）**：主編輯介面，邏輯/狀態高度集中，是最該拆分的檔案（播放器、留言分析、時間軸、Holodex 建議、AI 正規化可各自抽成元件 + hooks）。
- `components/YoutubePlayer.tsx` 使用 module-level 的 `playerInstance` 單例，多個播放器同頁時會互相干擾（目前僅單一使用情境）。
- 無前端測試。

---

## 已套用的修復摘要

| 檔案 | 變更 |
|------|------|
| `backend/pkg/itunes/client.go` | 補上 HTTP client 30s 逾時 |
| `backend/internal/service/holodex_service.go` | 修正迴圈內 `defer` body 洩漏 |
| `backend/internal/handler/router.go` | `POST /api/streams` 改回 501；新增選用 Bearer token 認證 middleware |
| `backend/internal/config/config.go` | 新增 `API_AUTH_TOKEN` 設定 |
| `backend/pkg/comment/dedup.go` | `mergeParsedSong` 對齊註解（優先較完整藝人名、非估計結束時間） |
| `backend/internal/repository/*`, `service/*` | 列表端點 N+1 改為批次查詢 |
| `frontend/src/api/client.ts` | 設定 `VITE_API_TOKEN` 時自動附帶 Bearer token |
| `backend/.env.example` | 新增 `API_AUTH_TOKEN` 說明 |
| （git 索引 + 歷史） | 移除追蹤並從歷史清除 `backend/.env`、`backend/bin/server` |
| 文件 | 新增 `README.md`、本 `CODE_REVIEW.md` |
