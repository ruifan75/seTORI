package repository

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
)

type SongItunesRepository struct {
	db *sql.DB
}

func NewSongItunesRepository(db *sql.DB) *SongItunesRepository {
	return &SongItunesRepository{db: db}
}

// Create 建立新的 iTunes ID 關聯
func (r *SongItunesRepository) Create(si *models.SongITunes) error {
	si.ID = uuid.New()
	query := `
		INSERT INTO song_itunes (id, song_id, itunes_id, collection_name, country, is_primary)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (song_id, itunes_id) DO NOTHING
		RETURNING created_at`

	err := r.db.QueryRow(query, si.ID, si.SongID, si.ITunesID,
		si.CollectionName, si.Country, si.IsPrimary).Scan(&si.CreatedAt)
	if err == sql.ErrNoRows {
		// Already exists, that's okay
		return nil
	}
	if err != nil {
		return fmt.Errorf("create song itunes: %w", err)
	}
	return nil
}

// FindBySongID 根據歌曲 ID 取得所有 iTunes ID 關聯
func (r *SongItunesRepository) FindBySongID(songID uuid.UUID) ([]models.SongITunes, error) {
	query := `
		SELECT id, song_id, itunes_id, collection_name, country, is_primary, created_at
		FROM song_itunes
		WHERE song_id = $1
		ORDER BY is_primary DESC, created_at ASC`

	rows, err := r.db.Query(query, songID)
	if err != nil {
		return nil, fmt.Errorf("find song itunes by song id: %w", err)
	}
	defer rows.Close()

	var results []models.SongITunes
	for rows.Next() {
		var si models.SongITunes
		err := rows.Scan(&si.ID, &si.SongID, &si.ITunesID, &si.CollectionName,
			&si.Country, &si.IsPrimary, &si.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan song itunes: %w", err)
		}
		results = append(results, si)
	}

	return results, nil
}

// FindBySongIDs 批次取得多首歌曲的 iTunes 關聯，避免 N+1
func (r *SongItunesRepository) FindBySongIDs(songIDs []uuid.UUID) (map[uuid.UUID][]models.SongITunes, error) {
	result := make(map[uuid.UUID][]models.SongITunes, len(songIDs))
	if len(songIDs) == 0 {
		return result, nil
	}

	ids := make([]string, len(songIDs))
	for i, id := range songIDs {
		ids[i] = id.String()
	}

	query := `
		SELECT id, song_id, itunes_id, collection_name, country, is_primary, created_at
		FROM song_itunes
		WHERE song_id = ANY($1::uuid[])
		ORDER BY is_primary DESC, created_at ASC`

	rows, err := r.db.Query(query, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("find song itunes by song ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var si models.SongITunes
		if err := rows.Scan(&si.ID, &si.SongID, &si.ITunesID, &si.CollectionName,
			&si.Country, &si.IsPrimary, &si.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan song itunes: %w", err)
		}
		result[si.SongID] = append(result[si.SongID], si)
	}
	return result, rows.Err()
}

// FindByItunesID 根據 iTunes ID 取得關聯的歌曲 ID
func (r *SongItunesRepository) FindByItunesID(itunesID int64) (*models.SongITunes, error) {
	query := `
		SELECT id, song_id, itunes_id, collection_name, country, is_primary, created_at
		FROM song_itunes
		WHERE itunes_id = $1
		LIMIT 1`

	var si models.SongITunes
	err := r.db.QueryRow(query, itunesID).Scan(&si.ID, &si.SongID, &si.ITunesID,
		&si.CollectionName, &si.Country, &si.IsPrimary, &si.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find by itunes id: %w", err)
	}
	return &si, nil
}

// Exists 檢查 iTunes ID 是否已存在
func (r *SongItunesRepository) Exists(itunesID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRow("SELECT EXISTS(SELECT 1 FROM song_itunes WHERE itunes_id = $1)", itunesID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check itunes id exists: %w", err)
	}
	return exists, nil
}

// Delete 刪除 iTunes ID 關聯
func (r *SongItunesRepository) Delete(id uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM song_itunes WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete song itunes: %w", err)
	}
	return nil
}

// DeleteBySongID 刪除某歌曲的所有 iTunes ID 關聯
func (r *SongItunesRepository) DeleteBySongID(songID uuid.UUID) error {
	_, err := r.db.Exec("DELETE FROM song_itunes WHERE song_id = $1", songID)
	if err != nil {
		return fmt.Errorf("delete song itunes by song id: %w", err)
	}
	return nil
}

// SetPrimary 設定主要 iTunes ID（會先將同歌曲的其他設為非主要）
func (r *SongItunesRepository) SetPrimary(songID uuid.UUID, itunesID int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 先將所有設為非主要
	_, err = tx.Exec("UPDATE song_itunes SET is_primary = FALSE WHERE song_id = $1", songID)
	if err != nil {
		return fmt.Errorf("unset primary: %w", err)
	}

	// 設定指定的為主要
	_, err = tx.Exec("UPDATE song_itunes SET is_primary = TRUE WHERE song_id = $1 AND itunes_id = $2",
		songID, itunesID)
	if err != nil {
		return fmt.Errorf("set primary: %w", err)
	}

	return tx.Commit()
}
