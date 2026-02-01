package handler

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/config"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/itunes"
)

// Router HTTP 路由器
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
}

// NewRouter 建立新的路由器
func NewRouter(db *sql.DB, cfg *config.Config) *Router {
	// 建立 repositories
	songRepo := repository.NewSongRepository(db)
	singerRepo := repository.NewSingerRepository(db)
	streamRepo := repository.NewStreamRepository(db)
	perfRepo := repository.NewPerformanceRepository(db)
	songItunesRepo := repository.NewSongItunesRepository(db)

	// 建立 services
	songService := service.NewSongService(songRepo, perfRepo, songItunesRepo)
	streamService := service.NewStreamService(streamRepo, perfRepo)
	singerService := service.NewSingerService(singerRepo, streamRepo, perfRepo)
	holodexService := service.NewHolodexService(cfg.HolodexAPIKey, cfg.YouTubeAPIKey, streamRepo, singerRepo)
	commentService := service.NewCommentService(holodexService, streamRepo)
	normalizationService := service.NewNormalizationService(cfg.GroqAPIKey, songRepo)
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

	// Holodex sync
	r.mux.HandleFunc("POST /api/sync/holodex", r.handleSyncHolodex)
	r.mux.HandleFunc("POST /api/sync/holodex/video/{id}", r.handleSyncHolodexVideo)

	// Load songs from Holodex (without adding to normalization queue)
	r.mux.HandleFunc("GET /api/streams/{id}/holodex-songs", r.handleLoadHolodexSongs)

	// Estimate end times
	r.mux.HandleFunc("POST /api/streams/{id}/estimate-end-times", r.handleEstimateEndTimes)

	// Create performances directly
	r.mux.HandleFunc("POST /api/streams/{id}/performances", r.handleCreatePerformances)
	r.mux.HandleFunc("DELETE /api/streams/{id}/performances", r.handleDeletePerformances)

	// Comment analysis
	r.mux.HandleFunc("GET /api/streams/{id}/comments", r.handleGetComments)
	r.mux.HandleFunc("POST /api/streams/{id}/comments/analyze", r.handleAnalyzeComments)

	// AI normalization (for direct editing flow)
	r.mux.HandleFunc("POST /api/ai/normalize", r.handleBatchAINormalization)

	// iTunes API
	r.mux.HandleFunc("GET /api/itunes/search", r.handleItunesSearch)
	r.mux.HandleFunc("GET /api/itunes/{id}", r.handleItunesQueryByID)
}

// ServeHTTP 實作 http.Handler 介面
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

	// 記錄請求
	log.Printf("[%s] %s %s", req.Method, req.URL.Path, req.RemoteAddr)

	r.mux.ServeHTTP(w, req)

	// 記錄請求完成時間
	log.Printf("[%s] %s completed in %v", req.Method, req.URL.Path, time.Since(start))
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
		respondError(w, http.StatusBadRequest, "無効的歌曲 ID")
		return
	}

	result, err := r.songService.GetByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "歌曲不存在")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleGetSongPerformances(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効的歌曲 ID")
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
		respondError(w, http.StatusNotFound, "歌曲不存在")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleCreateSong(w http.ResponseWriter, req *http.Request) {
	var songReq dto.CreateSongRequest
	if err := json.NewDecoder(req.Body).Decode(&songReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効的請求格式")
		return
	}

	if songReq.Name == "" || songReq.OriginalArtist == "" {
		respondError(w, http.StatusBadRequest, "歌曲名稱和原唱藝人為必填")
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
		respondError(w, http.StatusBadRequest, "無効的歌曲 ID")
		return
	}

	var songReq dto.UpdateSongRequest
	if err := json.NewDecoder(req.Body).Decode(&songReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効的請求格式")
		return
	}

	if songReq.Name == "" || songReq.OriginalArtist == "" {
		respondError(w, http.StatusBadRequest, "歌曲名稱和原唱藝人為必填")
		return
	}

	result, err := r.songService.Update(id, &songReq)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "歌曲不存在")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// handleDeleteSong 刪除歌曲
func (r *Router) handleDeleteSong(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効的歌曲 ID")
		return
	}

	// 檢查歌曲是否存在
	song, err := r.songService.GetByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if song == nil {
		respondError(w, http.StatusNotFound, "歌曲不存在")
		return
	}

	// 刪除歌曲
	if err := r.songService.Delete(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "歌曲已刪除",
		"id":      id.String(),
	})
}

// handleMergeSong 將來源歌曲合併至目標歌曲
func (r *Router) handleMergeSong(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	sourceSongID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効的來源歌曲 ID")
		return
	}

	var mergeReq struct {
		TargetSongID string `json:"target_song_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&mergeReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効的請求格式")
		return
	}

	targetSongID, err := uuid.Parse(mergeReq.TargetSongID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効的目標歌曲 ID")
		return
	}

	// 確保來源和目標是不同的歌曲
	if sourceSongID == targetSongID {
		respondError(w, http.StatusBadRequest, "來源和目標歌曲不能相同")
		return
	}

	// 驗證兩首歌曲都存在
	sourceSong, err := r.songService.GetByID(sourceSongID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sourceSong == nil {
		respondError(w, http.StatusNotFound, "來源歌曲不存在")
		return
	}

	targetSong, err := r.songService.GetByID(targetSongID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if targetSong == nil {
		respondError(w, http.StatusNotFound, "目標歌曲不存在")
		return
	}

	// 執行合併
	if err := r.songService.MergeSongs(sourceSongID, targetSongID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":     "歌曲已合併",
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
		respondError(w, http.StatusBadRequest, "無効的歌回 ID")
		return
	}

	result, err := r.streamService.GetByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "歌回不存在")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleCreateStream(w http.ResponseWriter, req *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"message": "TODO: Create stream"})
}

func (r *Router) handleUpdateStream(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "無効的歌回 ID")
		return
	}

	var streamReq dto.UpdateStreamRequest
	if err := json.NewDecoder(req.Body).Decode(&streamReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効的請求格式")
		return
	}

	result, err := r.streamService.Update(id, &streamReq)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		respondError(w, http.StatusNotFound, "歌回不存在")
		return
	}

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
		respondError(w, http.StatusBadRequest, "無効的チャンネル ID")
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
		respondError(w, http.StatusBadRequest, "無効的チャンネル ID")
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
		respondError(w, http.StatusBadRequest, "無効的チャンネル ID")
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
		respondError(w, http.StatusBadRequest, "無効的請求格式")
		return
	}

	if singerReq.ID == "" || singerReq.Name == "" {
		respondError(w, http.StatusBadRequest, "チャンネルIDと名前は必須です")
		return
	}

	// 只同步頻道資訊，不同步直播
	if err := r.holodexService.SyncChannelInfo(singerReq.ID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 返回成功訊息
	respondJSON(w, http.StatusCreated, map[string]string{
		"message": "チャンネルを追加しました",
		"id":      singerReq.ID,
	})
}

// ========== Holodex Sync Handlers ==========

func (r *Router) handleSyncHolodex(w http.ResponseWriter, req *http.Request) {
	var syncReq dto.SyncHolodexRequest
	if err := json.NewDecoder(req.Body).Decode(&syncReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効的請求格式")
		return
	}

	if syncReq.ChannelID == "" {
		respondError(w, http.StatusBadRequest, "channel_id 為必填")
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

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleSyncHolodexVideo(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効的影片 ID")
		return
	}

	result, err := r.holodexService.SyncVideo(videoID)
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
		respondError(w, http.StatusBadRequest, "無効的影片 ID")
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
		respondError(w, http.StatusBadRequest, "無効的歌回 ID")
		return
	}

	var estimateReq dto.EstimateEndTimesRequest
	if err := json.NewDecoder(req.Body).Decode(&estimateReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効的請求格式")
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
		respondError(w, http.StatusBadRequest, "無効的歌回 ID")
		return
	}

	var createReq dto.CreatePerformancesRequest
	if err := json.NewDecoder(req.Body).Decode(&createReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効的請求格式")
		return
	}

	if len(createReq.Performances) == 0 {
		respondError(w, http.StatusBadRequest, "至少需要一首歌曲")
		return
	}

	result, err := r.performanceService.CreatePerformances(streamID, createReq.Performances)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (r *Router) handleDeletePerformances(w http.ResponseWriter, req *http.Request) {
	streamID := req.PathValue("id")
	if streamID == "" {
		respondError(w, http.StatusBadRequest, "無効的歌回 ID")
		return
	}

	if err := r.performanceService.DeleteByStreamID(streamID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, dto.SuccessResponse{
		Success: true,
		Message: "已刪除所有演出記錄",
	})
}

// ========== Comment Analysis Handlers ==========

func (r *Router) handleGetComments(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効的影片 ID")
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
		respondError(w, http.StatusBadRequest, "無効的影片 ID")
		return
	}

	result, err := r.commentService.AnalyzeComments(videoID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// ========== Batch AI Normalization Handler ==========

func (r *Router) handleBatchAINormalization(w http.ResponseWriter, req *http.Request) {
	var batchReq dto.BatchAINormalizationRequest
	if err := json.NewDecoder(req.Body).Decode(&batchReq); err != nil {
		respondError(w, http.StatusBadRequest, "無効的請求格式")
		return
	}

	if len(batchReq.Items) == 0 {
		respondError(w, http.StatusBadRequest, "至少需要一首歌曲")
		return
	}

	result, err := r.normalizationService.BatchAINormalization(batchReq.Items)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// ========== iTunes API Handlers ==========

func (r *Router) handleItunesSearch(w http.ResponseWriter, req *http.Request) {
	term := req.URL.Query().Get("term")

	if term == "" {
		respondError(w, http.StatusBadRequest, "検索キーワードが必要です")
		return
	}

	itunesClient := itunes.NewClient()
	result, err := itunesClient.Search(term)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
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

// ========== Helper Functions ==========

// respondJSON 回傳 JSON 回應
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError 回傳錯誤回應
func respondError(w http.ResponseWriter, status int, message string) {
	log.Printf("[ERROR] status=%d message=%s", status, message)
	respondJSON(w, status, dto.ErrorResponse{Error: message})
}
