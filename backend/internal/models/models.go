package models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Singer 歌手/VTuber
type Singer struct {
	ID           string         `json:"id"`           // YouTube Channel ID
	Name         string         `json:"name"`         // 表示名
	EnglishName  sql.NullString `json:"english_name"` // 英語名
	PhotoURL     sql.NullString `json:"photo_url"`    // アバター URL
	Organization sql.NullString `json:"organization"` // 所属組織
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// Song 楽曲 Master
type Song struct {
	ID                    uuid.UUID      `json:"id"`
	Name                  string         `json:"name"`                    // 楽曲名
	NameReading           sql.NullString `json:"name_reading"`            // 読み（平仮名）
	OriginalArtist        string         `json:"original_artist"`         // 原曲アーティスト
	OriginalArtistReading sql.NullString `json:"original_artist_reading"` // 原唱藝人讀音
	Arts                  sql.NullString `json:"arts"`                    // 封面圖 URL
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

// SongITunes 歌曲的 iTunes ID
type SongITunes struct {
	ID             uuid.UUID      `json:"id"`
	SongID         uuid.UUID      `json:"song_id"`
	ITunesID       int64          `json:"itunes_id"`
	CollectionName sql.NullString `json:"collection_name"` // 專輯名稱
	Country        sql.NullString `json:"country"`         // 國家代碼
	IsPrimary      bool           `json:"is_primary"`
	CreatedAt      time.Time      `json:"created_at"`
}

// Stream 歌回直播
type Stream struct {
	ID              string         `json:"id"` // YouTube Video ID
	Title           string         `json:"title"`
	StreamDate      time.Time      `json:"stream_date"`
	DurationSeconds sql.NullInt32  `json:"duration_seconds"`
	ThumbnailURL    sql.NullString `json:"thumbnail_url"`
	HolodexData     []byte         `json:"holodex_data"` // JSONB - Holodex songs data
	HolodexHash     sql.NullString `json:"holodex_hash"`
	CommentRaw      []byte         `json:"comment_raw"`   // JSONB - Raw comment list
	CommentSongs    []byte         `json:"comment_songs"` // JSONB - Parsed songs (undeduped)
	IsProcessed     bool           `json:"is_processed"`  // 處理完成
	IsHidden        bool           `json:"is_hidden"`    // 隱藏
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// Performance 演出紀錄
type Performance struct {
	ID            uuid.UUID     `json:"id"`
	StreamID      string        `json:"stream_id"`
	SongID        uuid.UUID     `json:"song_id"`
	StartSeconds  int           `json:"start_seconds"`
	EndSeconds    int           `json:"end_seconds"`
	OrderIndex    int           `json:"order_index"`
	HolodexSongID uuid.NullUUID   `json:"holodex_song_id"`
	CustomTags    pq.StringArray `json:"custom_tags"`
	CreatedAt     time.Time      `json:"created_at"`
}

// PerformanceTag 演出版本標籤
type PerformanceTag struct {
	ID          string    `json:"id"` // 標籤代碼
	DisplayName string    `json:"display_name"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"created_at"`
}

// StreamTag 直播類型標籤
type StreamTag struct {
	ID          string    `json:"id"` // 標籤代碼
	DisplayName string    `json:"display_name"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"created_at"`
}

// FilterKeyword フィルターキーワード
type FilterKeyword struct {
	ID        int       `json:"id"`
	Keyword   string    `json:"keyword"`
	Type      string    `json:"type"` // "filter" or "keep"
	CreatedAt time.Time `json:"created_at"`
}

// AIProvider AI provider 設定（OpenAI 相容端點）
type AIProvider struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	BaseURL        string    `json:"base_url"`
	Model          string    `json:"model"`
	APIKey         string    `json:"-"` // 不輸出 API key
	Enabled        bool      `json:"enabled"`
	Priority       int       `json:"priority"`
	TimeoutSeconds int       `json:"timeout_seconds"` // 單次 AI 呼叫的超時秒數（不同 provider 可不同）
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// User 使用者
type User struct {
	ID           uuid.UUID    `json:"id"`
	Username     string       `json:"username"`
	DisplayName  string       `json:"display_name"`
	PasswordHash string       `json:"-"` // 不輸出密碼 hash
	Role         string       `json:"role"`
	CreatedAt    time.Time    `json:"created_at"`
	LastLogin    sql.NullTime `json:"last_login"`
}
