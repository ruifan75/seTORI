package models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// Singer 演唱者/VTuber
type Singer struct {
	ID           string         `json:"id"`           // YouTube Channel ID
	Name         string         `json:"name"`         // 顯示名稱
	EnglishName  sql.NullString `json:"english_name"` // 英文名稱
	PhotoURL     sql.NullString `json:"photo_url"`    // 頭像 URL
	Organization sql.NullString `json:"organization"` // 所屬組織
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// Song 歌曲 Master
type Song struct {
	ID                    uuid.UUID      `json:"id"`
	Name                  string         `json:"name"`                    // 歌曲名稱
	NameReading           sql.NullString `json:"name_reading"`            // 讀音（平假名）
	OriginalArtist        string         `json:"original_artist"`         // 原唱藝人
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
	HolodexData     []byte         `json:"holodex_data"` // JSONB
	HolodexHash     sql.NullString `json:"holodex_hash"`
	IsProcessed     bool           `json:"is_processed"` // 處理完成
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
	HolodexSongID uuid.NullUUID `json:"holodex_song_id"`
	CreatedAt     time.Time     `json:"created_at"`
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
