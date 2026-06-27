package repository

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/pkg/util"
	"github.com/lib/pq"
)

type SongRepository struct {
	db *sql.DB
}

func NewSongRepository(db *sql.DB) *SongRepository {
	return &SongRepository{db: db}
}

// FindAll 取得所有歌曲（支援分頁和搜尋）
func (r *SongRepository) FindAll(limit, offset int, search string) ([]models.Song, int, error) {
	var total int
	var rows *sql.Rows
	var err error

	if search != "" {
		// 使用 pg_trgm 進行模糊搜尋
		countQuery := `
			SELECT COUNT(*) FROM songs
			WHERE name ILIKE $1 OR original_artist ILIKE $1 OR name_reading ILIKE $1`
		searchPattern := "%" + search + "%"
		err = r.db.QueryRow(countQuery, searchPattern).Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("count songs: %w", err)
		}

		query := `
			SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at
			FROM songs
			WHERE name ILIKE $1 OR original_artist ILIKE $1 OR name_reading ILIKE $1
			ORDER BY name ASC
			LIMIT $2 OFFSET $3`
		rows, err = r.db.Query(query, searchPattern, limit, offset)
	} else {
		err = r.db.QueryRow("SELECT COUNT(*) FROM songs").Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("count songs: %w", err)
		}

		query := `
			SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at
			FROM songs
			ORDER BY name ASC
			LIMIT $1 OFFSET $2`
		rows, err = r.db.Query(query, limit, offset)
	}

	if err != nil {
		return nil, 0, fmt.Errorf("query songs: %w", err)
	}
	defer rows.Close()

	var songs []models.Song
	for rows.Next() {
		var s models.Song
		err := rows.Scan(&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
			&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("scan song: %w", err)
		}
		songs = append(songs, s)
	}

	return songs, total, nil
}

// FindByID 根據 ID 取得歌曲
func (r *SongRepository) FindByID(id uuid.UUID) (*models.Song, error) {
	query := `
		SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at
		FROM songs WHERE id = $1`

	var s models.Song
	err := r.db.QueryRow(query, id).Scan(
		&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
		&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find song by id: %w", err)
	}
	return &s, nil
}

// FindByNameAndArtist 根據歌名和藝人查詢（用於正規化時檢查是否已存在）
// 先精確比對，找不到時用 lower + trim 模糊比對（處理大小寫、空格差異）
// Go 側先做 NFKC 正規化（處理 Ⅱ→II 等 Unicode 差異）
func (r *SongRepository) FindByNameAndArtist(name, artist string) (*models.Song, error) {
	// 精確比對
	exactQuery := `
		SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at
		FROM songs WHERE name = $1 AND original_artist = $2`

	var s models.Song
	err := r.db.QueryRow(exactQuery, name, artist).Scan(
		&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
		&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
	if err == nil {
		return &s, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("find song by name and artist: %w", err)
	}

	// Fallback: NFKC 正規化 + lower + trim 模糊比對
	normalizedName := util.NormalizeUnicode(name)
	normalizedArtist := util.NormalizeUnicode(artist)

	fuzzyQuery := `
		SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at
		FROM songs
		WHERE lower(replace(trim(normalize(name, NFKC)), ' ', '')) = lower(replace(trim($1), ' ', ''))
		  AND lower(replace(trim(normalize(original_artist, NFKC)), ' ', '')) = lower(replace(trim($2), ' ', ''))`

	err = r.db.QueryRow(fuzzyQuery, normalizedName, normalizedArtist).Scan(
		&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
		&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find song by name and artist (fuzzy): %w", err)
	}
	return &s, nil
}

// Create 建立新歌曲
func (r *SongRepository) Create(s *models.Song) error {
	s.ID = uuid.New()
	query := `
		INSERT INTO songs (id, name, name_reading, original_artist, original_artist_reading, arts)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(query, s.ID, s.Name, s.NameReading, s.OriginalArtist,
		s.OriginalArtistReading, s.Arts).Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create song: %w", err)
	}
	return nil
}

// Update 更新歌曲
func (r *SongRepository) Update(s *models.Song) error {
	query := `
		UPDATE songs
		SET name = $2, name_reading = $3, original_artist = $4,
		    original_artist_reading = $5, arts = $6, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	err := r.db.QueryRow(query, s.ID, s.Name, s.NameReading, s.OriginalArtist,
		s.OriginalArtistReading, s.Arts).Scan(&s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update song: %w", err)
	}
	return nil
}

// Delete 刪除歌曲
func (r *SongRepository) Delete(id uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM songs WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete song: %w", err)
	}
	return nil
}

// GetPerformanceCount 取得歌曲的演出次數（只計算非隱藏的 Stream）
func (r *SongRepository) GetPerformanceCount(songID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		WHERE p.song_id = $1 AND st.is_hidden = FALSE
	`, songID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get performance count: %w", err)
	}
	return count, nil
}

// GetPerformanceCounts 批次取得多首歌曲的演出次數（只計算非隱藏的 Stream），避免 N+1
func (r *SongRepository) GetPerformanceCounts(songIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	counts := make(map[uuid.UUID]int, len(songIDs))
	if len(songIDs) == 0 {
		return counts, nil
	}

	ids := make([]string, len(songIDs))
	for i, id := range songIDs {
		ids[i] = id.String()
	}

	query := `
		SELECT p.song_id, COUNT(*)
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		WHERE p.song_id = ANY($1::uuid[]) AND st.is_hidden = FALSE
		GROUP BY p.song_id`

	rows, err := r.db.Query(query, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("get performance counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var c int
		if err := rows.Scan(&id, &c); err != nil {
			return nil, fmt.Errorf("scan performance count: %w", err)
		}
		counts[id] = c
	}
	return counts, rows.Err()
}

// FindByItunesID 根據 iTunes ID 查詢歌曲
func (r *SongRepository) FindByItunesID(itunesID int64) (*models.Song, error) {
	query := `
		SELECT s.id, s.name, s.name_reading, s.original_artist, s.original_artist_reading, s.arts, s.created_at, s.updated_at
		FROM songs s
		JOIN song_itunes si ON s.id = si.song_id
		WHERE si.itunes_id = $1
		LIMIT 1`

	var s models.Song
	err := r.db.QueryRow(query, itunesID).Scan(
		&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
		&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find song by itunes id: %w", err)
	}
	return &s, nil
}

// SearchSimilar 使用 trigram 搜尋相似歌曲（用於 AI 正規化建議）
func (r *SongRepository) SearchSimilar(name string, limit int) ([]models.Song, error) {
	query := `
		SELECT id, name, name_reading, original_artist, original_artist_reading, arts, created_at, updated_at,
		       similarity(name, $1) AS sim
		FROM songs
		WHERE similarity(name, $1) > 0.3
		ORDER BY sim DESC
		LIMIT $2`

	rows, err := r.db.Query(query, name, limit)
	if err != nil {
		return nil, fmt.Errorf("search similar songs: %w", err)
	}
	defer rows.Close()

	var songs []models.Song
	for rows.Next() {
		var s models.Song
		var sim float64
		err := rows.Scan(&s.ID, &s.Name, &s.NameReading, &s.OriginalArtist,
			&s.OriginalArtistReading, &s.Arts, &s.CreatedAt, &s.UpdatedAt, &sim)
		if err != nil {
			return nil, fmt.Errorf("scan similar song: %w", err)
		}
		songs = append(songs, s)
	}

	return songs, nil
}

// MergeSong 將來源歌曲的所有 performances 合併至目標歌曲，然後刪除來源歌曲
func (r *SongRepository) MergeSong(sourceSongID, targetSongID uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. 更新所有 performances，將 song_id 從來源改為目標
	updateQuery := `
		UPDATE performances 
		SET song_id = $1 
		WHERE song_id = $2`
	_, err = tx.Exec(updateQuery, targetSongID, sourceSongID)
	if err != nil {
		return fmt.Errorf("update performances: %w", err)
	}

	// 2. 刪除來源歌曲（及其關聯的 song_itunes）
	// song_itunes 設有 ON DELETE CASCADE，所以會自動刪除
	deleteQuery := `DELETE FROM songs WHERE id = $1`
	_, err = tx.Exec(deleteQuery, sourceSongID)
	if err != nil {
		return fmt.Errorf("delete source song: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
