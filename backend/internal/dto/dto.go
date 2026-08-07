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

type ArtistReference struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type SongResponse struct {
	ID                    uuid.UUID            `json:"id"`
	Name                  string               `json:"name"`
	NameReading           *string              `json:"name_reading,omitempty"`
	OriginalArtist        string               `json:"original_artist"`
	OriginalArtistReading *string              `json:"original_artist_reading,omitempty"`
	Artists               []ArtistReference    `json:"artists"`
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
	Artists        []ArtistReference        `json:"artists"`
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
	ID             uuid.UUID                `json:"id"`
	StreamID       string                   `json:"stream_id"`
	StreamTitle    string                   `json:"stream_title"`
	StreamDate     string                   `json:"stream_date"`
	ThumbnailURL   *string                  `json:"thumbnail_url,omitempty"`
	SongID         uuid.UUID                `json:"song_id,omitempty"`
	SongName       string                   `json:"song_name,omitempty"`
	OriginalArtist string                   `json:"original_artist,omitempty"`
	Artists        []ArtistReference        `json:"artists"`
	StartSeconds   int                      `json:"start_seconds"`
	EndSeconds     int                      `json:"end_seconds"`
	Tags           []PerformanceTagResponse `json:"tags"`
	CustomTags     []string                 `json:"custom_tags"`
	Singers        []SingerResponse         `json:"singers"`
	YouTubeURL     string                   `json:"youtube_url"`
	CreatedAt      time.Time                `json:"created_at"`
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
	// 自動採用に届かなかった照合候補（別名義・同名異曲など。UI で選ばせる）
	MatchCandidates []SongMatchCandidate `json:"match_candidates,omitempty"`
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
	Mode      string   `json:"mode,omitempty"`
	SingerID  string   `json:"singer_id,omitempty"` // 対象を絞ったチャンネル（空なら全チャンネル）
	Total     int      `json:"total"`
	Done      int      `json:"done"`
	Failed    int      `json:"failed"`
	Current   string   `json:"current,omitempty"`    // 処理中の配信タイトル
	FailedIDs []string `json:"failed_ids,omitempty"` // AI 失敗が解消しなかった配信
	Message   string   `json:"message,omitempty"`
}

// BatchAnalyzeRequest 一括分析の開始リクエスト
type BatchAnalyzeRequest struct {
	Mode     string `json:"mode"`      // unanalyzed / unprocessed / refresh / reanalyze
	SingerID string `json:"singer_id"` // 対象チャンネル（空なら全チャンネル）
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

	// Chat 拍手偵測結果（Holodex 明示 end との比較用。CommentSong と対称）
	ChatEnd int `json:"chat_end,omitempty"` // chat 偵測出的結束秒數（如果有）
	EndDiff int `json:"end_diff,omitempty"` // |end - chat_end|，只有兩邊都有值時才填

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
	// 自動採用に届かなかった照合候補（CommentSong と対称）
	MatchCandidates []SongMatchCandidate `json:"match_candidates,omitempty"`
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
	MatchReason           string   `json:"match_reason,omitempty"`    // 匹配原因（title_primary, itunes_id …）
	MatchScore            float64  `json:"match_score,omitempty"`     // 確信度 0.0〜1.0
	// DB 歌曲資訊（匹配到時回傳）
	MatchedSongName          *string `json:"matched_song_name,omitempty"`
	MatchedSongNameReading   *string `json:"matched_song_name_reading,omitempty"`
	MatchedSongArtist        *string `json:"matched_song_artist,omitempty"`
	MatchedSongArtistReading *string `json:"matched_song_artist_reading,omitempty"`
	MatchedSongArtURL        *string `json:"matched_song_art_url,omitempty"`
	MatchedSongItunesID      *int64  `json:"matched_song_itunes_id,omitempty"`
	// MatchCandidates は「似ているが自動採用の水準に届かなかった」既存楽曲。
	// 別名義（松任谷由実 / 荒井由実）や同名異曲がここに出る。UI で人に選ばせる。
	MatchCandidates []SongMatchCandidate `json:"match_candidates,omitempty"`
}

// ========== 楽曲の統合候補 ==========

// MergeCandidateSong は統合候補に出す楽曲の要約。
// ItunesIDs は「編曲がどれだけ違うか」を人が判断するための手がかり
// （収録アルバム・再生時間・試聴）をフロントが引くために返す。
type MergeCandidateSong struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	OriginalArtist   string  `json:"original_artist"`
	ArtURL           string  `json:"art_url,omitempty"`
	PerformanceCount int     `json:"performance_count"`
	ItunesIDs        []int64 `json:"itunes_ids,omitempty"`
	Role             string  `json:"role,omitempty"` // AI が説明したこの曲の立ち位置
}

// MergeVerdictResponse は AI の見立て。統合するかどうかの決定ではない。
type MergeVerdictResponse struct {
	SameComposition *bool  `json:"same_composition,omitempty"`
	SameArrangement *bool  `json:"same_arrangement,omitempty"`
	Recommendation  string `json:"recommendation,omitempty"` // merge | keep_separate
	Note            string `json:"note,omitempty"`
	Source          string `json:"source,omitempty"`
	Judged          bool   `json:"judged"`
}

// MergeCandidateResponse は「新しく作られた曲が既存曲と似ている」1 件。
// 照合が外れて新曲として登録されたものを、人が統合して畳むための入口。
type MergeCandidateResponse struct {
	ID           string                `json:"id"`
	Score        float64               `json:"score"`
	Reason       string                `json:"reason"`
	Origin       string                `json:"origin"` // create | scan
	NewSong      MergeCandidateSong    `json:"new_song"`
	ExistingSong MergeCandidateSong    `json:"existing_song"`
	Verdict      *MergeVerdictResponse `json:"verdict,omitempty"`
}

// ========== 照合の学習層（別名義・別表記） ==========

// ArtistAliasMemberResponse は別名義グループの 1 名。
type ArtistAliasMemberResponse struct {
	NameKey     string `json:"name_key"`
	DisplayName string `json:"display_name"`
	Source      string `json:"source"` // manual | ai
	Note        string `json:"note,omitempty"`
}

// ArtistAliasGroupResponse は同一人物としてまとめられた名前の集まり。
type ArtistAliasGroupResponse struct {
	GroupID string                      `json:"group_id"`
	Members []ArtistAliasMemberResponse `json:"members"`
}

// SongAliasResponse は統合から学習した「この表記はこの曲」の 1 件。
type SongAliasResponse struct {
	NameKey    string `json:"name_key"`
	ArtistKey  string `json:"artist_key"`
	Source     string `json:"source"`
	SongID     string `json:"song_id"`
	SongName   string `json:"song_name"`
	SongArtist string `json:"song_artist"`
}

// SongMatchCandidate は既存楽曲との照合候補 1 件。
type SongMatchCandidate struct {
	SongID  string  `json:"song_id"`
	Name    string  `json:"name"`
	Artist  string  `json:"artist"`
	Score   float64 `json:"score"`
	Reason  string  `json:"reason"`
	ArtURL  string  `json:"art_url,omitempty"`
	IsMatch bool    `json:"is_match"` // 自動採用された候補か
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

// ========== 読みのエクスポート / インポート ==========

// ReadingItem はエクスポート/インポート1件（アーティスト or 楽曲の読み）。
type ReadingItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Reading string `json:"reading"`
}

// ReadingsExport は読みデータの一括エクスポート形式。外部 AI で reading を埋めて再取り込みできる。
type ReadingsExport struct {
	Artists []ReadingItem `json:"artists"`
	Songs   []ReadingItem `json:"songs"`
}

// ImportReadingsResult は読み取り込みの結果。
type ImportReadingsResult struct {
	ArtistsUpdated int      `json:"artists_updated"`
	SongsUpdated   int      `json:"songs_updated"`
	Skipped        int      `json:"skipped"` // かな以外・不正な読みで採用しなかった件数
	Errors         []string `json:"errors,omitempty"`
}

// ========== 修正提案（閲覧モードからの提案 → 管理者レビュー） ==========

// MissingSongPayload 「この配信のこの時点に、登録されていない曲がある」という報告の中身。
// 既存レコードの修正ではないので before/after ではなくこの形で持つ。
type MissingSongPayload struct {
	StreamID       string `json:"stream_id"` // YouTube 動画 ID
	SongName       string `json:"song_name"`
	OriginalArtist string `json:"original_artist"`
	StartSeconds   int    `json:"start_seconds"`
	EndSeconds     int    `json:"end_seconds"` // 0 = 未指定（動画の最後まで）
}

// SongSwapPayload 「この歌唱は別の曲だ」という指摘の中身。
//
// 曲の同一性は文字列の差分では表せない（曲名を直すのではなく、別の曲マスタへ繋ぎ替える）ため、
// フィールド差し替えではなくこの形で持つ。
// SongID があれば既存の曲へ、無ければ名前から曲を探す／作る（perf.missing と同じ経路）。
type SongSwapPayload struct {
	SongID         string `json:"song_id"`
	SongName       string `json:"song_name"`
	OriginalArtist string `json:"original_artist"`
	// CurrentSongName は提案時点の曲名。レビュー時に「何から何へ」を見せるためと、
	// 提案後に曲が差し替えられていないかの確認に使う。
	CurrentSongName string `json:"current_song_name"`
}

// CreateSuggestionRequest 修正提案の投稿（要ログイン）。
//
// kind = "field"（既定）… 既存レコードのフィールド差し替え。TargetType / TargetID / Fields を使う。
// kind = "perf.missing" … 未登録曲の追加報告。Payload を使う（TargetID は不要）。
// kind = "perf.meta"    … 歌唱の曲の差し替え。TargetID（歌唱）と SongSwap を使う。
type CreateSuggestionRequest struct {
	TargetType string              `json:"target_type"` // song / artist / performance
	TargetID   string              `json:"target_id"`
	Kind       string              `json:"kind,omitempty"`
	Fields     map[string]string   `json:"fields"` // 提案する編集値（キーは対象の編集可能フィールド）
	Payload    *MissingSongPayload `json:"payload,omitempty"`
	SongSwap   *SongSwapPayload    `json:"song_swap,omitempty"`
	Note       string              `json:"note"` // 提案者コメント（任意）
}

// FieldConflict 承認時に検出した「提案時点の値」と「現在の値」のズレ。
type FieldConflict struct {
	Expected string `json:"expected"`
	Current  string `json:"current"`
}

// SuggestionResponse 提案1件（before/after を差分表示できるよう両方返す）。
type SuggestionResponse struct {
	ID          uuid.UUID         `json:"id"`
	TargetType  string            `json:"target_type"`
	TargetID    uuid.UUID         `json:"target_id"`
	TargetKey   string            `json:"target_key"` // 配信の YouTube 動画 ID（UUID 対象では空）
	TargetLabel string            `json:"target_label"`
	Kind        string            `json:"kind"`
	Before      map[string]string `json:"before"`
	After       map[string]string `json:"after"`
	// Payload は kind = perf.missing のときだけ入る（追加したい曲の内容）
	Payload *MissingSongPayload `json:"payload,omitempty"`
	// Overlaps は kind = perf.missing で、提案の時間帯に既存の歌唱があるときだけ入る
	Overlaps []OverlapInfo `json:"overlaps,omitempty"`
	// SongSwap は kind = perf.meta のときだけ入る（差し替え先の曲）
	SongSwap *SongSwapPayload `json:"song_swap,omitempty"`
	Note     string           `json:"note"`
	Status   string           `json:"status"`

	// Conflicts は未処理の提案について、対象が提案後に変更されたフィールドを示す。
	// 空でなければ、そのまま承認すると他人の編集を巻き戻すことになる。
	Conflicts map[string]FieldConflict `json:"conflicts,omitempty"`

	CreatedBy     *uuid.UUID `json:"created_by"`
	CreatedByName string     `json:"created_by_name"` // 匿名投稿では空
	ReviewNote    string     `json:"review_note"`
	CreatedAt     time.Time  `json:"created_at"`
	ReviewedAt    *time.Time `json:"reviewed_at"`
}

// UpdatePerformanceRequest 歌唱記録1件の部分更新。nil のフィールドは変更しない。
type UpdatePerformanceRequest struct {
	SongID       *string   `json:"song_id"`       // 別の曲へ差し替える場合
	StartSeconds *int      `json:"start_seconds"` // 開始秒
	EndSeconds   *int      `json:"end_seconds"`   // 終了秒（0 = 動画終了まで）
	CustomTags   *[]string `json:"custom_tags"`
	Tags         *[]string `json:"tags"`       // performance_tags の ID
	SingerIDs    *[]string `json:"singer_ids"` // 歌唱チャンネル
}

type SuggestionListResponse struct {
	Suggestions []SuggestionResponse `json:"suggestions"`
	Pagination  PaginationResponse   `json:"pagination"`
}

// OverlapInfo 未登録曲の追加提案と時間が重なる既存の歌唱。
// メドレーなど正当に重なる場合もあるので承認は止めないが、
// 「もう登録されている曲を報告していないか」をレビュー時に気づけるようにする。
type OverlapInfo struct {
	SongName     string `json:"song_name"`
	StartSeconds int    `json:"start_seconds"`
	EndSeconds   int    `json:"end_seconds"`
}

// SuggestionGroup 同一対象に集まった提案。同じ歌唱への通報を1枚で捌くための単位。
type SuggestionGroup struct {
	TargetType  string               `json:"target_type"`
	TargetID    uuid.UUID            `json:"target_id"`
	TargetKey   string               `json:"target_key"`
	TargetLabel string               `json:"target_label"`
	Current     map[string]string    `json:"current"` // 対象の現在値（提案と見比べるため）
	Suggestions []SuggestionResponse `json:"suggestions"`
}

// SuggestionGroupListResponse ページングの単位はグループ（対象）。
type SuggestionGroupListResponse struct {
	Groups     []SuggestionGroup  `json:"groups"`
	Pagination PaginationResponse `json:"pagination"`
}

// BatchReviewRequest 複数提案の一括承認/却下。
type BatchReviewRequest struct {
	IDs    []string `json:"ids"`
	Action string   `json:"action"`          // approve / reject
	Force  bool     `json:"force,omitempty"` // 承認時、衝突していても上書きする
	Note   string   `json:"note,omitempty"`  // 却下理由
}

type BatchReviewResult struct {
	ID       uuid.UUID `json:"id"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	Conflict bool      `json:"conflict,omitempty"` // 対象が変更済みで止まった
}

type BatchReviewResponse struct {
	Succeeded int                 `json:"succeeded"`
	Failed    int                 `json:"failed"`
	Results   []BatchReviewResult `json:"results"`
}

// MergeSuggestionsRequest 同一対象に集まった提案を、管理者が決めた値へ統合して反映する。
//
// 「どれか1つを丸ごと採用」では表せないケース（3人が 6708 / 6710 / 6716 と提案していて
// 中央値を採りたい、誰も出していない値にしたい、項目ごとに別の提案を採りたい）のための操作。
type MergeSuggestionsRequest struct {
	TargetType string            `json:"target_type"`
	TargetID   string            `json:"target_id"`
	Fields     map[string]string `json:"fields"` // 実際に反映する値
	IDs        []string          `json:"ids"`    // このグループの提案（すべて処理済みにする）
	Note       string            `json:"note"`   // レビューメモ（任意）
}

// MergeSuggestionsResponse 反映した値と、採用/不採用として記録した件数。
type MergeSuggestionsResponse struct {
	Applied  map[string]string `json:"applied"`
	Approved int               `json:"approved"` // 採用値と一致していた提案
	Rejected int               `json:"rejected"` // 別の値になった提案
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

// PerformanceListResponse ページングなしの歌唱一覧（首頁のおすすめ・ランダム再生用）
type PerformanceListResponse struct {
	Performances []PerformanceResponse `json:"performances"`
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

// ========== プレイリスト ==========

type CreatePlaylistRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"` // private | unlisted | public（省略時 private）
}

type UpdatePlaylistRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Visibility  *string `json:"visibility,omitempty"`
}

// PlaylistResponse は一覧・詳細で共通に返すプレイリスト情報。
// share_slug は所有者にだけ返す（限定公開 URL を第三者へ漏らさないため）。
type PlaylistResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility"`
	ShareSlug   string    `json:"share_slug,omitempty"`
	ItemCount   int       `json:"item_count"`
	OwnerName   string    `json:"owner_name"`
	IsOwner     bool      `json:"is_owner"`
	CreatedAt   string    `json:"created_at"`
	UpdatedAt   string    `json:"updated_at"`
}

type PlaylistListResponse struct {
	Playlists []PlaylistResponse `json:"playlists"`
	Total     int                `json:"total"`
}

type AddPlaylistItemRequest struct {
	PerformanceID string `json:"performance_id"`
}

type ReorderPlaylistRequest struct {
	PerformanceIDs []string `json:"performance_ids"`
}

// ========== 外部サービス連携の設定（管理画面） ==========

// SecretFieldStatus は機密項目の状態。値そのものは返さない。
type SecretFieldStatus struct {
	Configured bool   `json:"configured"`
	Hint       string `json:"hint,omitempty"` // 末尾4文字
	FromEnv    bool   `json:"from_env"`       // true なら .env 由来（UI で保存すると DB 側が優先される）
}

type IntegrationSettingsResponse struct {
	// EncryptionEnabled が false だと機密を保存できない（SETTINGS_ENCRYPTION_KEY 未設定）
	EncryptionEnabled bool                         `json:"encryption_enabled"`
	Secrets           map[string]SecretFieldStatus `json:"secrets"`
	Plain             map[string]string            `json:"plain"`
	PlainFromEnv      map[string]bool              `json:"plain_from_env"`
}

type UpdateIntegrationSettingsRequest struct {
	// Secrets は項目名 -> 新しい値。空文字の項目は「変更なし」として無視される。
	Secrets map[string]string `json:"secrets,omitempty"`
	// Clear は明示的に消したい項目名。
	Clear                []string `json:"clear,omitempty"`
	GoogleDriveClientID  *string  `json:"google_drive_client_id,omitempty"`
	GoogleSigninClientID *string  `json:"google_signin_client_id,omitempty"`
}
