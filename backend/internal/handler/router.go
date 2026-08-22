package handler

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	"github.com/ruifan75/setori/pkg/oauth"
	"github.com/ruifan75/setori/pkg/secrets"
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
	chapterService       *service.ChapterService
	artistService        *service.ArtistService
	batchAnalyzeService  *service.BatchAnalyzeService
	batchFillService     *service.BatchFillService
	authService          *service.AuthService
	// ログイン試行の絞り込み（総当たりと bcrypt による CPU 消費を止める）
	loginLimiter      *loginLimiter
	readingService    *service.ReadingService
	suggestionService *service.SuggestionService
	backupService     *service.BackupService
	playlistService   *service.PlaylistService
	presetService     *service.PresetService
	oauthService      *service.OAuthService
	settingsService   *service.SettingsService
	songMatchService  *service.SongMatchService
	aiService         *service.AIService
	orgService        *service.OrganizationService
	activityService   *service.ActivityService
	clientIPResolver  *clientIPResolver
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
	authRepo := repository.NewAuthRepository(db)
	artistRepo := repository.NewArtistRepository(db)
	songMatchRepo := repository.NewSongMatchRepository(db)
	aliasRepo := repository.NewAliasRepository(db)
	orgRepo := repository.NewOrganizationRepository(db)
	activityRepo := repository.NewActivityRepository(db)

	// AI サービス：複数 provider ローテーション + failover、未設定時は GROQ_API_KEY にフォールバック
	// 外部サービス連携の設定：DB（暗号化保存）→ .env の順に解決し、変更を各サービスへ即時反映する。
	// これにより .env を編集して再起動しなくても管理画面からキーを差し替えられる。
	settingsCipher, err := secrets.NewCipher(cfg.SettingsEncryptionKey)
	if err != nil {
		logger.Errorf("設定の暗号化を初期化できませんでした: %v", err)
		settingsCipher, _ = secrets.NewCipher("")
	}
	// AI プロバイダーのキーも同じ鍵で暗号化する（pkg/secrets の狙いは
	// 「バックアップ 1 つの流出で全キーが漏れない」こと。ここが平文だと成立しない）。
	aiProviderRepo := repository.NewAIProviderRepository(db, settingsCipher)
	if n, err := aiProviderRepo.EncryptPlaintextKeys(); err != nil {
		logger.Warnf("AI プロバイダーのキー暗号化に失敗しました: %v", err)
	} else if n > 0 {
		logger.Infof("AI プロバイダーのキー %d 件を暗号化しました", n)
	}

	aiService := service.NewAIService(aiProviderRepo, cfg.GroqAPIKey)

	// services を作成
	// 楽曲の同一性判定（曲名キー主導 + アーティストで検証）。照合を使う側は全員これを通す
	songMatchService := service.NewSongMatchService(songMatchRepo, songRepo, songItunesRepo, aliasRepo)
	songService := service.NewSongService(songRepo, perfRepo, songItunesRepo, artistRepo)
	songService.SetMatchService(songMatchService)
	artistService := service.NewArtistService(artistRepo, songRepo, aiService)
	streamService := service.NewStreamService(streamRepo, perfRepo)
	singerService := service.NewSingerService(singerRepo, streamRepo, perfRepo)
	orgService := service.NewOrganizationService(orgRepo)
	holodexService := service.NewHolodexService(cfg.HolodexAPIKey, cfg.YouTubeAPIKey, cfg.GroqAPIKey, streamRepo, singerRepo, cfg.HolodexEditorToken)
	holodexService.SetRepositoriesWithSongItunes(perfRepo, songRepo, songItunesRepo) // SyncSetoriToHolodex に必要な repositories を提供
	normalizationService := service.NewNormalizationService(aiService, songItunesRepo, songMatchService)
	// 照合は保存せず読み取り時に計算する。配信詳細を返すときにここを通す。
	chatEndService := service.NewChatEndService(streamRepo, cfg.YtdlpPath, "")
	// CommentService は分析時に正規化・拍手 end を内部で実行する（抽出→正規化→end→キャッシュ）
	commentService := service.NewCommentService(holodexService, streamRepo, filterKeywordRepo, aiService, normalizationService, chatEndService, songMatchService)
	// 3 つ目の入力元。抽出は CommentService と共有し、yt-dlp は ChatEndService のものを借りる
	chapterService := service.NewChapterService(streamRepo, commentService, normalizationService, chatEndService)
	// HolodexService も AnalyzeHolodexSongs で正規化・拍手 end を実行する（holodex_hash キャッシュ）
	holodexService.SetAnalysisServices(normalizationService, chatEndService)
	batchAnalyzeService := service.NewBatchAnalyzeService(commentService, streamRepo)
	performanceService := service.NewPerformanceService(perfRepo, songRepo, songItunesRepo, artistRepo, streamRepo, songMatchService)
	itunesClient := itunes.NewClient()
	endTimeEstimateService := service.NewEndTimeEstimateService(itunesClient)
	authService := service.NewAuthService(authRepo)
	activityService := service.NewActivityService(activityRepo, cfg.ActivityRetentionDays)
	ipResolver, err := newClientIPResolver(cfg.TrustedProxyCIDRs)
	if err != nil {
		logger.Errorf("信頼 proxy の設定を読み込めませんでした: %v", err)
		ipResolver, _ = newClientIPResolver("")
	}
	readingService := service.NewReadingService(artistRepo, songRepo)
	suggestionRepo := repository.NewSuggestionRepository(db)
	appSettingsRepo := repository.NewAppSettingsRepository(db)
	suggestionService := service.NewSuggestionService(suggestionRepo, appSettingsRepo, songService, artistService, performanceService, songMatchService)
	batchFillRepo := repository.NewBatchFillRepository(db)
	batchFillService := service.NewBatchFillService(streamRepo, perfRepo, batchFillRepo,
		commentService, holodexService, chapterService, normalizationService, performanceService, suggestionService)
	driveClient := gdrive.NewClient(cfg.GoogleOAuthClientID, cfg.GoogleOAuthSecret)
	backupService := service.NewBackupService(db, appSettingsRepo, driveClient, settingsCipher, cfg.DatabaseURL, cfg.BackupDir, cfg.BackupDockerContainer)
	if migrated, err := backupService.EncryptPlaintextDriveToken(); err != nil {
		logger.Warnf("Google Drive refresh token の暗号化移行に失敗しました: %v", err)
	} else if migrated {
		logger.Infof("Google Drive refresh token を暗号化しました")
	}
	playlistRepo := repository.NewPlaylistRepository(db, perfRepo)
	playlistService := service.NewPlaylistService(playlistRepo)
	presetService := service.NewPresetService(perfRepo, playlistRepo)
	oauthRepo := repository.NewOAuthRepository(db)
	// 連携先は Provider を足すだけで増やせる（X / Discord は実装を追加する）
	googleProvider := oauth.NewGoogleProvider(cfg.GoogleSigninClientID, cfg.GoogleSigninSecret)
	oauthService := service.NewOAuthService(authRepo, oauthRepo, cfg.OAuthRedirectBaseURL, googleProvider)

	settingsService := service.NewSettingsService(
		appSettingsRepo, settingsCipher,
		cfg.HolodexAPIKey, cfg.HolodexEditorToken, cfg.YouTubeAPIKey, cfg.GroqAPIKey,
		cfg.GoogleOAuthClientID, cfg.GoogleOAuthSecret,
		cfg.GoogleSigninClientID, cfg.GoogleSigninSecret,
		readCookieFile(cfg.YtdlpCookiesFile),
	)
	settingsService.OnChange(func(s service.IntegrationSettings) {
		holodexService.ApplyKeys(s.HolodexAPIKey, s.YouTubeAPIKey, s.HolodexEditorToken)
		aiService.SetFallbackKey(s.GroqAPIKey)
		driveClient.SetCredentials(s.GoogleDriveClientID, s.GoogleDriveSecret)
		googleProvider.SetCredentials(s.GoogleSigninClientID, s.GoogleSigninSecret)
		chatEndService.SetCookies(s.YtdlpCookies)
	})
	if err := settingsService.Load(); err != nil {
		logger.Errorf("連携設定の読み込みに失敗しました: %v", err)
	}

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
		chapterService:       chapterService,
		artistService:        artistService,
		batchAnalyzeService:  batchAnalyzeService,
		batchFillService:     batchFillService,
		authService:          authService,
		loginLimiter:         newLoginLimiter(),
		readingService:       readingService,
		suggestionService:    suggestionService,
		backupService:        backupService,
		playlistService:      playlistService,
		presetService:        presetService,
		oauthService:         oauthService,
		settingsService:      settingsService,
		songMatchService:     songMatchService,
		aiService:            aiService,
		orgService:           orgService,
		activityService:      activityService,
		clientIPResolver:     ipResolver,
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

// SongMatchService は main.go での照合キー再構築に使う。
func (r *Router) SongMatchService() *service.SongMatchService {
	return r.songMatchService
}

func (r *Router) setupRoutes() {
	// Health check
	r.mux.HandleFunc("GET /health", r.handleHealth)
	r.mux.HandleFunc("GET /api/version", r.handleVersion)

	// 認証
	r.mux.HandleFunc("POST /api/auth/login", r.handleLogin)
	r.mux.HandleFunc("POST /api/auth/logout", r.handleLogout)
	r.mux.HandleFunc("GET /api/auth/me", r.handleMe)

	// 外部アカウント連携（OAuth）
	r.mux.HandleFunc("GET /api/auth/oauth/providers", r.handleListOAuthProviders)
	r.mux.HandleFunc("GET /api/auth/oauth/identities", r.handleListMyIdentities)
	r.mux.HandleFunc("POST /api/auth/oauth/exchange", r.handleOAuthExchange)
	r.mux.HandleFunc("POST /api/auth/oauth/{provider}/start", r.handleOAuthStart)
	r.mux.HandleFunc("GET /api/auth/oauth/{provider}/callback", r.handleOAuthCallback)
	r.mux.HandleFunc("DELETE /api/auth/oauth/{provider}", r.handleUnlinkOAuth)

	// ユーザー・ロール・権限管理（要 users:manage）
	r.mux.HandleFunc("GET /api/users", r.handleListUsers)
	r.mux.HandleFunc("POST /api/users", r.handleCreateUser)
	r.mux.HandleFunc("PUT /api/users/{id}", r.handleUpdateUser)
	r.mux.HandleFunc("PUT /api/users/{id}/password", r.handleChangeUserPassword)
	r.mux.HandleFunc("DELETE /api/users/{id}", r.handleDeleteUser)
	r.mux.HandleFunc("POST /api/users/{id}/revoke-sessions", r.handleRevokeUserSessions)
	r.mux.HandleFunc("GET /api/roles", r.handleListRoles)
	r.mux.HandleFunc("POST /api/roles", r.handleCreateRole)
	r.mux.HandleFunc("PUT /api/roles/{id}", r.handleUpdateRole)
	r.mux.HandleFunc("DELETE /api/roles/{id}", r.handleDeleteRole)
	r.mux.HandleFunc("GET /api/permissions", r.handleListPermissions)

	// 訪客／利用者活動。記録だけは公開、閲覧は users:manage。
	r.mux.HandleFunc("POST /api/activity/visit", r.handleRecordVisit)
	r.mux.HandleFunc("GET /api/activity/policy", r.handleActivityPolicy)
	r.mux.HandleFunc("GET /api/activity", r.handleListActivity)
	r.mux.HandleFunc("GET /api/activity/stats", r.handleActivityStats)
	r.mux.HandleFunc("GET /api/activity/users", r.handleUserActivitySummaries)

	// 統一検索（楽曲・歌枠・チャンネル・YouTube URL/video ID）
	r.mux.HandleFunc("GET /api/search", r.handleGlobalSearch)
	// 複合条件の配信検索（キーワード × チャンネル × タグ AND）。
	// リテラルパターンのため /api/streams/{id} より優先マッチする。
	r.mux.HandleFunc("GET /api/streams/search", r.handleSearchStreams)

	// 未処理配信の一括プレ分析（背景ジョブ、singleton）
	r.mux.HandleFunc("POST /api/streams/batch-fill", r.handleStartBatchFill)
	r.mux.HandleFunc("POST /api/streams/batch-fill/cancel", r.handleCancelBatchFill)
	r.mux.HandleFunc("GET /api/streams/batch-fill/status", r.handleBatchFillStatus)
	r.mux.HandleFunc("GET /api/streams/batch-fill/runs", r.handleListBatchFillRuns)
	r.mux.HandleFunc("POST /api/streams/batch-fill/runs/{id}/revert", r.handleRevertBatchFill)
	r.mux.HandleFunc("GET /api/streams/batch-fill/runs/{id}/gaps", r.handleListBatchFillGaps)
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
	// 照合が外れて新曲になったものの統合候補（黙って重複が増えるのを防ぐ受け皿）
	r.mux.HandleFunc("GET /api/songs/merge-candidates", r.handleListMergeCandidates)
	r.mux.HandleFunc("POST /api/songs/merge-candidates/{id}/dismiss", r.handleDismissMergeCandidate)
	r.mux.HandleFunc("POST /api/songs/merge-candidates/scan", r.handleScanDuplicates)
	r.mux.HandleFunc("POST /api/songs/merge-candidates/adjudicate", r.handleAdjudicateDuplicates)
	r.mux.HandleFunc("GET /api/songs/{id}/merge-candidates", r.handleGetSongMergeCandidates)
	// 「この表記はこの曲ではない」という否決の見直し（見えないと誤判定を直せない）
	r.mux.HandleFunc("GET /api/songs/identity-checks", r.handleListIdentityChecks)
	r.mux.HandleFunc("POST /api/songs/identity-checks/delete", r.handleDeleteIdentityCheck)

	// API routes - 照合の学習層（アーティストの別名義・楽曲の別表記）

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
	r.mux.HandleFunc("GET /api/readings/stats", r.handleReadingsStats)
	r.mux.HandleFunc("GET /api/readings/export", r.handleExportReadings)
	r.mux.HandleFunc("POST /api/readings/import", r.handleImportReadings)

	// 修正提案：投稿は閲覧モードでも可、一覧/承認/却下は content:edit
	r.mux.HandleFunc("POST /api/suggestions", r.handleCreateSuggestion)
	r.mux.HandleFunc("GET /api/suggestions", r.handleListSuggestions)
	r.mux.HandleFunc("GET /api/suggestions/mine", r.handleListMySuggestions)
	r.mux.HandleFunc("GET /api/suggestions/count", r.handleCountSuggestions)
	r.mux.HandleFunc("POST /api/suggestions/batch", r.handleBatchReviewSuggestions)
	r.mux.HandleFunc("POST /api/suggestions/merge", r.handleMergeSuggestions)
	r.mux.HandleFunc("GET /api/suggestions/settings", r.handleGetSuggestionSettings)
	r.mux.HandleFunc("PUT /api/suggestions/settings", r.handleUpdateSuggestionSettings)
	r.mux.HandleFunc("POST /api/suggestions/{id}/approve", r.handleApproveSuggestion)
	r.mux.HandleFunc("POST /api/suggestions/{id}/reject", r.handleRejectSuggestion)
	r.mux.HandleFunc("DELETE /api/suggestions/{id}", r.handleWithdrawSuggestion)
	r.mux.HandleFunc("POST /api/suggestions/{id}/undo-rejection", r.handleUndoRejection)

	// 外部サービス連携の設定（キーの値は返さない。要 users:manage）
	r.mux.HandleFunc("GET /api/settings/integrations", r.handleGetIntegrationSettings)
	r.mux.HandleFunc("PUT /api/settings/integrations", r.handleUpdateIntegrationSettings)

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

	// プリセットプレイリスト（運営が用意した歌単）。
	// /api/playlists/preset/... にすると /api/playlists/{id} と食い合うので前置きを分ける
	// （限定公開 URL を /api/shared/playlists へ分けたのと同じ理由）。
	r.mux.HandleFunc("GET /api/presets", r.handleListPresets)
	r.mux.HandleFunc("GET /api/presets/followed", r.handleListFollowedPresets)
	r.mux.HandleFunc("GET /api/presets/{key}", r.handleGetPreset)
	r.mux.HandleFunc("GET /api/presets/{key}/items", r.handleListPresetItems)
	r.mux.HandleFunc("POST /api/presets/{key}/follow", r.handleFollowPreset)
	r.mux.HandleFunc("DELETE /api/presets/{key}/follow", r.handleUnfollowPreset)
	r.mux.HandleFunc("POST /api/presets/{key}/add", r.handleAddPresetToPlaylist)

	// API routes - Singers
	r.mux.HandleFunc("GET /api/singers", r.handleListSingers)
	r.mux.HandleFunc("GET /api/singers/search", r.handleSearchSingers)
	r.mux.HandleFunc("GET /api/singers/{id}", r.handleGetSinger)
	r.mux.HandleFunc("GET /api/singers/{id}/streams", r.handleGetSingerStreams)
	r.mux.HandleFunc("GET /api/singers/{id}/performances", r.handleGetSingerPerformances)
	r.mux.HandleFunc("POST /api/singers", r.handleCreateSinger)
	r.mux.HandleFunc("PUT /api/singers/{id}", r.handleUpdateSinger)
	r.mux.HandleFunc("PUT /api/singers/{id}/visibility", r.handleUpdateSingerVisibility)
	r.mux.HandleFunc("PUT /api/singers/{id}/organization", r.handleUpdateSingerOrganization)

	// 事務所（取り込み時の key と表示名を分けて持つ）
	r.mux.HandleFunc("GET /api/organizations", r.handleListOrganizations)
	r.mux.HandleFunc("POST /api/organizations", r.handleCreateOrganization)
	r.mux.HandleFunc("PUT /api/organizations/{key}", r.handleUpdateOrganization)
	r.mux.HandleFunc("DELETE /api/organizations/{key}", r.handleDeleteOrganization)

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
	r.mux.HandleFunc("POST /api/artists/aliases", r.handleProposeArtistAlias)
	r.mux.HandleFunc("POST /api/streams/{id}/performances", r.handleCreatePerformances)
	r.mux.HandleFunc("DELETE /api/streams/{id}/performances", r.handleDeletePerformances)

	// 歌唱記録の単件操作（セットリスト全体を送り直さずに1件だけ直す）
	r.mux.HandleFunc("GET /api/performances/{id}", r.handleGetPerformance)
	r.mux.HandleFunc("PUT /api/performances/{id}", r.handleUpdatePerformance)

	// Comment analysis
	r.mux.HandleFunc("GET /api/streams/{id}/comments", r.handleGetComments)
	r.mux.HandleFunc("POST /api/streams/{id}/comments/sync-youtube", r.handleSyncYouTubeComments)
	r.mux.HandleFunc("POST /api/streams/{id}/comments/analyze", r.handleAnalyzeComments)
	r.mux.HandleFunc("POST /api/comments/backfill", r.handleBackfillCommentSongs)
	r.mux.HandleFunc("POST /api/comments/backfill-hashes", r.handleBackfillCommentSongsHashes)
	r.mux.HandleFunc("POST /api/streams/{id}/analyze-chat-ends", r.handleAnalyzeChatEnds)
	r.mux.HandleFunc("POST /api/streams/{id}/chat-end-estimate", r.handleEstimateChatEnds)
	r.mux.HandleFunc("POST /api/chat-ends/backfill", r.handleBackfillChatEnds)

	// チャプター分析（配信者が付けた目次を 3 つ目の入力元にする）
	r.mux.HandleFunc("GET /api/streams/{id}/chapters", r.handleGetChapters)
	r.mux.HandleFunc("POST /api/streams/{id}/chapters/sync", r.handleSyncChapters)
	r.mux.HandleFunc("POST /api/streams/{id}/chapters/analyze", r.handleAnalyzeChapters)
	r.mux.HandleFunc("POST /api/chapters/backfill", r.handleBackfillChapters)

	// Filter keywords management
	r.mux.HandleFunc("GET /api/filter-keywords", r.handleListFilterKeywords)
	r.mux.HandleFunc("POST /api/filter-keywords", r.handleCreateFilterKeyword)
	r.mux.HandleFunc("DELETE /api/filter-keywords/{id}", r.handleDeleteFilterKeyword)

	// タグ検索（タグが付いた配信・演出の一覧）
	r.mux.HandleFunc("GET /api/stream-tags/{id}/streams", r.handleGetStreamsByTag)
	r.mux.HandleFunc("GET /api/performance-tags/{id}/performances", r.handleGetPerformancesByTag)

	// ホーム：おすすめ
	r.mux.HandleFunc("GET /api/performances/random", r.handleRandomPerformances)

	// Tag management
	r.mux.HandleFunc("GET /api/stream-tags", r.handleListStreamTags)
	r.mux.HandleFunc("POST /api/stream-tags", r.handleCreateStreamTag)
	r.mux.HandleFunc("DELETE /api/stream-tags/{id}", r.handleDeleteStreamTag)
	r.mux.HandleFunc("GET /api/performance-tags", r.handleListPerformanceTags)
	r.mux.HandleFunc("POST /api/performance-tags", r.handleCreatePerformanceTag)
	r.mux.HandleFunc("DELETE /api/performance-tags/{id}", r.handleDeletePerformanceTag)

	// タグ漏れ：解析キャッシュがタグを付けているのに歌唱に無い組のレビュー（content:edit）
	r.mux.HandleFunc("GET /api/tag-gaps", r.handleListTagGaps)
	r.mux.HandleFunc("POST /api/tag-gaps/dismiss", r.handleDismissTagGap)
	r.mux.HandleFunc("POST /api/tag-gaps/undismiss", r.handleUndismissTagGap)

	// タイトル自動タグ付けルール（stream tag をタイトルの文字列一致で付与）
	r.mux.HandleFunc("GET /api/tag-keyword-rules", r.handleListTagKeywordRules)
	r.mux.HandleFunc("POST /api/tag-keyword-rules", r.handleCreateTagKeywordRule)
	r.mux.HandleFunc("DELETE /api/tag-keyword-rules/{id}", r.handleDeleteTagKeywordRule)
	r.mux.HandleFunc("POST /api/tag-rules/backfill", r.handleBackfillTagRules)

	// AI normalization (for direct editing flow)
	r.mux.HandleFunc("POST /api/ai/normalize", r.handleBatchAINormalization)

	// AI プロバイダー設定（管理者）
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
	r.mux.HandleFunc("POST /api/backups/{name}/upload-drive", r.handleUploadBackupToDrive)
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

// handleStartBatchFill は範囲を指定してセットリストを自動で埋める（content:edit）。
//
// 一括プレ分析（batch-analyze）と混同しないこと。あちらは抽出だけで主データを触らない。
// こちらは performances に直接書くので、実行記録を残し、撤回できるようにしてある。
func (r *Router) handleStartBatchFill(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Mode string `json:"mode"` // unprocessed / force
		// SingerIDs は対象チャンネル（空なら全部）。既定はそのチャンネルが**所有する**配信で、
		// IncludeCollabs を立てるとゲスト参加した配信も含む。
		SingerIDs      []string `json:"singer_ids"`
		IncludeCollabs bool     `json:"include_collabs"`
		// SingerID は 1 チャンネルだけ指定していた頃の互換。
		SingerID string `json:"singer_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}
	if body.SingerID != "" {
		body.SingerIDs = append(body.SingerIDs, body.SingerID)
	}
	var startedBy *uuid.UUID
	if u := currentUser(req); u != nil {
		id := u.ID
		startedBy = &id
	}
	runID, err := r.batchFillService.Start(body.Mode, body.SingerIDs, body.IncludeCollabs, startedBy)
	if err != nil {
		if errors.Is(err, service.ErrBatchFillAlreadyRunning) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"run_id": runID, "message": "一括作成を開始しました"})
}

func (r *Router) handleCancelBatchFill(w http.ResponseWriter, req *http.Request) {
	r.batchFillService.Cancel()
	respondJSON(w, http.StatusOK, map[string]string{"message": "停止を要求しました"})
}

func (r *Router) handleBatchFillStatus(w http.ResponseWriter, req *http.Request) {
	respondJSON(w, http.StatusOK, r.batchFillService.Status())
}

func (r *Router) handleListBatchFillRuns(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	runs, err := r.batchFillService.ListRuns(limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 空でも null ではなく [] を返す。Go の nil スライスは JSON の null になり、
	// 受け手が配列として扱った瞬間に落ちる（実際に画面が真っ白になった）。
	if runs == nil {
		runs = []repository.BatchFillRun{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// handleRevertBatchFill はその実行が作った歌唱をまとめて消す（content:edit）。
func (r *Router) handleRevertBatchFill(w http.ResponseWriter, req *http.Request) {
	runID, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な実行 ID")
		return
	}
	n, err := r.batchFillService.RevertRun(runID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"deleted": n,
		"message": fmt.Sprintf("%d件の歌唱を撤回しました", n),
	})
}

// handleListBatchFillGaps はその実行で「DB にあるが入力元に無い」と分かった歌唱を返す（content:edit）。
//
// これらは提案として積んでいない（入力元は欠けているのが普通で、欠落 1 件ごとに待ち行列を
// 作ると処理できない量になるため）。実行履歴からここへ辿るのが唯一の入口。
func (r *Router) handleListBatchFillGaps(w http.ResponseWriter, req *http.Request) {
	runID, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な実行 ID")
		return
	}
	gaps, err := r.batchFillService.ListGaps(runID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"gaps": gaps})
}

// handleStartBatchAnalyze 未処理配信の一括プレ分析を開始する（content:edit）。
// mode / singer_id は JSON body で受け取る（後方互換で query param の mode もフォールバック）。
// maxBatchRequestBytes は一括処理の起動リクエストの上限。
// 中身は mode / singer_id / hidden の 3 つだけなので、これで十分足りる。
const maxBatchRequestBytes = 64 << 10 // 64 KiB

// parseBatchAnalyzeRequest は一括分析の起動リクエストを読む。
//
// 空ボディは許容（既定モードにフォールバック）。それ以外は**厳しく検証する** ──
// ここは数百本を AI にかける背景ジョブの起動口で、hidden が対象集合を 700 本近く変える。
// 惜しい間違いを黙って既定へ倒すと、意図と違う対象で走り出したうえ 202 が返り、
// 呼んだ側は成功したと思い込む。
//
// json.Decoder ではなく Unmarshal を使うのは、後続の JSON 値
// （{"hidden":"true"}{"hidden":"false"} のような body）を Decoder が先頭だけ読んで
// 通してしまうため。Unmarshal は top-level 値の後ろにゴミがあれば弾く。
func parseBatchAnalyzeRequest(r io.Reader) (dto.BatchAnalyzeRequest, error) {
	var body dto.BatchAnalyzeRequest

	// 上限は「切り捨て」ではなく「拒否」。LimitReader で黙って切ると、正しい object が
	// ちょうど上限で終わってその後ろに別の JSON 値がある body を先頭だけで受理してしまう。
	raw, err := io.ReadAll(io.LimitReader(r, maxBatchRequestBytes+1))
	if err != nil {
		return body, errors.New("リクエストを読めませんでした")
	}
	if len(raw) > maxBatchRequestBytes {
		return body, errors.New("リクエストが大きすぎます")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return body, nil // 空ボディは既定へ
	}

	// **まず生の形で検証してから型へ落とす。**
	//
	// 型へ直接落とすと、惜しい間違いが黙って既定になる経路がいくつも残る：
	//   - encoding/java の JSON キー照合は**大文字小文字を無視する**ので、
	//     {"Hidden": null} は Hidden フィールドに一致しつつ null で値が変わらない
	//   - body 全体が null でも Unmarshal は成功する
	//   - キーの打ち間違い（"hiddden"）は未知フィールドとして捨てられる
	//   - mode / singer_id の null もそれぞれ既定・全チャンネルへ落ちる
	//
	// ここは部分更新の DTO と違い「既定へ倒さないこと」自体が安全要件なので、
	// top-level が object であること・キーが完全一致であること・値が null でないことを
	// 生の map で確かめる。
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return body, fmt.Errorf("無効なリクエスト形式: %w", err)
	}
	if fields == nil {
		return body, errors.New("リクエスト本体が null です")
	}
	allowed := map[string]bool{"mode": true, "singer_id": true, "hidden": true}
	for k, v := range fields {
		if !allowed[k] {
			return body, fmt.Errorf("知らない項目です: %q（mode / singer_id / hidden のみ）", k)
		}
		if string(bytes.TrimSpace(v)) == "null" {
			return body, fmt.Errorf("%s に null は指定できません", k)
		}
	}

	if err := json.Unmarshal(raw, &body); err != nil {
		return body, fmt.Errorf("無効なリクエスト形式: %w", err)
	}
	return body, nil
}

func (r *Router) handleStartBatchAnalyze(w http.ResponseWriter, req *http.Request) {
	body, err := parseBatchAnalyzeRequest(req.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	mode := body.Mode
	if mode == "" {
		mode = req.URL.Query().Get("mode")
	}
	if mode == "" {
		mode = service.BatchModeUnprocessed
	}
	// 非表示の扱いはクエリでも body でも受ける（他の一括操作の呼び方と揃える）
	hiddenParam := body.Hidden
	if hiddenParam == "" {
		hiddenParam = req.URL.Query().Get("hidden")
	}
	hidden, err := service.ParseHiddenFilter(hiddenParam)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := r.batchAnalyzeService.Start(mode, body.SingerID, hidden); err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	logger.Infof("batch analyze started: mode=%s singer=%q hidden=%s", mode, body.SingerID, hiddenParam)
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

// handleReadingsStats は読みの整備状況（未整備の残件数）を返す。
func (r *Router) handleReadingsStats(w http.ResponseWriter, req *http.Request) {
	stats, err := r.readingService.Stats()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, stats)
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

// handleProposeArtistAlias は「この 2 つは同じ人」を登録する（要ログイン）。
//
// 権限があればその場で反映し、無ければ修正提案として積む
// （再生バーの時間修正と同じ扱い。詳細は docs/SETLIST_FLOW.md）。
//
// 別名義は**その人の全楽曲に効く**ので、読み込んだだけでは書かない。
// 編集フォームで人がチェックを入れて保存したときにだけここへ来る。
func (r *Router) handleProposeArtistAlias(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Canonical string `json:"canonical"` // DB 側の表記（別名義の本体）
		Alias     string `json:"alias"`     // コメント側の表記
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	body.Canonical, body.Alias = strings.TrimSpace(body.Canonical), strings.TrimSpace(body.Alias)
	if body.Canonical == "" || body.Alias == "" {
		respondError(w, http.StatusBadRequest, "2 つの名前が必要です")
		return
	}

	if userHasPermission(req, auth.PermContentEdit) {
		if err := r.songMatchService.AddArtistAlias(body.Canonical, body.Alias); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"applied": true})
		return
	}

	// 権限が無い場合は提案にする。対象は artists の行なので、行が無ければ提案できない
	// （曲が 1 つも無い名義は artists に行を持たない）。
	artist, err := r.artistService.FindByName(body.Canonical)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if artist == nil {
		respondError(w, http.StatusNotFound, "このアーティストはまだ登録されていないため提案できません")
		return
	}
	fields := map[string]string{"aliases": joinAliases(artist.Aliases, body.Alias)}
	sug, err := r.suggestionService.Create(&dto.CreateSuggestionRequest{
		TargetType: "artist",
		TargetID:   artist.ID.String(),
		Fields:     fields,
		Note:       fmt.Sprintf("%s は %s の別名義", body.Alias, body.Canonical),
	}, service.SuggestionActor{User: currentUser(req), ClientHint: r.clientIPResolver.clientHint(req)})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"applied": false, "suggestion_id": sug.ID})
}

// joinAliases は既存の別名義に 1 つ足した文字列を作る（重複は足さない）。
func joinAliases(existing []string, add string) string {
	for _, e := range existing {
		if e == add {
			return strings.Join(existing, "、")
		}
	}
	return strings.Join(append(append([]string{}, existing...), add), "、")
}

// ========== Suggestion Handlers（修正提案） ==========

// handleCreateSuggestion は修正提案を投稿する（閲覧モードでも可・匿名可）。
func (r *Router) handleCreateSuggestion(w http.ResponseWriter, req *http.Request) {
	var body dto.CreateSuggestionRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	// 未登録曲の追加・曲の差し替えは fields ではなく payload / song_swap で内容を渡す
	if body.Kind != service.KindMissingSong && body.Kind != service.KindSongSwap && len(body.Fields) == 0 {
		respondError(w, http.StatusBadRequest, "提案する変更がありません")
		return
	}

	actor := service.SuggestionActor{
		User:       currentUser(req),
		ClientHint: r.clientIPResolver.clientHint(req),
	}
	sug, err := r.suggestionService.Create(&body, actor)
	if err != nil {
		var invalidInput *service.ValidationError
		switch {
		case errors.As(err, &invalidInput):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrInvalidTarget), errors.Is(err, service.ErrNoChange),
			errors.Is(err, service.ErrInvalidTimeRange):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrTargetNotFound):
			respondError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrTooManySuggestions):
			respondError(w, http.StatusTooManyRequests, err.Error())
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

// handleListSuggestions は提案一覧を返す（content:edit）。
// ?status=pending|conflict|approved|rejected で絞る。
// ?group=target で対象ごとにまとめた形（ページングの単位も対象）で返す。
// ?kind=perf.missing で種別を絞る（一括が積んだ審査待ちだけを見たいときに使う）。
func (r *Router) handleListSuggestions(w http.ResponseWriter, req *http.Request) {
	status := req.URL.Query().Get("status")
	kind := req.URL.Query().Get("kind")
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	if req.URL.Query().Get("group") == "target" {
		grouped, err := r.suggestionService.ListGrouped(status, kind, page, limit)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, grouped)
		return
	}

	result, err := r.suggestionService.List(status, kind, page, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleListMySuggestions は自分が出した提案を返す（要ログイン・権限不要）。
// ?status= で絞る。取り下げと結果の確認のための画面用。
func (r *Router) handleListMySuggestions(w http.ResponseWriter, req *http.Request) {
	status := req.URL.Query().Get("status")
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))

	result, err := r.suggestionService.ListMine(currentUser(req), status, page, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, result)
}

// handleBatchReviewSuggestions は複数の提案をまとめて承認/却下する（content:edit）。
// 一部が失敗しても残りは処理し、結果を個別に返す。
func (r *Router) handleBatchReviewSuggestions(w http.ResponseWriter, req *http.Request) {
	var body dto.BatchReviewRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	if body.Action != "approve" && body.Action != "reject" {
		respondError(w, http.StatusBadRequest, "action は approve か reject を指定してください")
		return
	}
	if len(body.IDs) == 0 {
		respondError(w, http.StatusBadRequest, "対象の提案がありません")
		return
	}
	if len(body.IDs) > 200 {
		respondError(w, http.StatusBadRequest, "一度に処理できるのは200件までです")
		return
	}

	ids := make([]uuid.UUID, 0, len(body.IDs))
	for _, raw := range body.IDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "無効な提案ID: "+raw)
			return
		}
		ids = append(ids, id)
	}

	result := r.suggestionService.BatchReview(ids, body.Action, currentUser(req), body.Force, body.Note)
	respondJSON(w, http.StatusOK, result)
}

// handleGetSuggestionSettings は timing 提案の自動適用条件を返す（content:edit）。
func (r *Router) handleGetSuggestionSettings(w http.ResponseWriter, req *http.Request) {
	respondJSON(w, http.StatusOK, r.suggestionService.GetAutoApplySettings())
}

// handleUpdateSuggestionSettings は自動適用条件を更新する（content:edit）。
// 値はサービス層で安全な範囲に丸められる。
func (r *Router) handleUpdateSuggestionSettings(w http.ResponseWriter, req *http.Request) {
	var body service.AutoApplySettings
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}
	saved, err := r.suggestionService.UpdateAutoApplySettings(body)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, saved)
}

// handleWithdrawSuggestion は自分が出した未処理の提案を取り下げる（要ログイン）。
// content:edit を持つ管理者は他人の分も引ける。他人のものは存在を伏せて 404。
func (r *Router) handleWithdrawSuggestion(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な提案ID")
		return
	}
	if err := r.suggestionService.Withdraw(id, currentUser(req)); err != nil {
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
	respondJSON(w, http.StatusOK, map[string]string{"message": "提案を取り下げました"})
}

// handleMergeSuggestions は同一対象の提案を管理者が決めた値へ統合して反映する（content:edit）。
// 「どれか1つを丸ごと採用」では表せない決着（中央値・誰も出していない値）のための操作。
func (r *Router) handleMergeSuggestions(w http.ResponseWriter, req *http.Request) {
	var body dto.MergeSuggestionsRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	result, err := r.suggestionService.Merge(&body, currentUser(req))
	if err != nil {
		var invalidInput *service.ValidationError
		switch {
		case errors.As(err, &invalidInput):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrInvalidTarget):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrTargetNotFound):
			respondError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrDuplicatePerformance):
			respondError(w, http.StatusConflict, err.Error())
		default:
			respondError(w, http.StatusBadRequest, err.Error())
		}
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
// ?force=1 で、提案後に対象が変更されていても現在値を上書きして承認する。
func (r *Router) handleApproveSuggestion(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な提案ID")
		return
	}
	force := req.URL.Query().Get("force") == "1"
	// ボディは任意。審査の画面は「そこで直した内容」を添えて承認する
	// （曲の差し替え・時間の微調整・歌手の選択を、承認と 1 往復で済ませるため）。
	var body dto.ApproveSuggestionRequest
	_ = json.NewDecoder(req.Body).Decode(&body)

	if err := r.suggestionService.ApproveWithEdits(id, currentUser(req), force, body.Payload); err != nil {
		var conflict *service.ConflictError
		switch {
		case errors.As(err, &conflict):
			// 提案後に対象が変わっている。どのフィールドがどうズレたかを返し、
			// 管理者が「上書きしてよいか」を判断できるようにする。
			respondJSON(w, http.StatusConflict, map[string]any{
				"error":     conflict.Error(),
				"conflicts": conflict.Fields,
			})
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
	// 却下理由（任意）。ボディが無くても却下できる。
	// not_this_song を立てると「この表記はこの曲ではない」として学習する。
	var body dto.RejectSuggestionRequest
	_ = json.NewDecoder(req.Body).Decode(&body)

	if err := r.suggestionService.RejectWithVerdict(id, currentUser(req), body.Note, body.NotThisSong); err != nil {
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

// handleUndoRejection は却下を取り消し、次の一括実行でまた提案されるようにする（content:edit）。
//
// 却下した提案は status に関係なく重複判定に引っかかるため、そのままだと永久に出てこない。
// 「この曲ではない」を押していれば song_identity_checks にも残っているので、それも消す。
func (r *Router) handleUndoRejection(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な提案ID")
		return
	}
	if err := r.suggestionService.UndoRejection(id, currentUser(req)); err != nil {
		var invalid *service.ValidationError
		switch {
		case errors.Is(err, service.ErrSuggestionNotFound):
			respondError(w, http.StatusNotFound, err.Error())
		case errors.As(err, &invalid):
			respondError(w, http.StatusConflict, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{
		"message": "却下を取り消しました。次回の一括作成でまた提案されます",
	})
}

// handleListIdentityChecks は「この表記はこの曲ではない」という否決の一覧を返す（content:edit）。
//
// 否決は候補からその曲を外し続け、AI にも聞き直さない。効き続けるものが画面から
// 見えないと、誤判定が混ざっていても気付けないまま照合が歪む。
func (r *Router) handleListIdentityChecks(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	rows, err := r.songMatchService.ListSongRejections(limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"checks": rows})
}

// handleDeleteIdentityCheck は否決を 1 件取り消す（content:edit）。
//
// pair_key は区切りに制御文字を含むので URL パスには載せず body で受ける。
func (r *Router) handleDeleteIdentityCheck(w http.ResponseWriter, req *http.Request) {
	var body struct {
		PairKey string `json:"pair_key"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.PairKey == "" {
		respondError(w, http.StatusBadRequest, "pair_key が必要です")
		return
	}
	if err := r.songMatchService.DeleteSongRejection(body.PairKey); err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{
		"message": "判定を取り消しました。次回からまた候補に出ます",
	})
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

// handleDeleteSong は楽曲を削除する。
func (r *Router) handleDeleteSong(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な曲ID")
		return
	}

	// 楽曲が存在するか確認する
	song, err := r.songService.GetByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if song == nil {
		respondError(w, http.StatusNotFound, "曲が見つかりません")
		return
	}

	// 楽曲を削除する
	if err := r.songService.Delete(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "曲を削除しました",
		"id":      id.String(),
	})
}

// handleMergeSong は統合元楽曲を統合先楽曲へまとめる。
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

	// 統合元と統合先が別の楽曲であることを確認する
	if sourceSongID == targetSongID {
		respondError(w, http.StatusBadRequest, "元の曲と対象の曲は同じにできません")
		return
	}

	// 両方の楽曲が存在することを検証する
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

	// 統合を実行する
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

	// 解析結果（各タイムライン）は編集画面だけが読む中間生成物なので、
	// 編集権限を持つ利用者にだけ載せる。
	result, err := r.streamService.GetByID(id, userHasPermission(req, auth.PermContentEdit))
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
	// 配信は Holodex 同期（POST /api/sync/holodex/video/{id}）でのみ作成できる。
	// 手動作成の入口はまだないため、呼び出し側が作成成功と誤認しないよう 501 を返す。
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

	// 非表示チャンネルは既定で一覧から外す。include_hidden=true を出せるのは
	// content:edit を持つレビュー担当だけ（無い場合は黙って無視する）。
	includeHidden := req.URL.Query().Get("include_hidden") == "true" &&
		userHasPermission(req, auth.PermContentEdit)

	// group=organization は事務所別（ページングなし）。
	if req.URL.Query().Get("group") == "organization" {
		grouped, err := r.singerService.GetGrouped(includeHidden)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, grouped)
		return
	}

	result, err := r.singerService.GetAll(page, limit, sort, dir, includeHidden)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// handleUpdateSingerVisibility はチャンネルの非表示を切り替える（content:edit）。
// 非表示にしてもチャンネルページ自体は誰でも開ける。隠すのは一覧に載る場所だけ。
func (r *Router) handleUpdateSingerVisibility(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "チャンネルIDは必須です")
		return
	}

	var body dto.UpdateSingerVisibilityRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	found, err := r.singerService.SetHidden(id, body.IsHidden)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		respondError(w, http.StatusNotFound, "チャンネルが見つかりません")
		return
	}

	logger.Infof("singer %s visibility updated (is_hidden=%v)", id, body.IsHidden)
	respondJSON(w, http.StatusOK, map[string]any{"id": id, "is_hidden": body.IsHidden})
}

// handleUpdateSingerOrganization は Holodex の分類を手動で上書きする（content:edit）。
// 空文字で上書きを解除し、Holodex の値に戻る。Holodex 管理チャンネルでも設定できる
// （これは Holodex のメタデータではなく seTORI 側の判断のため）。
func (r *Router) handleUpdateSingerOrganization(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "チャンネルIDは必須です")
		return
	}

	var body dto.UpdateSingerOrganizationRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	found, err := r.singerService.SetOrganizationOverride(id, body.Organization)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		respondError(w, http.StatusNotFound, "チャンネルが見つかりません")
		return
	}

	logger.Infof("singer %s organization override set to %q", id, body.Organization)
	respondJSON(w, http.StatusOK, map[string]any{"id": id, "organization": body.Organization})
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

	// 絞り込みパラメータを解析する
	var processedFilter, hiddenFilter *bool

	// processed: "all" (nil), "true", "false"
	if processedStr := req.URL.Query().Get("processed"); processedStr != "" && processedStr != "all" {
		processed := processedStr == "true"
		processedFilter = &processed
	}

	// hidden: "all", "true"（非表示のみ）, "false"（非表示を除外、既定）
	hiddenStr := req.URL.Query().Get("hidden")
	if hiddenStr == "" {
		// 既定では非表示を除外する
		hidden := false
		hiddenFilter = &hidden
	} else if hiddenStr == "true" {
		hidden := true
		hiddenFilter = &hidden
	} else if hiddenStr == "false" {
		hidden := false
		hiddenFilter = &hidden
	}
	// hiddenStr == "all" の場合、hiddenFilter は nil のままにする

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

	// チャンネル情報だけを同期し、配信は同期しない
	singer, err := r.holodexService.SyncChannelInfo(singerReq.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 成功メッセージを返す
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

// seTORI のデータを Holodex へ同期する。
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

// handleGetPerformance は歌唱1件を配信・楽曲情報付きで返す（閲覧は公開）。
func (r *Router) handleGetPerformance(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な歌唱ID")
		return
	}
	perf, err := r.performanceService.GetByID(id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if perf == nil {
		respondError(w, http.StatusNotFound, "歌唱記録が見つかりません")
		return
	}
	respondJSON(w, http.StatusOK, perf)
}

// handleUpdatePerformance は歌唱1件を部分更新する（content:edit）。
// セットリスト全体を送り直す POST /api/streams/{id}/performances と違い、他の曲を巻き込まない。
func (r *Router) handleUpdatePerformance(w http.ResponseWriter, req *http.Request) {
	id, err := uuid.Parse(req.PathValue("id"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "無効な歌唱ID")
		return
	}
	var body dto.UpdatePerformanceRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "無効なリクエスト形式")
		return
	}

	updated, err := r.performanceService.UpdatePerformance(id, &body)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPerformanceNotFound):
			respondError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrDuplicatePerformance):
			respondError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrInvalidTimeRange):
			respondError(w, http.StatusBadRequest, err.Error())
		default:
			respondError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	respondJSON(w, http.StatusOK, updated)
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

	// ?force=true でキャッシュを無視して再分析（再正規化）する
	force := req.URL.Query().Get("force") == "true"

	// ?dry_run=true は解析だけして何も書かない（計測用の読み取り専用の口）。
	// 保存だけでなく、別名義の学習・コメントの取り直しの保存も止まる。
	// 結果は応答の stats で確かめる。
	var (
		result *dto.AnalyzeCommentsResponse
		err    error
	)
	if req.URL.Query().Get("dry_run") == "true" {
		result, err = r.commentService.AnalyzeCommentsDryRun(videoID)
	} else {
		result, err = r.commentService.AnalyzeComments(videoID, force)
	}
	if err != nil {
		// 分析中にコメントが差し替わっただけなので、やり直せば通る。
		// 500 だと利用者にはサーバーの故障に見え、再実行すべきだと分からない。
		if errors.Is(err, service.ErrCommentRawChanged) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
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

// handleAnalyzeChatEnds は手動実行で、live chat の拍手から歌枠内の各曲の end を検出する。
//
// AI を一切呼ばず、既存の comment_songs の start だけを入力に end を取り直す。
// 一括プレ分析はキャッシュ命中だと拍手 end まで飛ばすので、
// 「AI 分析は済んでいるが end だけ入っていない」配信を後から埋めるのがこの経路。
//
// 同期で応答する：画面から押すボタンなので、結果を見せずに 202 を返しても
// 何が起きたか分からない。live chat のダウンロードは初回で数十秒〜数分かかるが、
// 2回目以降はローカルキャッシュに当たるので速い。
func (r *Router) handleAnalyzeChatEnds(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	res, err := r.chatEndService.AnalyzeStream(videoID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"id":      videoID,
		"total":   res.Total,
		"filled":  res.Filled,
		"changed": res.Changed,
	})
}

// handleBackfillChatEnds は comment_songs を持つすべての歌枠で拍手 end 検出を補完する（バックグラウンド、同時実行数制限あり）。
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

// ========== Chapter Handlers ==========

// handleGetChapters は保存済みのチャプターを返す（未取得なら yt-dlp で取りに行く）。
func (r *Router) handleGetChapters(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	chapters, err := r.chapterService.GetChapters(videoID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"video_id": videoID, "chapters": chapters})
}

// handleSyncChapters は yt-dlp でチャプターを取り直す（配信者が後から目次を足した場合用）。
func (r *Router) handleSyncChapters(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	chapters, err := r.chapterService.RefreshChapters(videoID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"video_id": videoID, "chapter_count": len(chapters), "chapters": chapters})
}

// handleAnalyzeChapters はチャプターから楽曲を抽出して返す（?force=true でキャッシュ無視）。
func (r *Router) handleAnalyzeChapters(w http.ResponseWriter, req *http.Request) {
	videoID := req.PathValue("id")
	if videoID == "" {
		respondError(w, http.StatusBadRequest, "無効な動画ID")
		return
	}
	result, err := r.chapterService.AnalyzeChapters(videoID, req.URL.Query().Get("force") == "true")
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logger.Infof("/chapters/analyze completed for %s: %d songs", videoID, len(result.Songs))
	respondJSON(w, http.StatusOK, result)
}

// handleBackfillChapters はチャプターを未取得の配信をまとめて取りに行く
// （バックグラウンド、同時実行数制限あり）。一括セットリスト作成の前に流しておくためのもの。
func (r *Router) handleBackfillChapters(w http.ResponseWriter, req *http.Request) {
	concurrency := 3
	if c := req.URL.Query().Get("concurrency"); c != "" {
		if v, err := strconv.Atoi(c); err == nil && v > 0 {
			concurrency = v
		}
	}
	go r.chapterService.Backfill(concurrency)
	respondJSON(w, http.StatusAccepted, map[string]interface{}{
		"message":     "チャプターの取得を開始しました（バックグラウンド、ログ参照）",
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

// handleBackfillCommentSongs は comment_raw があり comment_songs がない配信をすべて補完する。
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
// （旧: 生bytes sha → 新: 正規化 sha）。AI は呼ばず、キャッシュが効かなくなっていた歌枠を修復する。
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

// ========== タグ漏れ（解析キャッシュ vs 歌唱） ==========

// handleListTagGaps は「解析キャッシュにタグがあるのに歌唱に無い」組と、
// 「付けない」と判断して無視した組を返す（content:edit）。
//
// 差分は毎回計算する（保存しない）。付ければ次から消え、意図的に付けないものは
// 無視して消す ── どちらも次の計算に効くので、一覧は放っておくと減る作りにしてある。
func (r *Router) handleListTagGaps(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	gaps, err := r.tagRepo.FindTagGaps(limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dismissed, err := r.tagRepo.ListTagGapDismissals(limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"gaps": gaps, "dismissed": dismissed})
}

// tagGapKeyFromBody は {performance_id, tag_id} を読む（dismiss / undismiss で共用）。
func tagGapKeyFromBody(req *http.Request) (uuid.UUID, string, error) {
	var body struct {
		PerformanceID string `json:"performance_id"`
		TagID         string `json:"tag_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return uuid.Nil, "", fmt.Errorf("リクエストの形式が不正です")
	}
	id, err := uuid.Parse(strings.TrimSpace(body.PerformanceID))
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("performance_id が不正です")
	}
	tagID := strings.TrimSpace(body.TagID)
	if tagID == "" {
		return uuid.Nil, "", fmt.Errorf("tag_id が必要です")
	}
	return id, tagID, nil
}

// handleDismissTagGap は「この歌唱にこのタグは付けない」を記録する。
func (r *Router) handleDismissTagGap(w http.ResponseWriter, req *http.Request) {
	perfID, tagID, err := tagGapKeyFromBody(req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	var by *uuid.UUID
	if u := currentUser(req); u != nil {
		id := u.ID
		by = &id
	}
	if err := r.tagRepo.DismissTagGap(perfID, tagID, by); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "このタグは付けないものとして記録しました"})
}

// handleUndismissTagGap は無視を取り消す（次の一覧からまた出る）。
func (r *Router) handleUndismissTagGap(w http.ResponseWriter, req *http.Request) {
	perfID, tagID, err := tagGapKeyFromBody(req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := r.tagRepo.UndismissTagGap(perfID, tagID); err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "無視を取り消しました"})
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

	// 1. iTunes で検索する
	itunesClient := itunes.NewClient()
	itunesResult, err := itunesClient.Search(term)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 2. 各 iTunes ID が DB に存在するか確認する
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

		enhanced.ExistingSong = existingSongBrief(songRepo, itunesItem.ItunesID)
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

	respondJSON(w, http.StatusOK, dto.ItunesQueryResultWithSong{
		ItunesID:        result.ItunesID,
		CollectionName:  result.CollectionName,
		TrackName:       result.TrackName,
		ArtistName:      result.ArtistName,
		ArtworkURL:      result.ArtworkURL,
		TrackViewURL:    result.TrackViewURL,
		TrackTimeMillis: result.TrackTimeMillis,
		PreviewURL:      result.PreviewURL,
		Country:         result.Country,
		ExistingSong:    existingSongBrief(repository.NewSongRepository(r.db), result.ItunesID),
	})
}

// existingSongBrief は iTunes ID に紐づく既存楽曲を返す。無ければ nil。
// **検索と ID 直引きで同じものを通す** ── 片方だけが「もう DB にある」を
// 知っている状態になると、同じ曲を二重に作る入口ができる。
func existingSongBrief(songRepo *repository.SongRepository, itunesID int64) *dto.SongBrief {
	song, err := songRepo.FindByItunesID(itunesID)
	if err != nil {
		logger.Warnf("Error checking iTunes ID %d: %v", itunesID, err)
		return nil
	}
	if song == nil {
		return nil
	}
	perfCount, err := songRepo.GetPerformanceCount(song.ID)
	if err != nil {
		logger.Warnf("Error counting performances for song %s: %v", song.ID, err)
		perfCount = 0
	}
	brief := &dto.SongBrief{
		ID:               song.ID,
		Name:             song.Name,
		OriginalArtist:   song.OriginalArtist,
		PerformanceCount: perfCount,
	}
	if song.NameReading.Valid {
		brief.NameReading = &song.NameReading.String
	}
	if song.OriginalArtistReading.Valid {
		brief.OriginalArtistReading = &song.OriginalArtistReading.String
	}
	if song.Arts.Valid {
		brief.Arts = &song.Arts.String
	}
	return brief
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
	// API key は値が指定され、空でない場合だけ更新する（空欄なら元の値を保持）
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

// resolveUser は Bearer トークンから現在のユーザーを解決する。未ログインなら (nil, nil)。
func (r *Router) resolveUser(req *http.Request) (*models.User, error) {
	token := bearerToken(req)
	if token == "" {
		return nil, nil
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
	case "/health", "/api/version", "/api/auth/login", "/api/activity/visit", "/api/activity/policy":
		return "", false
	case "/api/auth/logout", "/api/auth/me":
		return "", true
	}

	// 一括セットリスト作成・一括プレ分析は進捗・履歴も含めて content:edit。
	// 実行の履歴には誰が回したかが載るので、閲覧者には出さない。
	//
	// batch-analyze も同じ扱いにする理由：status の current には処理中の配信タイトルが、
	// failed_ids には配信 ID が入り、完了後も残る。非表示の配信を対象に回すと、
	// 未ログインのポーリングで「一覧に出していない配信」の題名と ID が読めてしまう。
	//
	// 判定は「完全一致か、その下のパス」で行う。単純な prefix にすると、将来
	// /api/streams/batch-analyze-report のような別ルートを足したときに黙って
	// 巻き込む（認可は ServeMux より前に path 文字列だけで決まるため）。
	if isRouteOrSubpath(path, "/api/streams/batch-fill") ||
		isRouteOrSubpath(path, "/api/streams/batch-analyze") {
		return auth.PermContentEdit, true
	}

	// 解析の素材を返す GET は編集者の道具であって、閲覧者向けの経路ではない。
	// **どれもキャッシュが無ければ外部へ取りに行く**：
	//
	//	/comments       … Holodex / YouTube API（運用者の API キー）
	//	/chapters       … yt-dlp（管理→設定に登録した cookies 付き＝運用者のメンバー資格）
	//	/holodex-songs  … Holodex API。そもそもキャッシュを見ずに毎回叩く
	//
	// 未ログインから叩けると、匿名のリクエストに運用者の資格情報と API 枠を使わせることになる。
	// 実測では、未ログインの GET /api/streams/{id}/chapters がメン限配信に対して
	// yt-dlp を起動し（3.7 秒）、結果を保存して返していた。
	//
	// フロントエンドからの利用も編集の文脈に限られるので、閲覧者への影響は無い：
	// /comments は配信編集画面（isEditing）と提案レビュー画面の RawCommentsPanel から呼ばれ、
	// どちらの導線も content:edit を要求する。/chapters と /holodex-songs は呼び出し自体が無い。
	//
	// **語尾ではなくルートの形で判定する。** 語尾だけで見ると
	// /api/streams/search/chapters のような別の形も拾い、逆に将来
	// /api/streams/{id}/comments/raw のようなサブリソースを足すと素通りする。
	if isStreamSubresource(path, "comments", "chapters", "holodex-songs") {
		return auth.PermContentEdit, true
	}

	// 別名義の登録：ログインだけ必要。権限があればその場で反映し、無ければ提案になる
	// （判定はハンドラ側。提案の投稿と同じ扱いにしている）。
	if path == "/api/artists/aliases" && method == http.MethodPost {
		return "", true
	}

	// 修正提案：投稿（POST /api/suggestions）はログインのみ必要（編集権限は不要）。
	// 匿名でも投稿できるようにしていたが、再生中のワンタップ通報を載せた結果、
	// 誰の指摘かを追えないと信頼度の重み付けも濫用への対処もできないため要ログインにした。
	// 取り下げ（DELETE）も同様：自分の提案を引っ込めるのに編集権限は要らない。
	// 誰の提案を引けるかという行単位の判定は SuggestionService が行う。
	// 一覧・件数・承認・却下・統合は content:edit（管理者レビュー）。
	if path == "/api/suggestions" && method == http.MethodPost {
		return "", true
	}
	// 自分の提案の一覧は本人のものしか返らないので、編集権限は要らない。
	if path == "/api/suggestions/mine" {
		return "", true
	}
	if strings.HasPrefix(path, "/api/suggestions") {
		if method == http.MethodDelete {
			return "", true
		}
		return auth.PermContentEdit, true
	}

	// 限定公開 URL（共有リンク）は未ログインでも閲覧可
	if strings.HasPrefix(path, "/api/shared/playlists") {
		return "", false
	}

	// 外部アカウント連携：ログイン導線なので未ログインで通す必要がある。
	// start はログイン中なら「連携追加」として扱うため、どちらでも通す（判定はハンドラ側）。
	// 連携一覧の閲覧と解除だけはログインが要る。
	if strings.HasPrefix(path, "/api/auth/oauth") {
		switch {
		case path == "/api/auth/oauth/identities":
			return "", true
		case method == http.MethodDelete:
			return "", true // 連携解除
		default:
			return "", false // providers / start / callback / exchange
		}
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

	// プリセットプレイリストは運営が用意した公開の歌単。中身の閲覧は誰でもできる。
	// フォローとコピーは自分のデータを作るだけなので、ログインだけを求める
	// （プレイリストと同じ考え方。フォロー中の一覧も本人の分しか返らない）。
	if strings.HasPrefix(path, "/api/presets") {
		if method == http.MethodGet && path != "/api/presets/followed" {
			return "", false
		}
		return "", true
	}

	// 照合の学習層は全楽曲の照合結果を左右する。AI の判定も含まれるので、
	// 閲覧も編集もレビュー担当（content:edit）に限る。
	if strings.HasPrefix(path, "/api/aliases") {
		return auth.PermContentEdit, true
	}

	// 統合候補はレビュー用の作業一覧なので、修正提案のレビューと同じ権限に揃える。
	// 楽曲詳細に出す個別の候補（/api/songs/{id}/merge-candidates）は閲覧のまま。
	if path == "/api/songs/merge-candidates" {
		return auth.PermContentEdit, true
	}

	// タグ漏れもレビュー用の作業一覧。閲覧も編集と同じ権限に揃える。
	if strings.HasPrefix(path, "/api/tag-gaps") {
		return auth.PermContentEdit, true
	}

	// 管理系リソースはメソッドを問わず専用権限が必要
	switch {
	case strings.HasPrefix(path, "/api/users"),
		strings.HasPrefix(path, "/api/roles"),
		path == "/api/permissions",
		strings.HasPrefix(path, "/api/activity"):
		return auth.PermUsersManage, true
	case strings.HasPrefix(path, "/api/settings"):
		// 連携設定は実質的に資格情報の管理なのでユーザー管理と同格の権限を要求する
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

// isRouteOrSubpath は path が route そのものか、その下のパスかを返す。
// strings.HasPrefix をそのまま使うと "/api/streams/batch-fill-x" のような
// 別ルートまで一致してしまうため、境界を "/" で区切って見る。
func isRouteOrSubpath(path, route string) bool {
	return path == route || strings.HasPrefix(path, route+"/")
}

// isStreamSubresource は path が /api/streams/{id}/{name}（および**その配下**）かを返す。
//
// 配下まで含めるのは、あとから /api/streams/{id}/comments/raw のようなサブリソースを
// 足したときに、語尾一致から外れて公開既定へ静かに落ちるのを防ぐため。
// 保護を継承するほうが、足した人が気付かないまま素通りするより安全側に倒れる。
// 末尾スラッシュも同じ理由で配下として扱う。
//
// 逆に /api/streams/{name} のように配信 ID の位置に来る形は対象外
// （それは配信詳細であって、解析素材の経路ではない）。
func isStreamSubresource(path string, names ...string) bool {
	const prefix = "/api/streams/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) < 2 || parts[0] == "" {
		return false
	}
	for _, n := range names {
		if parts[1] == n {
			return true
		}
	}
	return false
}

// ========== Helper Functions ==========

// readCookieFile は YTDLP_COOKIES_FILE で指定された cookies.txt を読む。
// 中身を設定サービスの env フォールバックとして渡すため、ここで一度だけ読む
// （＝ファイルを差し替えたら再起動が要る。管理画面から入れれば即時反映される）。
func readCookieFile(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		logger.Warnf("YTDLP_COOKIES_FILE を読めません (%s): %v", path, err)
		return ""
	}
	logger.Infof("YTDLP_COOKIES_FILE を読み込みました: %s", path)
	return string(b)
}

// respondJSON は JSON レスポンスを返す。
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError はエラーレスポンスを返す。
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
