package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/ruifan75/setori/pkg/itunes"
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

	// AI サービス：複数 provider ローテーション + failover、未設定時は GROQ_API_KEY にフォールバック
	aiService := service.NewAIService(aiProviderRepo, cfg.GroqAPIKey)

	// services を作成
	songService := service.NewSongService(songRepo, perfRepo, songItunesRepo)
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
	performanceService := service.NewPerformanceService(perfRepo, songRepo, songItunesRepo)
	itunesClient := itunes.NewClient()
	endTimeEstimateService := service.NewEndTimeEstimateService(itunesClient)

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
	}

	if cfg.APIAuthToken == "" {
		logger.Warnf("API_AUTH_TOKEN が未設定です：書き込み API は現在公開です。本番環境ではこの値を設定して認証を有効にしてください")
	}

	r.setupRoutes()
	return r
}

func (r *Router) setupRoutes() {
	// Health check
	r.mux.HandleFunc("GET /health", r.handleHealth)

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
	r.mux.HandleFunc("POST /api/streams/{id}/comments/analyze", r.handleAnalyzeComments)
	r.mux.HandleFunc("POST /api/comments/backfill", r.handleBackfillCommentSongs)
	r.mux.HandleFunc("POST /api/streams/{id}/analyze-chat-ends", r.handleAnalyzeChatEnds)
	r.mux.HandleFunc("POST /api/chat-ends/backfill", r.handleBackfillChatEnds)

	// Filter keywords management
	r.mux.HandleFunc("GET /api/filter-keywords", r.handleListFilterKeywords)
	r.mux.HandleFunc("POST /api/filter-keywords", r.handleCreateFilterKeyword)
	r.mux.HandleFunc("DELETE /api/filter-keywords/{id}", r.handleDeleteFilterKeyword)

	// Tag management
	r.mux.HandleFunc("GET /api/stream-tags", r.handleListStreamTags)
	r.mux.HandleFunc("POST /api/stream-tags", r.handleCreateStreamTag)
	r.mux.HandleFunc("DELETE /api/stream-tags/{id}", r.handleDeleteStreamTag)
	r.mux.HandleFunc("GET /api/performance-tags", r.handleListPerformanceTags)
	r.mux.HandleFunc("POST /api/performance-tags", r.handleCreatePerformanceTag)
	r.mux.HandleFunc("DELETE /api/performance-tags/{id}", r.handleDeletePerformanceTag)

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

	// 認証 middleware：API_AUTH_TOKEN 設定時は書き込み操作に Bearer token が必要
	if !r.authorized(req) {
		respondError(w, http.StatusUnauthorized, "認証が必要です")
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

// ========== Song Handlers ==========

func (r *Router) handleListSongs(w http.ResponseWriter, req *http.Request) {
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	search := req.URL.Query().Get("search")

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	result, err := r.songService.GetAll(page, limit, search)
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

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	result, err := r.streamService.GetAll(page, limit)
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

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	result, err := r.singerService.GetAll(page, limit)
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

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	result, err := r.singerService.GetPerformances(id, page, limit)
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

	comments, err := r.holodexService.GetVideoComments(videoID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"video_id": videoID,
		"comments": comments,
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

// ========== Auth ==========

// authorized 判斷請求是否通過認證。
// 規則：未設定 API_AUTH_TOKEN 時一律放行（公開）；
// 設定後，安全方法（GET/HEAD/OPTIONS）與 /health 仍公開，
// 其餘寫入操作需帶 Authorization: Bearer <token>。
func (r *Router) authorized(req *http.Request) bool {
	if r.cfg.APIAuthToken == "" {
		return true
	}

	token := strings.TrimSpace(strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
	authed := token == r.cfg.APIAuthToken

	// AI provider 設定一律需要認證（含 GET）——避免外洩 provider 設定
	if strings.HasPrefix(req.URL.Path, "/api/ai-providers") {
		return authed
	}

	// 其餘：安全方法（GET/HEAD/OPTIONS）與 /health 維持公開
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if req.URL.Path == "/health" {
		return true
	}

	return authed
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
