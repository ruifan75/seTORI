package dto

import (
	"time"

	"github.com/google/uuid"
)

// ========== ページング ==========

type PaginationRequest struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

type PaginationResponse struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// ========== 楽曲 ==========

type SongItunesResponse struct {
	ItunesID       int64   `json:"itunes_id"`
	CollectionName *string `json:"collection_name,omitempty"`
	Country        *string `json:"country,omitempty"`
	IsPrimary      bool    `json:"is_primary"`
}

type SongResponse struct {
	ID                    uuid.UUID            `json:"id"`
	Name                  string               `json:"name"`
	NameReading           *string              `json:"name_reading,omitempty"`
	OriginalArtist        string               `json:"original_artist"`
	OriginalArtistReading *string              `json:"original_artist_reading,omitempty"`
	Arts                  *string              `json:"arts,omitempty"`
	PerformanceCount      int                  `json:"performance_count"`
	ItunesIDs             []SongItunesResponse `json:"itunes_ids,omitempty"`
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
}

type SongListResponse struct {
	Songs      []SongResponse     `json:"songs"`
	Pagination PaginationResponse `json:"pagination"`
}

type CreateSongRequest struct {
	Name                  string       `json:"name"`
	NameReading           *string      `json:"name_reading,omitempty"`
	OriginalArtist        string       `json:"original_artist"`
	OriginalArtistReading *string      `json:"original_artist_reading,omitempty"`
	Arts                  *string      `json:"arts,omitempty"`
	ItunesIds             []ItunesItem `json:"itunes_ids,omitempty"`
}

type ItunesItem struct {
	ItunesID  int64 `json:"itunes_id"`
	IsPrimary bool  `json:"is_primary"`
}

type UpdateSongRequest struct {
	Name                  string       `json:"name"`
	NameReading           *string      `json:"name_reading,omitempty"`
	OriginalArtist        string       `json:"original_artist"`
	OriginalArtistReading *string      `json:"original_artist_reading,omitempty"`
	Arts                  *string      `json:"arts,omitempty"`
	ItunesIds             []ItunesItem `json:"itunes_ids,omitempty"`
}

// ========== 歌手 ==========

type SingerResponse struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	EnglishName     *string   `json:"english_name,omitempty"`
	PhotoURL        *string   `json:"photo_url,omitempty"`
	Organization    *string   `json:"organization,omitempty"`
	MetadataSource  string    `json:"metadata_source"`
	CanEditMetadata bool      `json:"can_edit_metadata"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SingerListResponse struct {
	Singers    []SingerResponse   `json:"singers"`
	Pagination PaginationResponse `json:"pagination"`
}

type SingerDetailResponse struct {
	SingerResponse
	StreamCount      int `json:"stream_count"`
	PerformanceCount int `json:"performance_count"`
}

type SingerPerformanceListResponse struct {
	Singer       SingerResponse            `json:"singer"`
	Performances []SongPerformanceResponse `json:"performances"`
	Pagination   PaginationResponse        `json:"pagination"`
}

type CreateSingerRequest struct {
	ID           string  `json:"id"` // YouTube Channel ID / @handle / URL
	Name         string  `json:"name,omitempty"`
	EnglishName  *string `json:"english_name,omitempty"`
	PhotoURL     *string `json:"photo_url,omitempty"`
	Organization *string `json:"organization,omitempty"`
}

type CreateSingerResponse struct {
	Message string `json:"message"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

type UpdateSingerRequest struct {
	Name         string  `json:"name"`
	EnglishName  *string `json:"english_name,omitempty"`
	PhotoURL     *string `json:"photo_url,omitempty"`
	Organization *string `json:"organization,omitempty"`
}

// ========== 歌枠 ==========

type StreamTagResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Color       string `json:"color"`
}

type StreamResponse struct {
	ID                   string              `json:"id"`
	Title                string              `json:"title"`
	StreamDate           string              `json:"stream_date"`
	DurationSeconds      *int32              `json:"duration_seconds,omitempty"`
	ThumbnailURL         *string             `json:"thumbnail_url,omitempty"`
	Tags                 []StreamTagResponse `json:"tags"`
	Participants         []SingerResponse    `json:"participants"`
	ChannelOwner         *SingerResponse     `json:"channel_owner,omitempty"` // 頻道擁有者
	IsProcessed          bool                `json:"is_processed"`
	IsHidden             bool                `json:"is_hidden"`
	HolodexTimelineSongs []SongSuggestion    `json:"holodex_timeline_songs,omitempty"` // 從 holodex_data 解析
	CommentTimelineSongs []CommentSong       `json:"comment_timeline_songs,omitempty"` // 從 comment_songs 解析（已分析快取）
	HasCommentRaw        bool                `json:"has_comment_raw"`                  // comment_raw 是否有留言可供分析
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
}

type StreamListResponse struct {
	Streams    []StreamResponse   `json:"streams"`
	Pagination PaginationResponse `json:"pagination"`
}

type StreamDetailResponse struct {
	StreamResponse
	Performances []PerformanceResponse `json:"performances"`
}

type UpdateStreamRequest struct {
	Title          *string  `json:"title,omitempty"`
	StreamDate     *string  `json:"stream_date,omitempty"`
	TagIDs         []string `json:"tag_ids,omitempty"`
	ParticipantIDs []string `json:"participant_ids,omitempty"`
	IsProcessed    *bool    `json:"is_processed,omitempty"`
	IsHidden       *bool    `json:"is_hidden,omitempty"`
}

// ========== 演出 ==========

type PerformanceTagResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Color       string `json:"color"`
}

type PerformanceResponse struct {
	ID             uuid.UUID                `json:"id"`
	StreamID       string                   `json:"stream_id"`
	SongID         uuid.UUID                `json:"song_id"`
	SongName       string                   `json:"song_name"`
	OriginalArtist string                   `json:"original_artist"`
	Arts           *string                  `json:"arts,omitempty"`
	StartSeconds   int                      `json:"start_seconds"`
	EndSeconds     int                      `json:"end_seconds"`
	OrderIndex     int                      `json:"order_index"`
	Tags           []PerformanceTagResponse `json:"tags"`
	CustomTags     []string                 `json:"custom_tags"`
	Singers        []SingerResponse         `json:"singers"`
	YouTubeURL     string                   `json:"youtube_url"`
	CreatedAt      time.Time                `json:"created_at"`
	// タグ検索など配信横断の一覧でのみ設定される（配信詳細では省略）
	StreamTitle  string  `json:"stream_title,omitempty"`
	StreamDate   string  `json:"stream_date,omitempty"`
	ThumbnailURL *string `json:"thumbnail_url,omitempty"`
}

// 用於歌曲詳情頁的反向查詢
type SongPerformanceResponse struct {
	ID           uuid.UUID                `json:"id"`
	StreamID     string                   `json:"stream_id"`
	StreamTitle  string                   `json:"stream_title"`
	StreamDate   string                   `json:"stream_date"`
	ThumbnailURL *string                  `json:"thumbnail_url,omitempty"`
	SongID       uuid.UUID                `json:"song_id,omitempty"`
	SongName     string                   `json:"song_name,omitempty"`
	StartSeconds int                      `json:"start_seconds"`
	EndSeconds   int                      `json:"end_seconds"`
	Tags         []PerformanceTagResponse `json:"tags"`
	CustomTags   []string                 `json:"custom_tags"`
	Singers      []SingerResponse         `json:"singers"`
	YouTubeURL   string                   `json:"youtube_url"`
	CreatedAt    time.Time                `json:"created_at"`
}

type SongPerformanceListResponse struct {
	Song         SongResponse              `json:"song"`
	Performances []SongPerformanceResponse `json:"performances"`
	Pagination   PaginationResponse        `json:"pagination"`
}

// ========== Holodex 同步 ==========

type SyncHolodexRequest struct {
	ChannelID   string `json:"channel_id"`
	Limit       int    `json:"limit,omitempty"`        // 同步多少筆（預設 50）
	ForceUpdate bool   `json:"force_update,omitempty"` // 強制更新所有資料
}

type SyncHolodexResponse struct {
	SyncedCount  int      `json:"synced_count"`
	TotalStreams int      `json:"total_streams"` // 總共要處理的 stream 數量
	Processed    int      `json:"processed"`     // 已處理的數量
	NewStreams   []string `json:"new_streams"`
	Updated      []string `json:"updated"`
	Skipped      []string `json:"skipped"`
	InProgress   bool     `json:"in_progress"`       // 是否還在進行中
	Message      string   `json:"message,omitempty"` // 狀態訊息
}

// ========== Comment 分析 ==========

type CommentSong struct {
	Start              int    `json:"start"`
	End                int    `json:"end"`
	Name               string `json:"name"`            // 抽出した歌名（逐字。正規化前）
	OriginalArtist     string `json:"original_artist"` // 抽出した歌手（逐字。正規化前）
	OriginalComment    string `json:"original_comment"`
	IsEndTimeEstimated bool   `json:"is_end_time_estimated"`

	// Chat 拍手偵測相關（用來跟 comment explicit end 比較）
	ChatEnd int `json:"chat_end,omitempty"` // chat 偵測出的結束秒數（如果有）
	EndDiff int `json:"end_diff,omitempty"` // |end - chat_end|，只有兩邊都有值時才填

	// ↓ 折り込んだ正規化結果（分析時に AI 正規化＋DB 照合まで実行し、ここに保存・再利用する）
	NormalizedName          string   `json:"normalized_name,omitempty"`
	NormalizedNameReading   string   `json:"normalized_name_reading,omitempty"`
	NormalizedArtist        string   `json:"normalized_artist,omitempty"`
	NormalizedArtistReading string   `json:"normalized_artist_reading,omitempty"`
	Tags                    []string `json:"tags,omitempty"`
	Confidence              float64  `json:"confidence,omitempty"`
	// DB 照合（マッチした既存曲）
	MatchedSongID            *string `json:"matched_song_id,omitempty"`
	MatchedSongName          *string `json:"matched_song_name,omitempty"`
	MatchedSongNameReading   *string `json:"matched_song_name_reading,omitempty"`
	MatchedSongArtist        *string `json:"matched_song_artist,omitempty"`
	MatchedSongArtistReading *string `json:"matched_song_artist_reading,omitempty"`
	MatchedSongArtURL        *string `json:"matched_song_art_url,omitempty"`
	MatchedSongItunesID      *int64  `json:"matched_song_itunes_id,omitempty"`
}

type AnalyzeCommentsResponse struct {
	Songs       []CommentSong `json:"songs"`
	RawComments []string      `json:"raw_comments"`
	// AI 正規化が失敗（全プロバイダー冷却等）し、抽出のみで返した場合に設定される。
	// バッチ分析はこれを見て冷却待ち後に force 再試行する。
	Warning string `json:"warning,omitempty"`
}

// BatchAnalyzeStatus 未処理配信の一括分析ジョブの進捗
type BatchAnalyzeStatus struct {
	Running   bool     `json:"running"`
	Total     int      `json:"total"`
	Done      int      `json:"done"`
	Failed    int      `json:"failed"`
	Current   string   `json:"current,omitempty"`    // 処理中の配信タイトル
	FailedIDs []string `json:"failed_ids,omitempty"` // AI 失敗が解消しなかった配信
	Message   string   `json:"message,omitempty"`
}

// ========== 直接建立演出 ==========

// 從 Holodex 或 Comment 讀取後的歌曲建議
type SongSuggestion struct {
	Name           string   `json:"name"`
	OriginalArtist string   `json:"original_artist"`
	StartSeconds   int      `json:"start_seconds"`
	EndSeconds     int      `json:"end_seconds"`
	Tags           []string `json:"tags"`       // 正規化で検出した版本タグ
	SingerIDs      []string `json:"singer_ids"` // 預設為頻道擁有者
	ArtURL         *string  `json:"art_url,omitempty"`
	ItunesID       *int64   `json:"itunes_id,omitempty"` // Holodex 提供的 iTunes ID

	// ↓ 折り込んだ正規化結果（AnalyzeHolodexSongs 時に AI 正規化＋DB 照合し埋め込む。CommentSong と対称）
	NormalizedName           string  `json:"normalized_name,omitempty"`
	NormalizedNameReading    string  `json:"normalized_name_reading,omitempty"`
	NormalizedArtist         string  `json:"normalized_artist,omitempty"`
	NormalizedArtistReading  string  `json:"normalized_artist_reading,omitempty"`
	Confidence               float64 `json:"confidence,omitempty"`
	MatchedSongID            *string `json:"matched_song_id,omitempty"`
	MatchedSongName          *string `json:"matched_song_name,omitempty"`
	MatchedSongNameReading   *string `json:"matched_song_name_reading,omitempty"`
	MatchedSongArtist        *string `json:"matched_song_artist,omitempty"`
	MatchedSongArtistReading *string `json:"matched_song_artist_reading,omitempty"`
	MatchedSongArtURL        *string `json:"matched_song_art_url,omitempty"`
	MatchedSongItunesID      *int64  `json:"matched_song_itunes_id,omitempty"`
}

// 載入 Holodex 歌曲的回應
type LoadHolodexSongsResponse struct {
	StreamID     string           `json:"stream_id"`
	StreamTitle  string           `json:"stream_title"`
	ChannelOwner SingerResponse   `json:"channel_owner"` // 頻道擁有者
	Participants []SingerResponse `json:"participants"`  // 所有參與者（包含頻道擁有者）
	Songs        []SongSuggestion `json:"songs"`
}

// 建立演出請求
type CreatePerformanceItem struct {
	Name                  string   `json:"name"`
	NameReading           string   `json:"name_reading,omitempty"`
	OriginalArtist        string   `json:"original_artist"`
	OriginalArtistReading string   `json:"original_artist_reading,omitempty"`
	StartSeconds          int      `json:"start_seconds"`
	EndSeconds            int      `json:"end_seconds"`
	Tags                  []string `json:"tags"`
	SingerIDs             []string `json:"singer_ids"`
	ArtURL                *string  `json:"art_url,omitempty"`
	ItunesID              *int64   `json:"itunes_id,omitempty"` // Holodex 提供的 iTunes ID
	CustomTags            []string `json:"custom_tags,omitempty"`
}

type CreatePerformancesRequest struct {
	Performances []CreatePerformanceItem `json:"performances"`
}

type CreatePerformancesResponse struct {
	CreatedCount int `json:"created_count"`
}

// ========== AI 正規化 ==========

// AI 正規化用的輸入項目
type AINormalizationItem struct {
	Name           string  `json:"name"`
	OriginalArtist string  `json:"original_artist"`
	ArtURL         *string `json:"art_url,omitempty"`
	ItunesID       *int64  `json:"itunes_id,omitempty"`
}

// 批量 AI 正規化請求
type BatchAINormalizationRequest struct {
	Items []AINormalizationItem `json:"items"`
}

// AI 建議結果
type AISuggestionResult struct {
	Index                 int      `json:"index"` // 對應原本的索引
	NormalizedName        string   `json:"normalized_name"`
	NormalizedNameReading string   `json:"normalized_name_reading"` // 歌名平假名讀音
	OriginalArtist        string   `json:"original_artist"`
	OriginalArtistReading string   `json:"original_artist_reading"` // 藝人名平假名讀音
	Tags                  []string `json:"tags"`
	Confidence            float64  `json:"confidence"`
	Reasoning             string   `json:"reasoning"`
	MatchedSongID         *string  `json:"matched_song_id,omitempty"` // 如果匹配到現有歌曲
	MatchReason           string   `json:"match_reason,omitempty"`    // 匹配原因（name, itunes_id）
	// DB 歌曲資訊（匹配到時回傳）
	MatchedSongName          *string `json:"matched_song_name,omitempty"`
	MatchedSongNameReading   *string `json:"matched_song_name_reading,omitempty"`
	MatchedSongArtist        *string `json:"matched_song_artist,omitempty"`
	MatchedSongArtistReading *string `json:"matched_song_artist_reading,omitempty"`
	MatchedSongArtURL        *string `json:"matched_song_art_url,omitempty"`
	MatchedSongItunesID      *int64  `json:"matched_song_itunes_id,omitempty"`
}

// 批量 AI 正規化回應
type BatchAINormalizationResponse struct {
	Suggestions []AISuggestionResult `json:"suggestions"`
	Warning     string               `json:"warning,omitempty"`
}

// ========== 認證 ==========

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  struct {
		ID          uuid.UUID `json:"id"`
		Username    string    `json:"username"`
		DisplayName string    `json:"display_name"`
		Role        string    `json:"role"`
	} `json:"user"`
}

// ========== アーティスト ==========

type ArtistResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	NameReading *string   `json:"name_reading,omitempty"`
	SongCount   int       `json:"song_count"`
}

type ArtistListResponse struct {
	Artists    []ArtistResponse   `json:"artists"`
	Pagination PaginationResponse `json:"pagination"`
}

type ArtistDetailResponse struct {
	Artist     ArtistResponse     `json:"artist"`
	Songs      []SongResponse     `json:"songs"`
	Pagination PaginationResponse `json:"pagination"`
}

type UpdateArtistRequest struct {
	Name        *string `json:"name,omitempty"`         // 変更時のみ。所属楽曲の表示テキストも連動更新
	NameReading *string `json:"name_reading,omitempty"` // 変更時のみ
}

type MergeArtistRequest struct {
	TargetArtistID string `json:"target_artist_id"`
}

// BackfillReadingsResponse 読み仮名 AI 補完の結果
type BackfillReadingsResponse struct {
	ArtistsUpdated int    `json:"artists_updated"`
	SongsUpdated   int    `json:"songs_updated"`
	Warning        string `json:"warning,omitempty"`
}

// ========== グローバル検索 ==========

// SearchStreamItem 検索結果の配信（軽量版）
type SearchStreamItem struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	StreamDate   time.Time `json:"stream_date"`
	ThumbnailURL *string   `json:"thumbnail_url,omitempty"`
	IsProcessed  bool      `json:"is_processed"`
	IsHidden     bool      `json:"is_hidden"`
}

// SearchTagItem 検索結果のタグ（使用件数付き）
type SearchTagItem struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Color       string `json:"color"`
	Count       int    `json:"count"`
}

// GlobalSearchResponse 統一検索の結果。
// 入力が YouTube URL / video ID の場合は video_id を返し、テキスト検索はスキップする。
type GlobalSearchResponse struct {
	Query           string             `json:"query"`
	VideoID         string             `json:"video_id,omitempty"`
	VideoRegistered bool               `json:"video_registered"`
	Songs           []SongResponse     `json:"songs"`
	Streams         []SearchStreamItem `json:"streams"`
	Singers         []SingerResponse   `json:"singers"`
	Artists         []ArtistResponse   `json:"artists"`
	StreamTags      []SearchTagItem    `json:"stream_tags"`
	PerformanceTags []SearchTagItem    `json:"performance_tags"`
}

// TagPerformanceListResponse タグ検索（演出）のページング付き結果
type TagPerformanceListResponse struct {
	Performances []PerformanceResponse `json:"performances"`
	Pagination   PaginationResponse    `json:"pagination"`
}

// ========== 通用回應 ==========

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

type SuccessResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// ========== 推算結束時間 ==========

type SongEndTimeEstimateRequest struct {
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	ItunesID  int64  `json:"itunes_id,omitempty"`
	NextStart int    `json:"next_start,omitempty"`
	StreamEnd int    `json:"stream_end,omitempty"`
}

type SongEndTimeEstimate struct {
	EstimatedEnd       int    `json:"estimated_end"`
	IsEndTimeEstimated bool   `json:"is_end_time_estimated"`
	Method             string `json:"method"` // "from_comment", "from_next_song", "from_itunes", "from_default"
	OriginalItunesDur  int    `json:"original_itunes_dur,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type EstimateEndTimesRequest struct {
	Songs       []SongEndTimeEstimateRequest `json:"songs"`
	StreamEnd   int                          `json:"stream_end"`
	StreamTitle string                       `json:"stream_title,omitempty"`
}

type EstimateEndTimesResponse struct {
	Estimates []SongEndTimeEstimate `json:"estimates"`
	Message   string                `json:"message,omitempty"`
}

// ========== iTunes 搜尋增強 ==========

// ItunesSearchResultWithSong iTunes 搜尋結果（可能已在資料庫中）
type ItunesSearchResultWithSong struct {
	ItunesID       int64      `json:"itunes_id"`
	CollectionName string     `json:"collection_name"`
	TrackName      string     `json:"track_name"`
	ArtistName     string     `json:"artist_name"`
	ArtworkURL     string     `json:"artwork_url"`
	Country        string     `json:"country"`
	ExistingSong   *SongBrief `json:"existing_song,omitempty"` // 如果已在 DB 中，回傳歌曲簡要資訊
}

// SongBrief 歌曲簡要資訊（用於 iTunes 搜尋結果）
type SongBrief struct {
	ID                    uuid.UUID `json:"id"`
	Name                  string    `json:"name"`
	NameReading           *string   `json:"name_reading,omitempty"`
	OriginalArtist        string    `json:"original_artist"`
	OriginalArtistReading *string   `json:"original_artist_reading,omitempty"`
	Arts                  *string   `json:"arts,omitempty"`
	PerformanceCount      int       `json:"performance_count"`
}

type ItunesSearchResponseWithSongs struct {
	Results []ItunesSearchResultWithSong `json:"results"`
}

// ========== AI Provider ==========

// AIProviderResponse AI provider 設定（不含 API key，僅回傳末四碼提示）
type AIProviderResponse struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	Enabled        bool   `json:"enabled"`
	Priority       int    `json:"priority"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	HasKey         bool   `json:"has_key"`
	KeyHint        string `json:"key_hint,omitempty"`
}
