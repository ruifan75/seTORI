package repository

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
	"github.com/lib/pq"
)

type PerformanceRepository struct {
	db *sql.DB
}

func NewPerformanceRepository(db *sql.DB) *PerformanceRepository {
	return &PerformanceRepository{db: db}
}

// PerformanceWithDetails 包含演出及相關資訊的結構
type PerformanceWithDetails struct {
	models.Performance
	StreamTitle    string                  `json:"stream_title"`
	StreamDate     string                  `json:"stream_date"`
	ThumbnailURL   sql.NullString          `json:"thumbnail_url"`
	SongName       string                  `json:"song_name"`
	OriginalArtist string                  `json:"original_artist"`
	Arts           sql.NullString          `json:"arts"`
	Tags           []models.PerformanceTag `json:"tags"`
	Singers        []models.Singer         `json:"singers"`
}

// FindByStreamID 根據 Stream ID 取得所有演出（用於歌回詳情頁）
func (r *PerformanceRepository) FindByStreamID(streamID string) ([]PerformanceWithDetails, error) {
	query := `
		SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index,
		       p.holodex_song_id, p.custom_tags, p.created_at,
		       s.name AS song_name, s.original_artist, s.arts
		FROM performances p
		JOIN songs s ON p.song_id = s.id
		WHERE p.stream_id = $1
		ORDER BY p.start_seconds ASC`

	rows, err := r.db.Query(query, streamID)
	if err != nil {
		return nil, fmt.Errorf("query performances by stream: %w", err)
	}
	defer rows.Close()

	var performances []PerformanceWithDetails
	for rows.Next() {
		var p PerformanceWithDetails
		err := rows.Scan(&p.ID, &p.StreamID, &p.SongID, &p.StartSeconds, &p.EndSeconds,
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt, &p.SongName, &p.OriginalArtist, &p.Arts)
		if err != nil {
			return nil, fmt.Errorf("scan performance: %w", err)
		}

		// 取得標籤
		tags, err := r.GetTags(p.ID)
		if err != nil {
			return nil, err
		}
		p.Tags = tags

		// 取得演唱者
		singers, err := r.GetSingers(p.ID)
		if err != nil {
			return nil, err
		}
		p.Singers = singers

		performances = append(performances, p)
	}

	return performances, nil
}

// FindBySongID 根據 Song ID 取得所有演出（反向查詢核心功能）
// 只顯示非隱藏的 Stream 中的演出
func (r *PerformanceRepository) FindBySongID(songID uuid.UUID, limit, offset int) ([]PerformanceWithDetails, int, error) {
	var total int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		WHERE p.song_id = $1 AND st.is_hidden = FALSE
	`, songID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count performances: %w", err)
	}

	query := `
		SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index,
		       p.holodex_song_id, p.custom_tags, p.created_at,
		       st.title AS stream_title, st.stream_date, st.thumbnail_url
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		WHERE p.song_id = $1 AND st.is_hidden = FALSE
		ORDER BY st.stream_date DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.Query(query, songID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query performances by song: %w", err)
	}
	defer rows.Close()

	var performances []PerformanceWithDetails
	for rows.Next() {
		var p PerformanceWithDetails
		err := rows.Scan(&p.ID, &p.StreamID, &p.SongID, &p.StartSeconds, &p.EndSeconds,
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt,
			&p.StreamTitle, &p.StreamDate, &p.ThumbnailURL)
		if err != nil {
			return nil, 0, fmt.Errorf("scan performance: %w", err)
		}

		// 取得標籤
		tags, err := r.GetTags(p.ID)
		if err != nil {
			return nil, 0, err
		}
		p.Tags = tags

		// 取得演唱者
		singers, err := r.GetSingers(p.ID)
		if err != nil {
			return nil, 0, err
		}
		p.Singers = singers

		performances = append(performances, p)
	}

	return performances, total, nil
}

// FindByTagID は指定の演出タグが付いた演出を新しい順で返す（タグ検索用、非表示配信は除外）。
func (r *PerformanceRepository) FindByTagID(tagID string, limit, offset int) ([]PerformanceWithDetails, int, error) {
	var total int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		JOIN performance_performance_tags ppt ON ppt.performance_id = p.id
		WHERE ppt.tag_id = $1 AND st.is_hidden = FALSE
	`, tagID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count performances by tag: %w", err)
	}

	query := `
		SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index,
		       p.holodex_song_id, p.custom_tags, p.created_at,
		       st.title AS stream_title, st.stream_date, st.thumbnail_url,
		       s.name AS song_name, s.original_artist, s.arts
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		JOIN songs s ON p.song_id = s.id
		JOIN performance_performance_tags ppt ON ppt.performance_id = p.id
		WHERE ppt.tag_id = $1 AND st.is_hidden = FALSE
		ORDER BY st.stream_date DESC, p.order_index ASC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.Query(query, tagID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query performances by tag: %w", err)
	}
	defer rows.Close()

	var performances []PerformanceWithDetails
	for rows.Next() {
		var p PerformanceWithDetails
		err := rows.Scan(&p.ID, &p.StreamID, &p.SongID, &p.StartSeconds, &p.EndSeconds,
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt,
			&p.StreamTitle, &p.StreamDate, &p.ThumbnailURL,
			&p.SongName, &p.OriginalArtist, &p.Arts)
		if err != nil {
			return nil, 0, fmt.Errorf("scan performance: %w", err)
		}

		tags, err := r.GetTags(p.ID)
		if err != nil {
			return nil, 0, err
		}
		p.Tags = tags

		singers, err := r.GetSingers(p.ID)
		if err != nil {
			return nil, 0, err
		}
		p.Singers = singers

		performances = append(performances, p)
	}

	return performances, total, rows.Err()
}

// Create 建立新演出
func (r *PerformanceRepository) Create(p *models.Performance) error {
	p.ID = uuid.New()
	query := `
		INSERT INTO performances (id, stream_id, song_id, start_seconds, end_seconds, order_index, holodex_song_id, custom_tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at`

	err := r.db.QueryRow(query, p.ID, p.StreamID, p.SongID, p.StartSeconds, p.EndSeconds,
		p.OrderIndex, p.HolodexSongID, p.CustomTags).Scan(&p.CreatedAt)
	if err != nil {
		return fmt.Errorf("create performance: %w", err)
	}
	return nil
}

// Delete 刪除演出
func (r *PerformanceRepository) Delete(id uuid.UUID) error {
	// 先刪除關聯
	_, err := r.db.Exec("DELETE FROM performance_performance_tags WHERE performance_id = $1", id)
	if err != nil {
		return fmt.Errorf("delete performance tags: %w", err)
	}

	_, err = r.db.Exec("DELETE FROM performance_singers WHERE performance_id = $1", id)
	if err != nil {
		return fmt.Errorf("delete performance singers: %w", err)
	}

	_, err = r.db.Exec("DELETE FROM performances WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete performance: %w", err)
	}
	return nil
}

// GetTags 取得演出的所有標籤
func (r *PerformanceRepository) GetTags(performanceID uuid.UUID) ([]models.PerformanceTag, error) {
	query := `
		SELECT pt.id, pt.display_name, pt.color, pt.created_at
		FROM performance_tags pt
		JOIN performance_performance_tags ppt ON pt.id = ppt.tag_id
		WHERE ppt.performance_id = $1`

	rows, err := r.db.Query(query, performanceID)
	if err != nil {
		return nil, fmt.Errorf("get performance tags: %w", err)
	}
	defer rows.Close()

	var tags []models.PerformanceTag
	for rows.Next() {
		var t models.PerformanceTag
		err := rows.Scan(&t.ID, &t.DisplayName, &t.Color, &t.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan performance tag: %w", err)
		}
		tags = append(tags, t)
	}

	return tags, nil
}

// AddTag 為演出添加標籤
func (r *PerformanceRepository) AddTag(performanceID uuid.UUID, tagID string) error {
	query := `INSERT INTO performance_performance_tags (performance_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(query, performanceID, tagID)
	if err != nil {
		return fmt.Errorf("add performance tag: %w", err)
	}
	return nil
}

// RemoveTag 移除演出標籤
func (r *PerformanceRepository) RemoveTag(performanceID uuid.UUID, tagID string) error {
	_, err := r.db.Exec("DELETE FROM performance_performance_tags WHERE performance_id = $1 AND tag_id = $2", performanceID, tagID)
	if err != nil {
		return fmt.Errorf("remove performance tag: %w", err)
	}
	return nil
}

// GetValidTagIDs 取得有效的標籤 ID（過濾掉不存在的標籤）
func (r *PerformanceRepository) GetValidTagIDs(tagIDs []string) ([]string, error) {
	if len(tagIDs) == 0 {
		return []string{}, nil
	}

	// 查詢所有有效的標籤 ID
	query := `SELECT id FROM performance_tags WHERE id = ANY($1)`
	rows, err := r.db.Query(query, pq.Array(tagIDs))
	if err != nil {
		return nil, fmt.Errorf("query valid tags: %w", err)
	}
	defer rows.Close()

	validTags := make([]string, 0)
	for rows.Next() {
		var tagID string
		if err := rows.Scan(&tagID); err != nil {
			return nil, fmt.Errorf("scan tag id: %w", err)
		}
		validTags = append(validTags, tagID)
	}

	return validTags, nil
}

// SetTags 設定演出的所有標籤（先刪除再新增，自動過濾無效標籤）
func (r *PerformanceRepository) SetTags(performanceID uuid.UUID, tagIDs []string) error {
	_, err := r.db.Exec("DELETE FROM performance_performance_tags WHERE performance_id = $1", performanceID)
	if err != nil {
		return fmt.Errorf("clear performance tags: %w", err)
	}

	// 過濾出有效的標籤
	validTags, err := r.GetValidTagIDs(tagIDs)
	if err != nil {
		return err
	}

	for _, tagID := range validTags {
		err := r.AddTag(performanceID, tagID)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetSingers 取得演出的所有演唱者
func (r *PerformanceRepository) GetSingers(performanceID uuid.UUID) ([]models.Singer, error) {
	query := `
		SELECT s.id, s.name, s.english_name, s.photo_url, s.organization, s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		JOIN performance_singers ps ON s.id = ps.singer_id
		WHERE ps.performance_id = $1`

	rows, err := r.db.Query(query, performanceID)
	if err != nil {
		return nil, fmt.Errorf("get performance singers: %w", err)
	}
	defer rows.Close()

	var singers []models.Singer
	for rows.Next() {
		var s models.Singer
		err := rows.Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL, &s.Organization, &s.MetadataSource, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan singer: %w", err)
		}
		singers = append(singers, s)
	}

	return singers, nil
}

// AddSinger 為演出添加演唱者
func (r *PerformanceRepository) AddSinger(performanceID uuid.UUID, singerID string) error {
	query := `INSERT INTO performance_singers (performance_id, singer_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(query, performanceID, singerID)
	if err != nil {
		return fmt.Errorf("add performance singer: %w", err)
	}
	return nil
}

// SetSingers 設定演出的所有演唱者
func (r *PerformanceRepository) SetSingers(performanceID uuid.UUID, singerIDs []string) error {
	_, err := r.db.Exec("DELETE FROM performance_singers WHERE performance_id = $1", performanceID)
	if err != nil {
		return fmt.Errorf("clear performance singers: %w", err)
	}

	for _, singerID := range singerIDs {
		err := r.AddSinger(performanceID, singerID)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteByStreamID 刪除指定 Stream 的所有演出記錄
func (r *PerformanceRepository) DeleteByStreamID(streamID string) error {
	// 先取得所有 performance ID
	rows, err := r.db.Query("SELECT id FROM performances WHERE stream_id = $1", streamID)
	if err != nil {
		return fmt.Errorf("query performances: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}

	// 刪除每個演出的關聯資料
	for _, id := range ids {
		if err := r.Delete(id); err != nil {
			return err
		}
	}

	return nil
}

// FindBySingerID 根據演唱者 ID 取得所有演出（支援分頁）
// 只顯示非隱藏的 Stream 中的演出
func (r *PerformanceRepository) FindBySingerID(singerID string, limit, offset int, sort, dir string) ([]PerformanceWithDetails, int, error) {
	var total int
	err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT p.id)
		FROM performances p
		JOIN performance_singers ps ON p.id = ps.performance_id
		JOIN streams st ON p.stream_id = st.id
		WHERE ps.singer_id = $1 AND st.is_hidden = FALSE
	`, singerID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count performances: %w", err)
	}

	// 既定は配信日の新しい順。"song"=曲名順、"stream"=歌枠タイトル順。
	order := "st.stream_date " + dirOr(dir, "desc") + ", p.start_seconds ASC"
	switch sort {
	case "song":
		order = nameSortOrderDir("s.name", "s.name_reading", dir)
	case "stream":
		order = nameSortOrderDir("st.title", "''", dir)
	}

	query := `
		SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index,
		       p.holodex_song_id, p.custom_tags, p.created_at,
		       st.title AS stream_title, st.stream_date, st.thumbnail_url,
		       s.name AS song_name, s.original_artist
		FROM performances p
		JOIN performance_singers ps ON p.id = ps.performance_id
		JOIN streams st ON p.stream_id = st.id
		JOIN songs s ON p.song_id = s.id
		WHERE ps.singer_id = $1 AND st.is_hidden = FALSE
		ORDER BY ` + order + `
		LIMIT $2 OFFSET $3`

	rows, err := r.db.Query(query, singerID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query performances by singer: %w", err)
	}
	defer rows.Close()

	var performances []PerformanceWithDetails
	for rows.Next() {
		var p PerformanceWithDetails
		err := rows.Scan(&p.ID, &p.StreamID, &p.SongID, &p.StartSeconds, &p.EndSeconds,
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt,
			&p.StreamTitle, &p.StreamDate, &p.ThumbnailURL,
			&p.SongName, &p.OriginalArtist)
		if err != nil {
			return nil, 0, fmt.Errorf("scan performance: %w", err)
		}

		// 取得標籤
		tags, err := r.GetTags(p.ID)
		if err != nil {
			return nil, 0, err
		}
		p.Tags = tags

		// 取得演唱者
		singers, err := r.GetSingers(p.ID)
		if err != nil {
			return nil, 0, err
		}
		p.Singers = singers

		performances = append(performances, p)
	}

	return performances, total, nil
}

// ========== 首頁：ランダム再生 ==========

// perfDetailSelect は配信・楽曲情報付きで歌唱を引く共通 SELECT（FindByTagID と同形）。
const perfDetailSelect = `
	SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index,
	       p.holodex_song_id, p.custom_tags, p.created_at,
	       st.title AS stream_title, st.stream_date, st.thumbnail_url,
	       s.name AS song_name, s.original_artist, s.arts
	FROM performances p
	JOIN streams st ON p.stream_id = st.id
	JOIN songs s ON p.song_id = s.id`

// queryPerformanceDetails は perfDetailSelect 形のクエリを実行し、タグ・歌手も付けて返す。
func (r *PerformanceRepository) queryPerformanceDetails(query string, args ...interface{}) ([]PerformanceWithDetails, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query performance details: %w", err)
	}
	defer rows.Close()

	performances := make([]PerformanceWithDetails, 0)
	for rows.Next() {
		var p PerformanceWithDetails
		err := rows.Scan(&p.ID, &p.StreamID, &p.SongID, &p.StartSeconds, &p.EndSeconds,
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt,
			&p.StreamTitle, &p.StreamDate, &p.ThumbnailURL,
			&p.SongName, &p.OriginalArtist, &p.Arts)
		if err != nil {
			return nil, fmt.Errorf("scan performance: %w", err)
		}

		tags, err := r.GetTags(p.ID)
		if err != nil {
			return nil, err
		}
		p.Tags = tags

		singers, err := r.GetSingers(p.ID)
		if err != nil {
			return nil, err
		}
		p.Singers = singers

		performances = append(performances, p)
	}

	return performances, rows.Err()
}

// FindRandom は曲単位で重複しないランダムな歌唱を返す。
// 非表示に加え、再生できない可能性が高い メン限・アーカイブなし の配信も除外する。
func (r *PerformanceRepository) FindRandom(limit int, excludedSongIDs []string) ([]PerformanceWithDetails, error) {
	if excludedSongIDs == nil {
		excludedSongIDs = []string{}
	}
	query := `
		WITH random_per_song AS (
			SELECT DISTINCT ON (p.song_id) p.id
			FROM performances p
			JOIN streams st ON p.stream_id = st.id
			WHERE st.is_hidden = FALSE
			  AND NOT (p.song_id = ANY($2::uuid[]))
			  AND NOT EXISTS (
				SELECT 1 FROM stream_stream_tags sst
				WHERE sst.stream_id = st.id AND sst.tag_id IN ('members_only', 'unarchived')
			  )
			ORDER BY p.song_id, random()
		)
	` + perfDetailSelect + `
		JOIN random_per_song rps ON rps.id = p.id
		ORDER BY random()
		LIMIT $1`

	return r.queryPerformanceDetails(query, limit, pq.Array(excludedSongIDs))
}
