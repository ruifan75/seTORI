# seTORI コードレビュー報告

> レビュー日：2026-06-28 · 範囲：`backend/`（Go）、`frontend/`（React/TS）、`docker/`、CI 設定
> ビルド状態：`go vet ./...` ✅ · `go build ./...` ✅ · `tsc -b --noEmit` ✅（すべて通過）

本ドキュメントは完全なレビューで発見された事項を記録します。**[修正済]** と記載されたものは今回のコミットで処理済みです。それ以外は今後の対応用に記録しています。

---

## 1. 重大 — セキュリティ

### 1.1 実在の API キーがバージョン管理にコミットされた **[追跡は修正済、手動ローテーションが必要]**
`backend/.env` に実在の `HOLODEX_API_KEY`、`HOLODEX_EDITOR_TOKEN`、`GROQ_API_KEY`、`YOUTUBE_API_KEY` が含まれ、`backend/bin/server`（10 MB のビルド成果物）もコミットされていました。どちらも `backend/.gitignore` に入っていたものですが、gitignore 追加前に先にコミットしてしまったものです。

- **実施済**：`git rm --cached backend/.env backend/bin/server`（ファイルはディスクに残し、インデックスからのみ削除）。
- **未対応**：キーは git 履歴に残っている → **すべてのトークンをローテーション**してください。リポジトリが外部に漏れる可能性がある場合は `git filter-repo` / BFG で `.env` の履歴を除去してください。

### 1.2 認証なし + CORS 全開放 **[一部修正]**
`internal/handler/router.go` がすべてのオリジンに `Access-Control-Allow-Origin: *` を返しており、元々すべての書き込みエンドポイントに認証がありませんでした。

- **実施済**：任意選択の Bearer トークンゲートを追加。`API_AUTH_TOKEN` を設定すると POST/PUT/DELETE に `Authorization: Bearer <token>` が必要になります（GET と `/health` は公開のまま）。未設定時は公開で起動時に警告を表示します。フロントエンドの `VITE_API_TOKEN` が自動で付与します。非破壊的で、ローカル開発には影響しません。
- **未対応**：CORS オリジンホワイトリスト。複数ユーザー/ロールが必要になった場合は完全な JWT ログインを導入（`config.JWTSecret`、`models.User` はまだ予約のみ）。

---

## 2. 中 — 正確性 / リソース

### 2.1 iTunes client にタイムアウトなし **[修正済]**
`pkg/itunes/client.go` の `NewClient()` が `&http.Client{}`（`Timeout` なし）を使っていたため、Apple エンドポイントへのリクエストが無期限にハングする可能性がありました。他の client（holodex/youtube/groq）は 30~60s のタイムアウトがあります。`Timeout: 30 * time.Second` を追加しました。

### 2.2 ループ内での `defer resp.Body.Close()` **[修正済]**
`service/holodex_service.go` の `SyncSetoriToHolodex` で、「performance ごとに Holodex へ PUT」するループ内で `defer resp.Body.Close()` を使っていました。すべてのレスポンスボディが関数終了まで閉じられず、接続リークが発生していました。ループ内で読み込み後に即時 `resp.Body.Close()` するよう修正しました。
> 同じパターンが `pkg/holodex/client.go` の `AddSongs()` にもありますが、現在は呼び出し元がない（デッドコード）ため未修正。

### 2.3 `POST /api/streams` が誤解を招くスタブだった **[修正済]**
`handleCreateStream` が `200 OK` + `{"message":"TODO: Create stream"}` を返しており、呼び出し側が作成成功と誤解する恐れがありました（フロントの `streamApi` は実際には呼んでいませんでした）。`501 Not Implemented` を返し、Holodex 同期を使うようメッセージを変更しました。

---

## 3. 低 — パフォーマンス（アーキテクチャ）

### 3.1 リストクエリの N+1 **[修正済]**
元々：
- `SongService.GetAll`：曲ごとに `GetPerformanceCount` と `FindBySongID`（iTunes）を 1 回ずつ実行。
- `StreamService.GetAll`：stream ごとに `GetTags` / `GetSingers` / `GetChannelOwner` を 1 回ずつ実行。

バッチクエリ（`= ANY($1)`）に変更：`SongRepository.GetPerformanceCounts`、`SongItunesRepository.FindBySongIDs`、`StreamRepository.GetTagsForStreams`、`StreamRepository.GetSingersForStreams` を追加。各リストエンドポイントは固定 3 クエリ（list + 2 バッチ）で、件数に比例した線形増加がなくなりました。`toSongResponse` を `buildSongResponse` に分離してバッチと単一で共有。

### 3.2 RateLimiter がロック保持中に `Sleep`
`pkg/ratelimit/ratelimit.go` の `Wait()` が上限に達したときに `mu` を保持したまま `time.Sleep` します（window 全体に達する可能性）。現在「一度に 1 チャンネル同期」というユースケースでは許容できます（事実上直列化できている）が、アンチパターンであり、高並行時には全 goroutine がブロックされます。

---

## 4. 低 — 保守性 / デッドコード

呼び出し元が一切存在しないコード（定義のみ）：
- `service/end_time_estimate_service.go`：`addRateLimit()`、`ParseTimestamp()`（`pkg/comment` 内のタイムスタンプ解析と重複）
- `pkg/comment/estimator.go`：`EstimateEndTimes()`、`AssignOrderIndex()`（終了時間推定は実際には service 層で行う）
- `pkg/comment/dedup.go`：`MergeSongs()`
- `pkg/ratelimit`：`CanRequest()`
- `pkg/holodex/client.go`：`AddSongs()`、`GetChannelVideos()`、`SearchVideos()`
- `internal/repository/stream_repository.go`：`FindByDateRange()`、`CheckHashChanged()`、`UpdateStatus()`

その他：
- `dto.AnalyzeCommentsResponse.RawComments` / フロント `AnalyzeCommentsResponse.raw_comments` はバックエンドが一度も値を設定していない。
- `config.Load()` のシグネチャは `error` を返すが、決してエラーを返さない。
- `handleItunesSearch` / `handleItunesQueryByID` はリクエストごとに `itunes.NewClient()` と `songRepo` の再構築を行っている。既存インスタンスを注入するよう変更可能。
- `models.User` / `dto.LoginRequest`/`LoginResponse` は未実装の認証用に予約されている。

---

## 5. フロントエンド

全体的に良好：`tsc` 通過、`any` の使用は極めて少ない（`YoutubePlayer` の YT IFrame API と少数の axios エラーハンドラに集中）。

- **`pages/StreamDetailPage.tsx`（2329 行）**：主編集画面。ロジックと状態が高度に集中しており、最も分割すべきファイル（プレイヤー、コメント分析、タイムライン、Holodex 提案、AI 正規化をそれぞれコンポーネント + hooks に抽出可能）。
- `components/YoutubePlayer.tsx` は module レベルの `playerInstance` シングルトンを使用しており、同一ページに複数プレイヤーがあると干渉する（現在は単一使用のみの前提）。
- フロントエンドテストなし。

---

## 適用済み修正のサマリー

| ファイル | 変更 |
|------|------|
| `backend/pkg/itunes/client.go` | HTTP client に 30s タイムアウトを追加 |
| `backend/internal/service/holodex_service.go` | ループ内 `defer` body リークを修正 |
| `backend/internal/handler/router.go` | `POST /api/streams` を 501 に変更；任意 Bearer トークン認証ミドルウェアを追加 |
| `backend/internal/config/config.go` | `API_AUTH_TOKEN` 設定を追加 |
| `backend/pkg/comment/dedup.go` | `mergeParsedSong` のコメントを整合（完全なアーティスト名を優先、推定終了時間は非優先） |
| `backend/internal/repository/*`, `service/*` | リストエンドポイントの N+1 をバッチクエリに変更 |
| `frontend/src/api/client.ts` | `VITE_API_TOKEN` 設定時に自動で Bearer トークンを付与 |
| `backend/.env.example` | `API_AUTH_TOKEN` の説明を追加 |
| （git インデックス + 履歴） | `backend/.env`、`backend/bin/server` の追跡を解除し履歴から除去 |
| ドキュメント | `README.md`、本 `CODE_REVIEW.md` を追加 |
