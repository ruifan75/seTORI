package handler

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/config"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/auth"
	"github.com/ruifan75/setori/pkg/gdrive"
	"github.com/ruifan75/setori/pkg/itunes"
	"github.com/ruifan75/setori/pkg/youtube"
)

// Router HTTP ルーター
type Router struct {
	db  *sql.DB
	cfg *config.Config
	mux *http.ServeMux

	songService          *service.SongService
	streamService        *service.StreamService
	singerService        *service.SingerService
	holodexService       *service.HolodexService
	commentService       *service.CommentService
	normalizationService *service.NormalizationService
	performanceService   *service.PerformanceService
	endTimeEstimate      *service.EndTimeEstimateService
	filterKeywordRepo    *repository.FilterKeywordRepository
	tagRepo              *repository.TagRepository
	aiProviderRepo       *repository.AIProviderRepository
	chatEndService       *service.ChatEndService
	artistService        *service.ArtistService
	batchAnalyzeService  *service.BatchAnalyzeService
	authService          *service.AuthService
	readingService       *service.ReadingService
	suggestionService    *service.SuggestionService
	backupService        *service.BackupService
	playlistService      *service.PlaylistService
}

// NewRouter 新しいルーターを作成
func NewRouter(db *sql.DB, cfg *config.Config) *Router {
	// repositories を作成
	songRepo := repository.NewSongRepository(db)
	singerRepo := repository.NewSingerRepository(db)
	streamRepo := repository.NewStreamRepository(db)
	perfRepo := repository.NewPerformanceRepository(db)
	songItunesRepo := repository.NewSongItunesRepository(db)
	filterKeywordRepo := repository.NewFilterKeywordRepository(db)
	tagRepo := repository.NewTagRepository(db)
	aiProviderRepo := repository.NewAIProviderRepository(db)
	authRepo := repository.NewAuthRepository(db)
	artistRepo := repository.NewArtistRepository(db)

	// AI サービス：複数 provider ローテーション + failover、未設定時は GROQ_API_KEY にフォールバック
	aiService := service.NewAIService(aiProviderRepo, cfg.GroqAPIKey)

	// services を作成
	songService := service.NewSongService(songRepo, perfRepo, songItunesRepo, artistRepo)
	artistService := service.NewArtistService(artistRepo, songRepo, aiService)
	streamService := service.NewStreamService(streamRepo, perfRepo)
	singerService := service.NewSingerService(singerRepo, streamRepo, perfRepo)
	holodexService := service.NewHolodexService(cfg.HolodexAPIKey, cfg.YouTubeAPIKey, cfg.GroqAPIKey, streamRepo, singerRepo, cfg.HolodexEditorToken)
	holodexService.SetRepositoriesWithSongItunes(perfRepo, songRepo, songItunesRepo) // SyncSetoriToHolodex に必要な repositories を提供
	normalizationService := service.NewNormalizationService(aiService, songRepo, songItunesRepo)
	chatEndService := service.NewChatEndService(streamRepo, "", "")
	// CommentService は分析時に正規化・拍手 end を内部で実行する（抽出→正規化→end→キャッシュ）
	commentService := service.NewCommentService(holodexService, streamRepo, filterKeywordRepo, aiService, normalizationService, chatEndService)
	// HolodexService も AnalyzeHolodexSongs で正規化・拍手 end を実行する（holodex_hash キャッシュ）
	holodexService.SetAnalysisServices(normalizationService, chatEndService)
	batchAnalyzeService := service.NewBatchAnalyzeService(commentService, streamRepo)
	performanceService := service.NewPerformanceService(perfRepo, songRepo, songItunesRepo, artistRepo)
	itunesClient := itunes.NewClient()
	endTimeEstimateService := service.NewEndTimeEstimateService(itunesClient)
	authService := service.NewAuthService(authRepo)
	readingService := service.NewReadingService(artistRepo, songRepo)
	suggestionRepo := repository.NewSuggestionRepository(db)
	suggestionService := service.NewSuggestionService(suggestionRepo, songService, artistService)
	appSettingsRepo := repository.NewAppSettingsRepository(db)
	driveClient := gdrive.NewClient(cfg.GoogleOAuthClientID, cfg.GoogleOAuthSecret)
	backupService := service.NewBackupService(db, appSettingsRepo, driveClient, cfg.DatabaseURL, cfg.BackupDir, cfg.BackupDockerContainer)
	playlistRepo := repository.NewPlaylistRepository(db, perfRepo)
	playlistService := service.NewPlaylistService(playlistRepo)

	r := &Router{
		db:                   db,
		cfg:                  cfg,
		mux:                  http.NewServeMux(),
		songService:          songService,
		streamService:        streamService,
		singerService:        singerService,
		holodexService:       holodexService,
		commentService:       commentService,
		endTimeEstimate:      endTimeEstimateService,
		normalizationService: normalizationService,
		performanceService:   performanceService,
		filterKeywordRepo:    filterKeywordRepo,
		tagRepo:              tagRepo,
		aiProviderRepo:       aiProviderRepo,
		chatEndService:       chatEndService,
		artistService:        artistService,
		batchAnalyzeService:  batchAnalyzeService,
		authService:          authService,
		readingService:       readingService,
		suggestionService:    suggestionService,
		backupService:        backupService,
		playlistService:      playlistService,
	}

	r.setupRoutes()
	return r
}

// AuthService は main.go でのブートストラップ（初期管理者作成）に使う。
func (r *Router) AuthService() *service.AuthService {
	return r.authService
}

// BackupService は main.go での自動バックアップスケジューラ起動に使う。
func (r *Router) BackupService() *service.BackupService {
	return r.backupService
}

func (r *Router) setupRoutes() {
	// Health check
	r.mux.HandleFunc("GET /health", r.handleHealth)

	// 認証
	r.mux.HandleFunc("POST /api/auth/login", r.handleLogin)
	r.mux.HandleFunc("POST /api/auth/logout", r.handleLogout)
	r.mux.HandleFunc("GET /api/auth/me", r.handleMe)

	// ユーザー・ロール・権限管理（要 users:manage）
	r.mux.HandleFunc("GET /api/users", r.handleListUsers)
	r.mux.HandleFunc("POST /api/users", r.handleCreateUser)
	r.mux.HandleFunc("PUT /api/users/{id}", r.handleUpdateUser)
	r.mux.HandleFunc("PUT /api/users/{id}/password", r.handleChangeUserPassword)
	r.mux.HandleFunc("DELETE /api/users/{id}", r.handleDeleteUser)
	r.mux.HandleFunc("GET /api/roles", r.handleListRoles)
	r.mux.HandleFunc("POST /api/roles", r.handleCreateRole)
	r.mux.HandleFunc("PUT /api/roles/{id}", r.handleUpdateRole)
	r.mux.HandleFunc("DELETE /api/roles/{id}", r.handleDeleteRole)
	r.mux.HandleFunc("GET /api/permissions", r.handleListPermissions)

	// 統一検索（楽曲・歌枠・チャンネル・YouTube URL/video ID）
	r.mux.HandleFunc("GET /api/search", r.handleGlobalSearch)
	// 複合条件の配信検索（キーワード × チャンネル × タグ AND）。
	// リテラルパターンのため /api/streams/{id} より優先マッチする。
	r.mux.HandleFunc("GET /api/streams/search", r.handleSearchStreams)

	// 未処理配信の一括プレ分析（背景ジョブ、singleton）
	r.mux.HandleFunc("POST /api/streams/batch-analyze", r.handleStartBatchAnalyze)
	r.mux.HandleFunc("POST /api/streams/batch-analyze/cancel", r.handleCancelBatchAnalyze)
	r.mux.HandleFunc("GET /api/streams/batch-analyze/status", r.handleBatchAnalyzeStatus)

	// API routes - Songs
	r.mux.HandleFunc("GET /api/songs", r.handleListSongs)
	r.mux.HandleFunc("GET /api/songs/{id}", r.handleGetSong)
	r.mux.HandleFunc("GET /api/songs/{id}/performances", r.handleGetSongPerformances)
	r.mux.HandleFunc("POST /api/songs", r.handleCreateSong)
	r.mux.HandleFunc("PUT /api/songs/{id}", r.handleUpdateSong)
	r.mux.HandleFunc("DELETE /api/songs/{id}", r.handleDeleteSong)
	r.mux.HandleFunc("POST /api/songs/{id}/merge", r.handleMergeSong)

	// API routes - Streams
	r.mux.HandleFunc("GET /api/streams", r.handleListStreams)
	r.mux.HandleFunc("GET /api/streams/{id}", r.handleGetStream)
	r.mux.HandleFunc("POST /api/streams", r.handleCreateStream)
	r.mux.HandleFunc("PUT /api/streams/{id}", r.handleUpdateStream)

	// API routes - Artists（原曲アーティスト）
	r.mux.HandleFunc("GET /api/artists", r.handleListArtists)
	r.mux.HandleFunc("GET /api/artists/{id}", r.handleGetArtist)
	r.mux.HandleFunc("PUT /api/artists/{id}", r.handleUpdateArtist)
	r.mux.HandleFunc("POST /api/artists/{id}/merge", r.handleMergeArtist)
	r.mux.HandleFunc("POST /api/ai/backfill-readings", r.handleBackfillReadings)
	// 読みデータのエクスポート/インポート（外部 AI で読みを作成する運用向け）
	r.mux.HandleFunc("GET /api/readings/export", r.handleExportReadings)
	r.mux.HandleFunc("POST /api/readings/import", r.handleImportReadings)

	// 修正提案：投稿は閲覧モードでも可、一覧/承認/却下は content:edit
	r.mux.HandleFunc("POST /api/suggestions", r.handleCreateSuggestion)
	r.mux.HandleFunc("GET /api/suggestions", r.handleListSuggestions)
	r.mux.HandleFunc("GET /api/suggestions/count", r.handleCountSuggestions)
	r.mux.HandleFunc("POST /api/suggestions/{id}/approve", r.handleApproveSuggestion)
	r.mux.HandleFunc("POST /api/suggestions/{id}/reject", r.handleRejectSuggestion)

	// プレイリスト
	// 公開範囲の判定は行単位なので、認可は各ハンドラ（PlaylistService）側で行う。
	// 限定公開 URL は /api/shared/playlists/... に分ける。/api/playlists/shared/{slug} だと
	// /api/playlists/{id}/items と「どちらも /api/playlists/shared/items に一致し、
	// どちらがより具体的とも言えない」ため ServeMux が起動時に panic する。
	r.mux.HandleFunc("GET /api/playlists/public", r.handleListPublicPlaylists)
	r.mux.HandleFunc("GET /api/shared/playlists/{slug}", r.handleGetSharedPlaylist)
	r.mux.HandleFunc("GET /api/shared/playlists/{slug}/items", r.handleListSharedPlaylistItems)
	r.mux.HandleFunc("GET /api/playlists", r.handleListMyPlaylists)
	r.mux.HandleFunc("POST /api/playlists", r.handleCreatePlaylist)
	r.mux.HandleFunc("GET /api/playlists/{id}", r.handleGetPlaylist)
	r.mux.HandleFunc("PUT /api/playlists/{id}", r.handleUpdatePlaylist)
	r.mux.HandleFunc("DELETE /api/playlists/{id}", r.handleDeletePlaylist)
	r.mux.HandleFunc("GET /api/playlists/{id}/items", r.handleListPlaylistItems)
	r.mux.HandleFunc("POST /api/playlists/{id}/items", r.handleAddPlaylistItem)
	r.mux.HandleFunc("DELETE /api/playlists/{id}/items/{performanceId}", r.handleRemovePlaylistItem)
	r.mux.HandleFunc("PUT /api/playlists/{id}/order", r.handleReorderPlaylist)

	// API routes - Singers
	r.mux.HandleFunc("GET /api/singers", r.handleListSingers)
	r.mux.HandleFunc("GET /api/singers/search", r.handleSearchSingers)
	r.mux.HandleFunc("GET /api/singers/{id}", r.handleGetSinger)
	r.mux.HandleFunc("GET /api/singers/{id}/streams", r.handleGetSingerStreams)
	r.mux.HandleFunc("GET /api/singers/{id}/performances", r.handleGetSingerPerformances)
	r.mux.HandleFunc("POST /api/singers", r.handleCreateSinger)
	r.mux.HandleFunc("PUT /api/singers/{id}", r.handleUpdateSinger)

	// Holodex sync
	r.mux.HandleFunc("POST /api/sync/holodex", r.handleSyncHolodex)
	r.mux.HandleFunc("POST /api/sync/holodex/video/{id}", r.handleSyncHolodexVideo)
	r.mux.HandleFunc("POST /api/sync/holodex/to-holodex/{id}", r.handleSyncSetoriToHolodex)

	// Load songs from Holodex (without adding to normalization queue)
	r.mux.HandleFunc("GET /api/streams/{id}/holodex-songs", r.handleLoadHolodexSongs)
	r.mux.HandleFunc("POST /api/streams/{id}/holodex-songs/analyze", r.handleAnalyzeHolodexSongs)

	// Estimate end times
	r.mux.HandleFunc("POST /api/streams/{id}/estimate-end-times", r.handleEstimateEndTimes)

	// Create performances directly
	r.mux.HandleFunc("POST /api/streams/{id}/performances", r.handleCreatePerformances)
	r.mux.HandleFunc("DELETE /api/streams/{id}/performances", r.handleDeletePerformances)

	// Comment analysis
	r.mux.HandleFunc("GET /api/streams/{id}/comments", r.handleGetComments)
	r.mux.HandleFunc("POST /api/streams/{id}/comments/sync-youtube", r.handleSyncYouTubeComments)
	r.mux.HandleFunc("POST /api/streams/{id}/comments/analyze", r.handleAnalyzeComments)
	r.mux.HandleFunc("POST /api/comments/backfill", r.handleBackfillCommentSongs)
	r.mux.HandleFunc("POST /api/comments/backfill-hashes", r.handleBackfillCommentSongsHashes)
	r.mux.HandleFunc("POST /api/streams/{id}/analyze-chat-ends", r.handleAnalyzeChatEnds)
	r.mux.HandleFunc("POST /api/streams/{id}/chat-end-estimate", r.handleEstimateChatEnds)
	r.mux.HandleFunc("POST /api/chat-ends/backfill", r.handleBackfillChatEnds)

	// Filter keywords management
	r.mux.HandleFunc("GET /api/filter-keywords", r.handleListFilterKeywords)
	r.mux.HandleFunc("POST /api/filter-keywords", r.handleCreateFilterKeyword)
	r.mux.HandleFunc("DELETE /api/filter-keywords/{id}", r.handleDeleteFilterKeyword)

	// タグ検索（タグが付いた配信・演出の一覧）
	r.mux.HandleFunc("GET /api/stream-tags/{id}/streams", r.handleGetStreamsByTag)
	r.mux.HandleFunc("GET /api/performance-tags/{id}/performances", r.handleGetPerformancesByTag)

	// 首頁：おすすめ
	r.mux.HandleFunc("GET /api/performances/random", r.handleRandomPerformances)

	// Tag management
	r.mux.HandleFunc("GET /api/stream-tags", r.handleListStreamTags)
	r.mux.HandleFunc("POST /api/stream-tags", r.handleCreateStreamTag)
	r.mux.HandleFunc("DELETE /api/stream-tags/{id}", r.handleDeleteStreamTag)
	r.mux.HandleFunc("GET /api/performance-tags", r.handleListPerformanceTags)
	r.mux.HandleFunc("POST /api/performance-tags", r.handleCreatePerformanceTag)
	r.mux.HandleFunc("DELETE /api/performance-tags/{id}", r.handleDeletePerformanceTag)

	// タイトル自動タグ付けルール（stream tag をタイトルの文字列一致で付与）
	r.mux.HandleFunc("GET /api/tag-keyword-rules", r.handleListTagKeywordRules)
	r.mux.HandleFunc("POST /api/tag-keyword-rules", r.handleCreateTagKeywordRule)
	r.mux.HandleFunc("DELETE /api/tag-keyword-rules/{id}", r.handleDeleteTagKeywordRule)
	r.mux.HandleFunc("POST /api/tag-rules/backfill", r.handleBackfillTagRules)

	// AI normalization (for direct editing flow)
	r.mux.HandleFunc("POST /api/ai/normalize", r.handleBatchAINormalization)

	// AI provider 設定（管理員）
	r.mux.HandleFunc("GET /api/ai-providers", r.handleListAIProviders)
	r.mux.HandleFunc("POST /api/ai-providers", r.handleCreateAIProvider)
	r.mux.HandleFunc("PUT /api/ai-providers/{id}", r.handleUpdateAIProvider)
	r.mux.HandleFunc("DELETE /api/ai-providers/{id}", r.handleDeleteAIProvider)
	r.mux.HandleFunc("GET /api/ai-providers/{id}/models", r.handleListAIProviderModels)
	r.mux.HandleFunc("POST /api/ai-providers/models/preview", r.handlePreviewAIProviderModels)

	// Log management (for admin)
	r.mux.HandleFunc("GET /api/logs", r.handleGetLogs)
	r.mux.HandleFunc("PUT /api/logs/level", r.handleSetLogLevel)

	// DB バックアップ/リストア（要 backup:manage）
	r.mux.HandleFunc("GET /api/backups", r.handleBackupStatus)
	r.mux.HandleFunc("POST /api/backups", r.handleCreateBackup)
	r.mux.HandleFunc("PUT /api/backups/settings", r.handleUpdateBackupSettings)
	r.mux.HandleFunc("POST /api/backups/restore-upload", r.handleRestoreUpload)
	r.mux.HandleFunc("GET /api/backups/{name}/download", r.handleDownloadBackup)
	r.mux.HandleFunc("POST /api/backups/{name}/restore", r.handleRestoreBackup)
	r.mux.HandleFunc("DELETE /api/backups/{name}", r.handleDeleteBackup)

	// Google Drive 連携（要 backup:manage）
	r.mux.HandleFunc("POST /api/backups/gdrive/auth/start", r.handleGDriveAuthStart)
	r.mux.HandleFunc("POST /api/backups/gdrive/auth/poll", r.handleGDriveAuthPoll)
	r.mux.HandleFunc("DELETE /api/backups/gdrive", r.handleGDriveDisconnect)
	r.mux.HandleFunc("GET /api/backups/gdrive/files", r.handleGDriveListFiles)
	r.mux.HandleFunc("DELETE /api/backups/gdrive/files/{id}", r.handleGDriveDeleteFile)
	r.mux.HandleFunc("POST /api/backups/gdrive/files/{id}/restore", r.handleGDriveRestoreFile)

	// iTunes API
	r.mux.HandleFunc("GET /api/itunes/search", r.handleItunesSearch)
	r.mux.HandleFunc("GET /api/itunes/{id}", r.handleItunesQueryByID)
}

// ServeHTTP http.Handler インターフェースを実装
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	start := time.Now()

	// CORS middleware
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 認証：Bearer トークンから現在のユーザーを解決（未ログインなら nil）
	user, err := r.resolveUser(req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "認証処理に失敗しました")
		return
	}
	if user != nil {
		req = withUser(req, user)
	}

	// 認可：メソッド＋パスから必要権限を求めて判定
	if !authorize(req.Method, req.URL.Path, user) {
		if user == nil {
			respondError(w, http.StatusUnauthorized, "ログインが必要です")
		} else {
			respondError(w, http.StatusForbidden, "この操作を行う権限がありません")
		}
		return
	}

	// リクエストを記録
	if req.URL.Path == "/api/logs" {
		logger.Debugf("[%s] %s %s", req.Method, req.URL.Path, req.RemoteAddr)
	} else {
		logger.Infof("[%s] %s %s", req.Method, req.URL.Path, req.RemoteAddr)
	}

	r.mux.ServeHTTP(w, req)

	// リクエストを記録完成時間
	if req.URL.Path == "/api/logs" {
		logger.Debugf("[%s] %s completed in %v", req.Method, req.URL.Path, time.Since(start))
	} else {
		logger.Infof("[%s] %s completed in %v", req.Method, req.URL.Path, time.Since(start))
	}
}

// Health check handler
func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ========== Global Search Handler ==========

// handleGlobalSearch 楽曲・歌枠・チャンネルを横断検索する。
// 入力が YouTube URL / video ID の場合は video_id と登録状況を返し、テキスト検索はスキップする。
func (r *Router) handleGlobalSearch(w http.ResponseWriter, req *http.Request) {
	query := strings.TrimSpace(req.URL.Query().Get("q"))
	if query == "" {
		respondError(w, http.StatusBadRequest, "検索キーワードが必要です")
		return
	}

	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit < 1 || limit > 20 {
		limit = 5
	}

	resp := dto.GlobalSearchResponse{
		Query:           query,
		Songs:           []dto.SongResponse{},
		Streams:         []dto.SearchStreamItem{},
		Singers:         []dto.SingerResponse{},
		Artists:         []dto.ArtistResponse{},
		StreamTags:      []dto.SearchTagItem{},
		PerformanceTags: []dto.SearchTagItem{},
	}

	// YouTube URL / video ID 判定。該当すればテキスト検索はせず登録状況のみ返す。
	if videoID := youtube.ParseVideoID(query); videoID != "" {
		resp.VideoID = videoID
		registered, err := r.streamService.Exists(videoID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp.VideoRegistered = registered
		respondJSON(w, http.StatusOK, resp)
		return
	}

	// テキスト検索：楽曲・歌枠・チャンネルを横断。個別の失敗は空リストで続行する。
	if songs, err := r.songService.GetAll(1, limit, query, "", ""); err != nil {
		logger.Warnf("global search songs failed: %v", err)
	} else if songs != nil {
		resp.Songs = songs.Songs
	}

	if streams, err := r.streamService.SearchByTitle(query, limit); err != nil {
		logger.Warnf("global search streams failed: %v", err)
	} else {
		resp.Streams = streams
	}

	if singers, err := r.singerService.Search(query, limit); err != nil {
		logger.Warnf("global search singers failed: %v", err)
	} else {
		resp.Singers = singers
	}

	// 原曲アーティスト（名前・読みの部分一致、曲数の多い順＝関連度の代用）
	if artists, err := r.artistService.GetAll(1, limit, query, "songs", ""); err != nil {
		logger.Warnf("global search artists failed: %v", err)
	} else {
		resp.Artists = artists.Artists
	}

	// タグ（id / 表示名の部分一致、使用件数付き）
	if tags, err := r.tagRepo.SearchStreamTags(query, limit); err != nil {
		logger.Warnf("global search stream tags failed: %v", err)
	} else {
		resp.StreamTags = toSearchTagItems(tags)
	}
	if tags, err := r.tagRepo.SearchPerformanceTags(query, limit); err != nil {
		logger.Warnf("global search performance tags failed: %v", err)
	} else {
		resp.PerformanceTags = toSearchTagItems(tags)
	}

	respondJSON(w, http.StatusOK, resp)
}

func toSearchTagItems(tags []repository.TagWithCount) []dto.SearchTagItem {
	items := make([]dto.SearchTagItem, len(tags))
	for i, t := range tags {
		items[i] = dto.SearchTagItem{ID: t.ID, DisplayName: t.DisplayName, Color: t.Color, Count: t.Count}
	}
	return items
}

// handleSearchStreams は非表示を含め、配信元・参加者・ボーカル・タグを組み合わせて配信を検索する。
func (r *Router) handleSearchStreams(w http.ResponseWriter, req *http.Request) {
	filters := models.StreamSearchFilters{
		Query:             strings.TrimSpace(req.URL.Query().Get("q")),
		OwnerID:           strings.TrimSpace(req.URL.Query().Get("owner_id")),
		ParticipantIDs:    parseIDQueryParams(req, "participant_ids", "participant_id", "singer_id"),
		VocalistIDs:       parseIDQueryParams(req, "vocalist_ids", "vocalist_id"),
		StreamTagIDs:      parseCSVQueryParam(req, "tags"),
		PerformanceTagIDs: parseCSVQueryParam(req, "performance_tags"),
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	result, err := r.streamService.SearchStreams(filters, page, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func parseCSVQueryParam(req *http.Request, key string) []string {
	raw := strings.TrimSpace(req.URL.Query().Get(key))
	if raw == "" {
		return nil
	}

	seen := make(map[string]struct{})
	values := make([]string, 0)
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func parseUUIDCSVQueryParam(req *http.Request, key string) []string {
	seen := make(map[string]struct{})
	values := make([]string, 0)
	for _, rawID := range parseCSVQueryParam(req, key) {
		id, err := uuid.Parse(rawID)
		if err != nil {
			continue
		}
		normalized := id.String()
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		values = append(values, normalized)
	}
	return values
}

// parseIDQueryParams は複数形パラメータと旧単値パラメータを重複なしで結合する。
func parseIDQueryParams(req *http.Request, multiKey string, legacyKeys ...string) []string {
	values := parseCSVQueryParam(req, multiKey)
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, key := range legacyKeys {
		for _, value := range parseCSVQueryParam(req, key) {
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			values = append(values, value)
		}
	}
	return values
}

// ========== Batch Analyze Handlers ==========

// handleStartBatchAnalyze 未処理配信の一括プレ分析を開始する（content:edit）。
func (r *Router) handleStartBatchAnalyze(w http.ResponseWriter, req *http.Request) {
	mode := req.URL.Query().Get("mode")
	if mode == "" {
		mode = service.BatchModeUnprocessed
	}
	if err := r.batchAnalyzeService.Start(mode); err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	logger.Infof("batch analyze started")
	respondJSON(w, http.StatusAccepted, map[string]string{"message": "一括分析を開始しました"})
}

func (r *Router) handleCancelBatchAnalyze(w http.ResponseWriter, req *http.Request) {
	r.batchAnalyzeService.Cancel()
	respondJSON(w, http.StatusOK, map[string]string{"message": "キャンセルを要求しました"})
}

func (r *Router) handleBatchAnalyzeStatus(w http.ResponseWriter, req *http.Request) {
	respondJSON(w, http.StatusOK, r.batchAnalyzeService.Status())
}

// ========== Artist Handlers ==========

func (r *Router) handleListArtists(w http.ResponseWriter, req *http.Request) {
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	search := req.URL.Query().Get("search")
	sort := req.URL.Query().Get("sort")
	dir := req.URL.Query().Get("dir")

	result, err := r.artistService.GetAll(page, limit, search, sort, dir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleGetArtist(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なアーティストID")
		return
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	sort := req.URL.Query().Get("sort")
	dir := req.URL.Query().Get("dir")

	result, err := r.artistService.GetByID(id, page, limit, sort, dir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "アーティストが見つかりません")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleUpdateArtist は名前・読み仮名を更新する（content:edit）。
// 名前変更時は所属する全楽曲の original_artist テキストも連動更新される。
func (r *Router) handleUpdateArtist(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なアーティストID")
		return
	}
	var body dto.UpdateArtistRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	result, err := r.artistService.Update(id, body.Name, body.NameReading)
	if err != nil {
		if errors.Is(err, service.ErrArtistNameTaken) {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "アーティストが見つかりません")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleMergeArtist は source アーティストを target に統合する（content:edit）。
func (r *Router) handleMergeArtist(w http.ResponseWriter, req *http.Request) {
	sourceID, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なアーティストID")
		return
	}
	var body dto.MergeArtistRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	targetID, err := uuid.Parse(body.TargetArtistID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な対象アーティストID")
		return
	}

	result, err := r.artistService.Merge(sourceID, targetID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "アーティストが見つかりません")
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleBackfillReadings は読み仮名の AI 補完を実行する（1回で各対象最大30件処理）。
func (r *Router) handleBackfillReadings(w http.ResponseWriter, req *http.Request) {
	result, err := r.artistService.BackfillReadings()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleExportReadings はアーティスト・楽曲の読みを一括出力する。
// ?filter=needs_fix で未整備のみ、?format=csv で CSV（type,id,name,reading）出力。
func (r *Router) handleExportReadings(w http.ResponseWriter, req *http.Request) {
	onlyNeedsFix := req.URL.Query().Get("filter") == "needs_fix"
	data, err := r.readingService.Export(onlyNeedsFix)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if req.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="readings.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"type", "id", "name", "reading"})
		for _, a := range data.Artists {
			_ = cw.Write([]string{"artist", a.ID, a.Name, a.Reading})
		}
		for _, s := range data.Songs {
			_ = cw.Write([]string{"song", s.ID, s.Name, s.Reading})
		}
		cw.Flush()
		return
	}

	respondJSON(w, http.StatusOK, data)
}

// handleImportReadings は読みデータを取り込む（content:edit）。JSON または CSV を受け付ける。
func (r *Router) handleImportReadings(w http.ResponseWriter, req *http.Request) {
	var data dto.ReadingsExport

	if strings.HasPrefix(req.Header.Get("Content-Type"), "text/csv") {
		parsed, err := parseReadingsCSV(req.Body)
		if err != nil {
			respondError(w, http.StatusBadRequest, "CSV の解析に失敗しました: "+err.Error())
			return
		}
		data = *parsed
	} else if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	result, err := r.readingService.Import(&data)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// parseReadingsCSV は type,id,name,reading の CSV を ReadingsExport に変換する。
// ヘッダー行（type で始まる）は読み飛ばす。列数が不足する行は無視する。
func parseReadingsCSV(rd io.Reader) (*dto.ReadingsExport, error) {
	cr := csv.NewReader(rd)
	cr.FieldsPerRecord = -1 // 列数のばらつきを許容
	out := &dto.ReadingsExport{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rec) < 4 {
			continue
		}
		typ := strings.TrimSpace(rec[0])
		item := dto.ReadingItem{ID: strings.TrimSpace(rec[1]), Name: rec[2], Reading: rec[3]}
		switch typ {
		case "artist":
			out.Artists = append(out.Artists, item)
		case "song":
			out.Songs = append(out.Songs, item)
		}
	}
	return out, nil
}

// ========== Suggestion Handlers（修正提案） ==========

// handleCreateSuggestion は修正提案を投稿する（閲覧モードでも可・匿名可）。
func (r *Router) handleCreateSuggestion(w http.ResponseWriter, req *http.Request) {
	var body dto.CreateSuggestionRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	if len(body.Fields) == 0 {
		respondError(w, http.StatusBadRequest, "提案する変更がありません")
		return
	}

	sug, err := r.suggestionService.Create(&body)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTarget), errors.Is(err, service.ErrNoChange):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrTargetNotFound):
			respondError(w, http.StatusNotFound, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"message": "修正提案を送信しました。管理者の確認をお待ちください",
		"id":      sug.ID,
	})
}

// handleListSuggestions は提案一覧を返す（content:edit）。?status=pending|approved|rejected で絞る。
func (r *Router) handleListSuggestions(w http.ResponseWriter, req *http.Request) {
	status := req.URL.Query().Get("status")
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	result, err := r.suggestionService.List(status, page, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleCountSuggestions は未処理の提案数を返す（バッジ表示用、content:edit）。
func (r *Router) handleCountSuggestions(w http.ResponseWriter, req *http.Request) {
	n, err := r.suggestionService.CountPending()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"pending": n})
}

// handleApproveSuggestion は提案を承認して対象へ反映する（content:edit）。
func (r *Router) handleApproveSuggestion(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な提案ID")
		return
	}
	if err := r.suggestionService.Approve(id); err != nil {
		switch {
		case errors.Is(err, service.ErrSuggestionNotFound):
			respondError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrAlreadyReviewed):
			respondError(w, http.StatusConflict, err.Error())
		default:
			respondError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "提案を承認して反映しました"})
}

// handleRejectSuggestion は提案を却下する（content:edit）。
func (r *Router) handleRejectSuggestion(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な提案ID")
		return
	}
	if err := r.suggestionService.Reject(id); err != nil {
		switch {
		case errors.Is(err, service.ErrSuggestionNotFound):
			respondError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrAlreadyReviewed):
			respondError(w, http.StatusConflict, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "提案を却下しました"})
}

// ========== Tag Search Handlers ==========

// handleGetStreamsByTag 指定タグが付いた配信一覧（タグ検索ページ）
func (r *Router) handleGetStreamsByTag(w http.ResponseWriter, req *http.Request) {
	tagID := req.PathValue("id")
	if tagID == "" {
		respondError(w, http.StatusBadRequest, "無効なタグID")
		return
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	result, err := r.streamService.GetByTag(tagID, page, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleGetPerformancesByTag 指定の演出タグが付いた演出一覧（タグ検索ページ）
func (r *Router) handleGetPerformancesByTag(w http.ResponseWriter, req *http.Request) {
	tagID := req.PathValue("id")
	if tagID == "" {
		respondError(w, http.StatusBadRequest, "無効なタグID")
		return
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	result, err := r.streamService.GetPerformancesByTag(tagID, page, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// ========== Home Handlers（ランダム再生） ==========

// handleRandomPerformances 曲単位で重複しないランダムな歌唱一覧（公開）
func (r *Router) handleRandomPerformances(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	excludedSongIDs := parseUUIDCSVQueryParam(req, "exclude_song_ids")
	result, err := r.streamService.GetRandomPerformances(limit, excludedSongIDs)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// ========== Song Handlers ==========

func (r *Router) handleListSongs(w http.ResponseWriter, req *http.Request) {
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	search := req.URL.Query().Get("search")
	sort := req.URL.Query().Get("sort")
	dir := req.URL.Query().Get("dir")

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	result, err := r.songService.GetAll(page, limit, search, sort, dir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleGetSong(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な曲ID")
		return
	}

	result, err := r.songService.GetByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "曲が見つかりません")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleGetSongPerformances(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な曲ID")
		return
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	result, err := r.songService.GetPerformances(id, page, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "曲が見つかりません")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleCreateSong(w http.ResponseWriter, req *http.Request) {
	var songReq dto.CreateSongRequest
	if err := json.NewDecoder(req.Body).Decode(&songReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	if songReq.Name == "" || songReq.OriginalArtist == "" {
		respondError(w, http.StatusBadRequest, "曲名と原曲アーティストは必須です")
		return
	}

	result, err := r.songService.Create(&songReq)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, result)
}

func (r *Router) handleUpdateSong(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な曲ID")
		return
	}

	var songReq dto.UpdateSongRequest
	if err := json.NewDecoder(req.Body).Decode(&songReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	if songReq.Name == "" || songReq.OriginalArtist == "" {
		respondError(w, http.StatusBadRequest, "曲名と原曲アーティストは必須です")
		return
	}

	result, err := r.songService.Update(id, &songReq)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "曲が見つかりません")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// handleDeleteSong 刪除歌曲
func (r *Router) handleDeleteSong(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な曲ID")
		return
	}

	// 檢查歌曲是否存在
	song, err := r.songService.GetByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if song == nil {
		respondError(w, http.StatusNotFound, "曲が見つかりません")
		return
	}

	// 刪除歌曲
	if err := r.songService.Delete(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "曲を削除しました",
		"id":      id.String(),
	})
}

// handleMergeSong 將來源歌曲合併至目標歌曲
func (r *Router) handleMergeSong(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	sourceSongID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な元の曲ID")
		return
	}

	var mergeReq struct {
		TargetSongID string `json:"target_song_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&mergeReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	targetSongID, err := uuid.Parse(mergeReq.TargetSongID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な対象の曲ID")
		return
	}

	// 確保來源和目標是不同的歌曲
	if sourceSongID == targetSongID {
		respondError(w, http.StatusBadRequest, "元の曲と対象の曲は同じにできません")
		return
	}

	// 驗證兩首歌曲都存在
	sourceSong, err := r.songService.GetByID(sourceSongID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sourceSong == nil {
		respondError(w, http.StatusNotFound, "元の曲が見つかりません")
		return
	}

	targetSong, err := r.songService.GetByID(targetSongID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if targetSong == nil {
		respondError(w, http.StatusNotFound, "対象の曲が見つかりません")
		return
	}

	// 執行合併
	if err := r.songService.MergeSongs(sourceSongID, targetSongID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":     "曲を統合しました",
		"source_id":   sourceSongID.String(),
		"target_id":   targetSongID.String(),
		"target_song": targetSong,
	})
}

// ========== Stream Handlers ==========

func (r *Router) handleListStreams(w http.ResponseWriter, req *http.Request) {
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	sort := req.URL.Query().Get("sort")
	dir := req.URL.Query().Get("dir")

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	result, err := r.streamService.GetAll(page, limit, sort, dir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleGetStream(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "無効な歌枠ID")
		return
	}

	result, err := r.streamService.GetByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "歌枠が見つかりません")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleCreateStream(w http.ResponseWriter, req *http.Request) {
	// 直播只能透過 Holodex 同步建立（POST /api/sync/holodex/video/{id}），
	// 尚未提供手動建立的入口。回傳 501 以避免讓呼叫端誤以為已建立成功。
	respondError(w, http.StatusNotImplemented, "ストリームの手動作成は未対応です。Holodex同期を使用してください")
}

func (r *Router) handleUpdateStream(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "無効な歌枠ID")
		return
	}

	var streamReq dto.UpdateStreamRequest
	if err := json.NewDecoder(req.Body).Decode(&streamReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	result, err := r.streamService.Update(id, &streamReq)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "歌枠が見つかりません")
		return
	}

	logger.Infof("stream %s metadata updated (title=%v, processed=%v)", id, streamReq.Title, streamReq.IsProcessed)
	respondJSON(w, http.StatusOK, result)
}

// ========== Singer Handlers ==========

func (r *Router) handleListSingers(w http.ResponseWriter, req *http.Request) {
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	sort := req.URL.Query().Get("sort")
	dir := req.URL.Query().Get("dir")

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	result, err := r.singerService.GetAll(page, limit, sort, dir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleSearchSingers(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query().Get("q")
	if query == "" {
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}

	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 10
	}

	result, err := r.singerService.Search(query, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleGetSinger(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "無効なチャンネルID")
		return
	}

	result, err := r.singerService.GetByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "チャンネルが見つかりません")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleGetSingerStreams(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "無効なチャンネルID")
		return
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	// 解析篩選參數
	var processedFilter, hiddenFilter *bool

	// processed: "all" (nil), "true", "false"
	if processedStr := req.URL.Query().Get("processed"); processedStr != "" && processedStr != "all" {
		processed := processedStr == "true"
		processedFilter = &processed
	}

	// hidden: "all", "true" (只看隱藏), "false" (不顯示隱藏，預設)
	hiddenStr := req.URL.Query().Get("hidden")
	if hiddenStr == "" {
		// 預設不顯示隱藏的
		hidden := false
		hiddenFilter = &hidden
	} else if hiddenStr == "true" {
		hidden := true
		hiddenFilter = &hidden
	} else if hiddenStr == "false" {
		hidden := false
		hiddenFilter = &hidden
	}
	// hiddenStr == "all" 時，hiddenFilter 保持 nil

	result, err := r.singerService.GetStreams(id, page, limit, processedFilter, hiddenFilter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleGetSingerPerformances(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "無効なチャンネルID")
		return
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	sort := req.URL.Query().Get("sort")
	dir := req.URL.Query().Get("dir")

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	result, err := r.singerService.GetPerformances(id, page, limit, sort, dir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "チャンネルが見つかりません")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleCreateSinger(w http.ResponseWriter, req *http.Request) {
	var singerReq dto.CreateSingerRequest
	if err := json.NewDecoder(req.Body).Decode(&singerReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	singerReq.ID = strings.TrimSpace(singerReq.ID)

	if singerReq.ID == "" {
		respondError(w, http.StatusBadRequest, "チャンネルID、handle、またはURLは必須です")
		return
	}

	// 只同步頻道資訊，不同步直播
	singer, err := r.holodexService.SyncChannelInfo(singerReq.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 返回成功訊息
	respondJSON(w, http.StatusCreated, dto.CreateSingerResponse{
		Message: "チャンネルを追加しました",
		ID:      singer.ID,
		Name:    singer.Name,
	})
}

func (r *Router) handleUpdateSinger(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "チャンネルIDは必須です")
		return
	}

	var singerReq dto.UpdateSingerRequest
	if err := json.NewDecoder(req.Body).Decode(&singerReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	result, err := r.singerService.UpdateManualMetadata(id, &singerReq)
	if err != nil {
		if errors.Is(err, service.ErrSingerMetadataManagedByHolodex) {
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		if errors.Is(err, service.ErrSingerNameRequired) {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "チャンネルが見つかりません")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// ========== Holodex Sync Handlers ==========

func (r *Router) handleSyncHolodex(w http.ResponseWriter, req *http.Request) {
	var syncReq dto.SyncHolodexRequest
	if err := json.NewDecoder(req.Body).Decode(&syncReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	if syncReq.ChannelID == "" {
		respondError(w, http.StatusBadRequest, "channel_id は必須です")
		return
	}

	limit := syncReq.Limit
	if limit <= 0 {
		limit = 50
	}

	result, err := r.holodexService.SyncChannel(syncReq.ChannelID, limit, syncReq.ForceUpdate)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Infof("holodex sync channel %s completed: synced=%d new=%d", syncReq.ChannelID, result.SyncedCount, len(result.NewStreams))
	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleSyncHolodexVideo(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}

	result, err := r.holodexService.SyncVideo(videoID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Infof("holodex sync video %s completed", videoID)
	respondJSON(w, http.StatusOK, result)
}

// 同步 seTORI 資料到 Holodex
func (r *Router) handleSyncSetoriToHolodex(w http.ResponseWriter, req *http.Request) {
	streamID := req.PathValue("id")
	if streamID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}

	result, err := r.holodexService.SyncSetoriToHolodex(streamID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// ========== Load Holodex Songs Handler ==========

func (r *Router) handleLoadHolodexSongs(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}

	result, err := r.holodexService.LoadHolodexSongs(videoID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// ========== Estimate End Times Handler ==========

func (r *Router) handleEstimateEndTimes(w http.ResponseWriter, req *http.Request) {
	streamID := req.PathValue("id")
	if streamID == "" {
		respondError(w, http.StatusBadRequest, "無効な歌枠ID")
		return
	}

	var estimateReq dto.EstimateEndTimesRequest
	if err := json.NewDecoder(req.Body).Decode(&estimateReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	result, err := r.endTimeEstimate.EstimateEndTimes(&estimateReq)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// ========== Performance Handlers ==========

func (r *Router) handleCreatePerformances(w http.ResponseWriter, req *http.Request) {
	streamID := req.PathValue("id")
	if streamID == "" {
		respondError(w, http.StatusBadRequest, "無効な歌枠ID")
		return
	}

	var createReq dto.CreatePerformancesRequest
	if err := json.NewDecoder(req.Body).Decode(&createReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	if len(createReq.Performances) == 0 {
		respondError(w, http.StatusBadRequest, "最低でも1曲は必要です")
		return
	}

	result, err := r.performanceService.CreatePerformances(streamID, createReq.Performances)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Infof("created %d performances for stream %s", len(createReq.Performances), streamID)
	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleDeletePerformances(w http.ResponseWriter, req *http.Request) {
	streamID := req.PathValue("id")
	if streamID == "" {
		respondError(w, http.StatusBadRequest, "無効な歌枠ID")
		return
	}

	if err := r.performanceService.DeleteByStreamID(streamID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Infof("deleted all performances for stream %s", streamID)
	respondJSON(w, http.StatusOK, dto.SuccessResponse{
		Success: true,
		Message: "すべての演奏記録を削除しました",
	})
}

// ========== Comment Analysis Handlers ==========

func (r *Router) handleGetComments(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}

	comments, err := r.commentService.GetRawComments(videoID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"video_id": videoID,
		"comments": comments,
	})
}

func (r *Router) handleSyncYouTubeComments(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}

	count, err := r.commentService.SyncYouTubeCommentRaw(videoID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"video_id":      videoID,
		"comment_count": count,
	})
}

func (r *Router) handleAnalyzeComments(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}

	// ?force=true で快取を無視して再分析（再正規化）する
	force := req.URL.Query().Get("force") == "true"

	result, err := r.commentService.AnalyzeComments(videoID, force)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Infof("/comments/analyze completed for %s: %d songs (AI used or cached)", videoID, len(result.Songs))
	respondJSON(w, http.StatusOK, result)
}

// handleAnalyzeHolodexSongs stored holodex_data を正規化＋DB 照合＋拍手 end 補完し、
// holodex_hash をキーにキャッシュして返す。?force=true でキャッシュ無視。
func (r *Router) handleAnalyzeHolodexSongs(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}

	force := req.URL.Query().Get("force") == "true"

	songs, err := r.holodexService.AnalyzeHolodexSongs(videoID, force)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Infof("/holodex-songs/analyze completed for %s: %d songs (AI used or cached)", videoID, len(songs))
	respondJSON(w, http.StatusOK, map[string]any{"songs": songs})
}

// handleEstimateChatEnds 指定した開始秒数それぞれの拍手 end を推定して返す
// （編集ページで生コメント等から曲を1件追加するときの終了時間推定用）。
func (r *Router) handleEstimateChatEnds(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	var body struct {
		Starts []int `json:"starts"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || len(body.Starts) == 0 {
		respondError(w, http.StatusBadRequest, "無効なリクエスト")
		return
	}
	ends, err := r.chatEndService.EstimateEnds(videoID, body.Starts)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"ends": ends})
}

// handleAnalyzeChatEnds 手動觸發：用 live chat 拍手偵測該歌回各首歌的 end（背景執行）
func (r *Router) handleAnalyzeChatEnds(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	// 下載 live chat 可能要數十秒～數分鐘，背景跑、立即回 202
	r.chatEndService.AnalyzeStreamAsync(videoID)
	respondJSON(w, http.StatusAccepted, map[string]string{
		"message": "拍手解析を開始しました（バックグラウンド処理）",
		"id":      videoID,
	})
}

// handleBackfillChatEnds 對所有有 comment_songs 的歌回補跑拍手 end 偵測（背景、限並發）
func (r *Router) handleBackfillChatEnds(w http.ResponseWriter, req *http.Request) {
	concurrency := 3
	if c := req.URL.Query().Get("concurrency"); c != "" {
		if v, err := strconv.Atoi(c); err == nil && v > 0 {
			concurrency = v
		}
	}
	go r.chatEndService.Backfill(concurrency)
	respondJSON(w, http.StatusAccepted, map[string]interface{}{
		"message":     "拍手 end のバックフィルを開始しました（バックグラウンド、ログ参照）",
		"concurrency": concurrency,
	})
}

// ========== Filter Keywords Handlers ==========

func (r *Router) handleListFilterKeywords(w http.ResponseWriter, req *http.Request) {
	keywords, err := r.filterKeywordRepo.FindAll()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, keywords)
}

func (r *Router) handleCreateFilterKeyword(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Keyword string `json:"keyword"`
		Type    string `json:"type"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	if body.Keyword == "" {
		respondError(w, http.StatusBadRequest, "キーワードは必須です")
		return
	}
	if body.Type != "filter" && body.Type != "keep" {
		respondError(w, http.StatusBadRequest, "typeは 'filter' または 'keep' である必要があります")
		return
	}

	kw, err := r.filterKeywordRepo.Create(body.Keyword, body.Type)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, kw)
}

func (r *Router) handleDeleteFilterKeyword(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}

	if err := r.filterKeywordRepo.Delete(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "キーワードを削除しました"})
}

// handleBackfillCommentSongs 補填所有有 comment_raw 但沒有 comment_songs 的 stream
func (r *Router) handleBackfillCommentSongs(w http.ResponseWriter, req *http.Request) {
	count, err := r.commentService.BackfillCommentSongs()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("%d件のストリームを補填しました", count),
		"count":   count,
	})
}

// handleBackfillCommentSongsHashes は comment_songs_hash を現行の正規化アルゴリズムへ移行する
// （旧: 生bytes sha → 新: 正規化 sha）。AI は呼ばず、快取が効かなくなっていた歌回を修復する。
func (r *Router) handleBackfillCommentSongsHashes(w http.ResponseWriter, req *http.Request) {
	res, err := r.commentService.BackfillCommentSongsHashes()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// ========== Tag Management Handlers ==========

func (r *Router) handleListStreamTags(w http.ResponseWriter, req *http.Request) {
	tags, err := r.tagRepo.FindAllStreamTags()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, tags)
}

func (r *Router) handleCreateStreamTag(w http.ResponseWriter, req *http.Request) {
	var body struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Color       string `json:"color"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	if body.ID == "" || body.DisplayName == "" {
		respondError(w, http.StatusBadRequest, "IDと表示名は必須です")
		return
	}

	tag, err := r.tagRepo.CreateStreamTag(body.ID, body.DisplayName, body.Color)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, tag)
}

func (r *Router) handleDeleteStreamTag(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	if err := r.tagRepo.DeleteStreamTag(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "タグを削除しました"})
}

func (r *Router) handleListPerformanceTags(w http.ResponseWriter, req *http.Request) {
	tags, err := r.tagRepo.FindAllPerformanceTags()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, tags)
}

func (r *Router) handleCreatePerformanceTag(w http.ResponseWriter, req *http.Request) {
	var body struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Color       string `json:"color"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	if body.ID == "" || body.DisplayName == "" {
		respondError(w, http.StatusBadRequest, "IDと表示名は必須です")
		return
	}

	tag, err := r.tagRepo.CreatePerformanceTag(body.ID, body.DisplayName, body.Color)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, tag)
}

func (r *Router) handleDeletePerformanceTag(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	if err := r.tagRepo.DeletePerformanceTag(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "タグを削除しました"})
}

// ========== Tag Keyword Rule Handlers ==========

func (r *Router) handleListTagKeywordRules(w http.ResponseWriter, req *http.Request) {
	rules, err := r.tagRepo.FindAllTagKeywordRules()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, rules)
}

func (r *Router) handleCreateTagKeywordRule(w http.ResponseWriter, req *http.Request) {
	var body struct {
		TagID   string `json:"tag_id"`
		Keyword string `json:"keyword"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	body.TagID = strings.TrimSpace(body.TagID)
	body.Keyword = strings.TrimSpace(body.Keyword)
	if body.TagID == "" || body.Keyword == "" {
		respondError(w, http.StatusBadRequest, "tag_id とキーワードは必須です")
		return
	}

	rule, err := r.tagRepo.CreateTagKeywordRule(body.TagID, body.Keyword)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, rule)
}

func (r *Router) handleDeleteTagKeywordRule(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.Atoi(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	if err := r.tagRepo.DeleteTagKeywordRule(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "ルールを削除しました"})
}

// handleBackfillTagRules 全配信にルールを適用して既存配信にタグを付与する。
func (r *Router) handleBackfillTagRules(w http.ResponseWriter, req *http.Request) {
	added, err := r.streamService.ApplyTagRulesToAll()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logger.Infof("tag rule backfill added %d tag assignments", added)
	respondJSON(w, http.StatusOK, map[string]any{
		"message": fmt.Sprintf("%d 件のタグを付与しました", added),
		"added":   added,
	})
}

// ========== Batch AI Normalization Handler ==========

func (r *Router) handleBatchAINormalization(w http.ResponseWriter, req *http.Request) {
	var batchReq dto.BatchAINormalizationRequest
	if err := json.NewDecoder(req.Body).Decode(&batchReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	if len(batchReq.Items) == 0 {
		respondError(w, http.StatusBadRequest, "最低でも1曲は必要です")
		return
	}

	result, err := r.normalizationService.BatchAINormalization(batchReq.Items)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logger.Infof("AI normalization requested for %d items, warning=%s", len(batchReq.Items), result.Warning)
	respondJSON(w, http.StatusOK, result)
}

// ========== iTunes API Handlers ==========

func (r *Router) handleItunesSearch(w http.ResponseWriter, req *http.Request) {
	term := req.URL.Query().Get("term")

	if term == "" {
		respondError(w, http.StatusBadRequest, "検索キーワードが必要です")
		return
	}

	// 1. 從 iTunes 搜尋
	itunesClient := itunes.NewClient()
	itunesResult, err := itunesClient.Search(term)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 2. 檢查每個 iTunes ID 是否已在資料庫中
	songRepo := repository.NewSongRepository(r.db)

	enhancedResults := make([]dto.ItunesSearchResultWithSong, 0, len(itunesResult.Results))

	for _, itunesItem := range itunesResult.Results {
		enhanced := dto.ItunesSearchResultWithSong{
			ItunesID:       itunesItem.ItunesID,
			CollectionName: itunesItem.CollectionName,
			TrackName:      itunesItem.TrackName,
			ArtistName:     itunesItem.ArtistName,
			ArtworkURL:     itunesItem.ArtworkURL,
			Country:        itunesItem.Country,
		}

		// 檢查是否已在資料庫中
		existingSong, err := songRepo.FindByItunesID(itunesItem.ItunesID)
		if err != nil {
			logger.Warnf("Error checking iTunes ID %d: %v", itunesItem.ItunesID, err)
		}

		if existingSong != nil {
			// 取得演唱次數
			perfCount, err := songRepo.GetPerformanceCount(existingSong.ID)
			if err != nil {
				logger.Warnf("Error counting performances for song %s: %v", existingSong.ID, err)
				perfCount = 0
			}

			// 轉換 sql.NullString 為 *string
			var nameReading *string
			if existingSong.NameReading.Valid {
				nameReading = &existingSong.NameReading.String
			}

			var originalArtistReading *string
			if existingSong.OriginalArtistReading.Valid {
				originalArtistReading = &existingSong.OriginalArtistReading.String
			}

			var arts *string
			if existingSong.Arts.Valid {
				arts = &existingSong.Arts.String
			}

			enhanced.ExistingSong = &dto.SongBrief{
				ID:                    existingSong.ID,
				Name:                  existingSong.Name,
				NameReading:           nameReading,
				OriginalArtist:        existingSong.OriginalArtist,
				OriginalArtistReading: originalArtistReading,
				Arts:                  arts,
				PerformanceCount:      perfCount,
			}
		}

		enhancedResults = append(enhancedResults, enhanced)
	}

	response := dto.ItunesSearchResponseWithSongs{
		Results: enhancedResults,
	}

	respondJSON(w, http.StatusOK, response)
}

func (r *Router) handleItunesQueryByID(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	if idStr == "" {
		respondError(w, http.StatusBadRequest, "iTunes IDが必要です")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な iTunes ID")
		return
	}

	itunesClient := itunes.NewClient()
	result, err := itunesClient.QueryByID(id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// ========== AI Provider Handlers ==========

func toAIProviderResponse(p models.AIProvider) dto.AIProviderResponse {
	resp := dto.AIProviderResponse{
		ID:             p.ID,
		Name:           p.Name,
		BaseURL:        p.BaseURL,
		Model:          p.Model,
		Enabled:        p.Enabled,
		Priority:       p.Priority,
		TimeoutSeconds: p.TimeoutSeconds,
		HasKey:         p.APIKey != "",
	}
	if n := len(p.APIKey); n > 0 {
		hint := p.APIKey
		if n > 4 {
			hint = p.APIKey[n-4:]
		}
		resp.KeyHint = "…" + hint
	}
	return resp
}

func (r *Router) handleListAIProviders(w http.ResponseWriter, req *http.Request) {
	providers, err := r.aiProviderRepo.FindAll()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := make([]dto.AIProviderResponse, len(providers))
	for i, p := range providers {
		resp[i] = toAIProviderResponse(p)
	}
	respondJSON(w, http.StatusOK, resp)
}

func (r *Router) handleCreateAIProvider(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name           string `json:"name"`
		BaseURL        string `json:"base_url"`
		Model          string `json:"model"`
		APIKey         string `json:"api_key"`
		Enabled        *bool  `json:"enabled"`
		Priority       int    `json:"priority"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	if body.Name == "" || body.BaseURL == "" || body.Model == "" || body.APIKey == "" {
		respondError(w, http.StatusBadRequest, "name, base_url, model, api_key は必須です")
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	timeoutSec := body.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	p := &models.AIProvider{
		Name:           body.Name,
		BaseURL:        body.BaseURL,
		Model:          body.Model,
		APIKey:         body.APIKey,
		Enabled:        enabled,
		Priority:       body.Priority,
		TimeoutSeconds: timeoutSec,
	}
	if err := r.aiProviderRepo.Create(p); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, toAIProviderResponse(*p))
}

func (r *Router) handleUpdateAIProvider(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.Atoi(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}

	existing, err := r.aiProviderRepo.FindByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		respondError(w, http.StatusNotFound, "プロバイダーが見つかりません")
		return
	}

	var body struct {
		Name           *string `json:"name"`
		BaseURL        *string `json:"base_url"`
		Model          *string `json:"model"`
		APIKey         *string `json:"api_key"`
		Enabled        *bool   `json:"enabled"`
		Priority       *int    `json:"priority"`
		TimeoutSeconds *int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	if body.Name != nil {
		existing.Name = *body.Name
	}
	if body.BaseURL != nil {
		existing.BaseURL = *body.BaseURL
	}
	if body.Model != nil {
		existing.Model = *body.Model
	}
	if body.Enabled != nil {
		existing.Enabled = *body.Enabled
	}
	if body.Priority != nil {
		existing.Priority = *body.Priority
	}
	if body.TimeoutSeconds != nil && *body.TimeoutSeconds > 0 {
		existing.TimeoutSeconds = *body.TimeoutSeconds
	}
	// API key 只有在有提供且非空時才更新（留空表示保持原值）
	if body.APIKey != nil && *body.APIKey != "" {
		existing.APIKey = *body.APIKey
	}

	if err := r.aiProviderRepo.Update(existing); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, toAIProviderResponse(*existing))
}

func (r *Router) handleDeleteAIProvider(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.Atoi(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	if err := r.aiProviderRepo.Delete(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "プロバイダーを削除しました"})
}

// handleListAIProviderModels プロバイダーの保存済み API key を使い、
// OpenAI 互換 GET {base}/models を呼んで利用可能なモデル一覧を返す。
func (r *Router) handleListAIProviderModels(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.Atoi(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効なID")
		return
	}
	p, err := r.aiProviderRepo.FindByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		respondError(w, http.StatusNotFound, "プロバイダーが見つかりません")
		return
	}
	if p.APIKey == "" {
		respondError(w, http.StatusBadRequest, "このプロバイダーには API key が設定されていません")
		return
	}

	modelList, err := ai.ListModels(p.BaseURL, p.APIKey)
	if err != nil {
		// プロバイダー側のエラー（401/404 など）はそのまま 502 で伝える
		respondError(w, http.StatusBadGateway, "モデル一覧の取得に失敗しました: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"models": modelList})
}

// handlePreviewAIProviderModels 未保存の base_url + api_key で利用可能なモデルを取得する。
// プロバイダー新規追加フォームで、保存前に model を選べるようにするため。
func (r *Router) handlePreviewAIProviderModels(w http.ResponseWriter, req *http.Request) {
	var body struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	if body.BaseURL == "" || body.APIKey == "" {
		respondError(w, http.StatusBadRequest, "base_url と api_key は必須です")
		return
	}
	modelList, err := ai.ListModels(body.BaseURL, body.APIKey)
	if err != nil {
		respondError(w, http.StatusBadGateway, "モデル一覧の取得に失敗しました: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"models": modelList})
}

// ========== Auth / ACL ==========

// resolveUser は Bearer トークンから現在のユーザーを解決する。
// 未ログインなら (nil, nil)。後方互換として、静的 API_AUTH_TOKEN に一致する場合は
// 全権限を持つ疑似管理者として扱う（既存のスクリプト/デプロイ向け）。
func (r *Router) resolveUser(req *http.Request) (*models.User, error) {
	token := bearerToken(req)
	if token == "" {
		return nil, nil
	}
	if r.cfg.APIAuthToken != "" && token == r.cfg.APIAuthToken {
		return &models.User{
			Username:    "api-token",
			DisplayName: "API Token",
			RoleName:    "admin",
			Permissions: []string{auth.PermAll},
			IsActive:    true,
		}, nil
	}
	return r.authService.Authenticate(token)
}

// authorize はメソッド＋パスに必要な権限を求め、user がそれを満たすか判定する。
func authorize(method, path string, user *models.User) bool {
	perm, needsAuth := requiredPermission(method, path)
	if !needsAuth {
		return true // 公開（未ログインでも可）
	}
	if user == nil {
		return false // 要ログイン
	}
	if perm == "" {
		return true // ログイン済みなら誰でも可（例：/api/auth/me）
	}
	return auth.HasPermission(user.Permissions, perm)
}

// requiredPermission はエンドポイントに必要な権限キーと、認証要否を返す。
// needsAuth=false は公開エンドポイント。perm="" は「ログイン済みなら誰でも可」。
//
// 方針：GET などの読み取りは基本公開（閲覧）。書き込みは content:edit。
// 管理系リソース（ユーザー/ロール/AIプロバイダー/ログ/同期）は読み取りも含めて専用権限が必要。
func requiredPermission(method, path string) (perm string, needsAuth bool) {
	if method == http.MethodOptions {
		return "", false
	}

	// 公開・認証のみ（特定権限不要）のエンドポイント
	switch path {
	case "/health", "/api/auth/login":
		return "", false
	case "/api/auth/logout", "/api/auth/me":
		return "", true
	}

	// 修正提案：投稿（POST /api/suggestions）は閲覧モードでも可（公開）。
	// 一覧・件数・承認・却下は content:edit（管理者レビュー）。
	if path == "/api/suggestions" && method == http.MethodPost {
		return "", false
	}
	if strings.HasPrefix(path, "/api/suggestions") {
		return auth.PermContentEdit, true
	}

	// 限定公開 URL（共有リンク）は未ログインでも閲覧可
	if strings.HasPrefix(path, "/api/shared/playlists") {
		return "", false
	}

	// プレイリストは「自分のもの」を扱う機能なので、編集権限ではなくログインだけを求める
	// （viewer ロールの一般利用者も自分のプレイリストを作れる必要がある）。
	// 誰のものを触れるかという行単位の判定は PlaylistService が行う。
	// 公開・限定公開の閲覧は未ログインでも可。
	if strings.HasPrefix(path, "/api/playlists") {
		if strings.HasPrefix(path, "/api/playlists/public") {
			return "", false
		}
		if method == http.MethodGet {
			// 個別取得は公開のものもあるため未ログインで通し、可否はサービス層で判定する。
			// ただし一覧（GET /api/playlists）は本人のものを返すのでログインが要る。
			if path == "/api/playlists" {
				return "", true
			}
			return "", false
		}
		return "", true // 作成・更新・削除はログイン必須（特定権限は不要）
	}

	// 管理系リソースはメソッドを問わず専用権限が必要
	switch {
	case strings.HasPrefix(path, "/api/users"),
		strings.HasPrefix(path, "/api/roles"),
		path == "/api/permissions":
		return auth.PermUsersManage, true
	case strings.HasPrefix(path, "/api/ai-providers"):
		return auth.PermAIManage, true
	case strings.HasPrefix(path, "/api/logs"):
		return auth.PermLogsView, true
	case strings.HasPrefix(path, "/api/sync"):
		return auth.PermSyncRun, true
	case strings.HasPrefix(path, "/api/backups"):
		// バックアップ/リストアはダウンロード（GET）含め全操作で専用権限が必要
		return auth.PermBackupManage, true
	}

	// それ以外：安全メソッドは公開閲覧、書き込みは content:edit
	switch method {
	case http.MethodGet, http.MethodHead:
		return "", false
	}
	return auth.PermContentEdit, true
}

// ========== Helper Functions ==========

// respondJSON 回傳 JSON 回應
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError 回傳錯誤回應
func respondError(w http.ResponseWriter, status int, message string) {
	logger.Errorf("status=%d message=%s", status, message)
	respondJSON(w, status, dto.ErrorResponse{Error: message})
}

// ========== Log Management Handlers ==========

func (r *Router) handleGetLogs(w http.ResponseWriter, req *http.Request) {
	limit := 100
	if l := req.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	entries := logger.GetRecent(limit)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  entries,
		"level": logger.GetLevel(),
	})
}

func (r *Router) handleSetLogLevel(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	upper := strings.ToUpper(body.Level)
	if upper != "DEBUG" && upper != "INFO" && upper != "WARN" && upper != "ERROR" {
		respondError(w, http.StatusBadRequest, "無効なログレベル（DEBUG/INFO/WARN/ERROR）")
		return
	}

	logger.SetLevel(upper)
	logger.Infof("log level changed to %s", upper)
	respondJSON(w, http.StatusOK, map[string]string{"level": upper})
}
