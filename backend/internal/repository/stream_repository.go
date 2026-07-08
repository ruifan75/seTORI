package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/pkg/util"
	"github.com/lib/pq"
)

type StreamRepository struct {
	db *sql.DB
}

func NewStreamRepository(db *sql.DB) *StreamRepository {
	return &StreamRepository{db: db}
}

// FindAll 取得所有歌回（支援分頁，預設不顯示隱藏的）
func (r *StreamRepository) FindAll(limit, offset int, includeHidden bool) ([]models.Stream, int, error) {
	var total int
	var countQuery string
	if includeHidden {
		countQuery = "SELECT COUNT(*) FROM streams"
	} else {
		countQuery = "SELECT COUNT(*) FROM streams WHERE is_hidden = FALSE"
	}
	err := r.db.QueryRow(countQuery).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count streams: %w", err)
	}

	var query string
	if includeHidden {
		query = `
			SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, is_processed, is_hidden, created_at, updated_at
			FROM streams
			ORDER BY stream_date DESC
			LIMIT $1 OFFSET $2`
	} else {
		query = `
			SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, is_processed, is_hidden, created_at, updated_at
			FROM streams
			WHERE is_hidden = FALSE
			ORDER BY stream_date DESC
			LIMIT $1 OFFSET $2`
	}

	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		err := rows.Scan(&s.ID, &s.Title, &s.StreamDate, &s.DurationSeconds,
			&s.ThumbnailURL, &s.HolodexData, &s.HolodexHash, &s.CommentRaw, &s.CommentSongs, &s.IsProcessed, &s.IsHidden, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}

	return streams, total, nil
}

// FindByID 根據 Video ID 取得歌回
func (r *StreamRepository) FindByID(id string) (*models.Stream, error) {
	query := `
		SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, is_processed, is_hidden, created_at, updated_at
		FROM streams WHERE id = $1`

	var s models.Stream
	err := r.db.QueryRow(query, id).Scan(
		&s.ID, &s.Title, &s.StreamDate, &s.DurationSeconds,
		&s.ThumbnailURL, &s.HolodexData, &s.HolodexHash, &s.CommentRaw, &s.CommentSongs, &s.IsProcessed, &s.IsHidden, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find stream by id: %w", err)
	}
	return &s, nil
}

// Create 建立新歌回
func (r *StreamRepository) Create(s *models.Stream) error {
	query := `
		INSERT INTO streams (id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(query, s.ID, s.Title, s.StreamDate, s.DurationSeconds,
		s.ThumbnailURL, s.HolodexData, s.HolodexHash).
		Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}
	return nil
}

// Update 更新歌回（完整更新，包含大型 JSONB 欄位，僅用於同步/分析流程）
func (r *StreamRepository) Update(s *models.Stream) error {
	// 預先清理可能導致 "invalid input syntax for type json" 的壞資料（如舊 backfill 產生的 [null]）
	s.HolodexData = util.SanitizeJSONB(s.HolodexData)
	s.CommentRaw = util.SanitizeJSONB(s.CommentRaw)
	s.CommentSongs = util.SanitizeJSONB(s.CommentSongs)

	query := `
		UPDATE streams
		SET title = $2, stream_date = $3, duration_seconds = $4, thumbnail_url = $5,
		    holodex_data = $6, holodex_hash = $7, comment_raw = $8, comment_songs = $9, is_processed = $10, is_hidden = $11, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	err := r.db.QueryRow(query, s.ID, s.Title, s.StreamDate, s.DurationSeconds,
		s.ThumbnailURL, s.HolodexData, s.HolodexHash, s.CommentRaw, s.CommentSongs, s.IsProcessed, s.IsHidden).
		Scan(&s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update stream: %w", err)
	}
	return nil
}

// UpdateMetadata 只更新可由使用者編輯的 metadata 欄位，不動大型 JSONB（holodex_data / comment_*）
// 這樣可以避免在一般資訊更新時重寫可能有問題的 JSONB 資料
func (r *StreamRepository) UpdateMetadata(id string, title string, streamDate time.Time, isProcessed, isHidden bool) error {
	query := `
		UPDATE streams
		SET title = $2, stream_date = $3, is_processed = $4, is_hidden = $5, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at`

	var updatedAt time.Time
	err := r.db.QueryRow(query, id, title, streamDate, isProcessed, isHidden).Scan(&updatedAt)
	if err != nil {
		return fmt.Errorf("update stream metadata: %w", err)
	}
	return nil
}

// UpdateStatus 更新歌回狀態（處理完成/隱藏）
func (r *StreamRepository) UpdateStatus(id string, isProcessed, isHidden bool) error {
	query := `UPDATE streams SET is_processed = $2, is_hidden = $3, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(query, id, isProcessed, isHidden)
	if err != nil {
		return fmt.Errorf("update stream status: %w", err)
	}
	return nil
}

// Upsert 建立或更新歌回（用於 Holodex 同步）
func (r *StreamRepository) Upsert(s *models.Stream) error {
	query := `
		INSERT INTO streams (id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, is_hidden)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			title = EXCLUDED.title,
			stream_date = EXCLUDED.stream_date,
			duration_seconds = EXCLUDED.duration_seconds,
			thumbnail_url = EXCLUDED.thumbnail_url,
			holodex_data = EXCLUDED.holodex_data,
			holodex_hash = EXCLUDED.holodex_hash,
			is_hidden = EXCLUDED.is_hidden,
			updated_at = NOW()
		RETURNING created_at, updated_at`

	err := r.db.QueryRow(query, s.ID, s.Title, s.StreamDate, s.DurationSeconds,
		s.ThumbnailURL, s.HolodexData, s.HolodexHash, s.IsHidden).
		Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert stream: %w", err)
	}
	return nil
}

// Delete 刪除歌回
func (r *StreamRepository) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM streams WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete stream: %w", err)
	}
	return nil
}

// FindWithoutCommentSongs 取得有 comment_raw 的全部 stream（用於 backfill / 重新生成）
func (r *StreamRepository) FindWithoutCommentSongs() ([]models.Stream, error) {
	query := `
		SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, is_processed, is_hidden, created_at, updated_at
		FROM streams
		WHERE comment_raw IS NOT NULL AND comment_raw != 'null'`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query streams without comment songs: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		err := rows.Scan(&s.ID, &s.Title, &s.StreamDate, &s.DurationSeconds,
			&s.ThumbnailURL, &s.HolodexData, &s.HolodexHash, &s.CommentRaw, &s.CommentSongs, &s.IsProcessed, &s.IsHidden, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}

	return streams, nil
}

// FindIDsWithCommentSongs 取得所有有 comment_songs 的歌回 ID（供拍手 end backfill）
func (r *StreamRepository) FindIDsWithCommentSongs() ([]string, error) {
	rows, err := r.db.Query(`
		SELECT id FROM streams
		WHERE comment_songs IS NOT NULL AND comment_songs::text NOT IN ('null', '[]')
		ORDER BY stream_date DESC`)
	if err != nil {
		return nil, fmt.Errorf("query streams with comment songs: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FindByDateRange 根據日期範圍取得歌回
func (r *StreamRepository) FindByDateRange(start, end time.Time) ([]models.Stream, error) {
	query := `
		SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, is_processed, is_hidden, created_at, updated_at
		FROM streams
		WHERE stream_date >= $1 AND stream_date <= $2
		ORDER BY stream_date DESC`

	rows, err := r.db.Query(query, start, end)
	if err != nil {
		return nil, fmt.Errorf("query streams by date range: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		err := rows.Scan(&s.ID, &s.Title, &s.StreamDate, &s.DurationSeconds,
			&s.ThumbnailURL, &s.HolodexData, &s.HolodexHash, &s.CommentRaw, &s.CommentSongs, &s.IsProcessed, &s.IsHidden, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}

	return streams, nil
}

// GetTags 取得歌回的所有標籤
func (r *StreamRepository) GetTags(streamID string) ([]models.StreamTag, error) {
	query := `
		SELECT st.id, st.display_name, st.color, st.created_at
		FROM stream_tags st
		JOIN stream_stream_tags sst ON st.id = sst.tag_id
		WHERE sst.stream_id = $1`

	rows, err := r.db.Query(query, streamID)
	if err != nil {
		return nil, fmt.Errorf("get stream tags: %w", err)
	}
	defer rows.Close()

	var tags []models.StreamTag
	for rows.Next() {
		var t models.StreamTag
		err := rows.Scan(&t.ID, &t.DisplayName, &t.Color, &t.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan stream tag: %w", err)
		}
		tags = append(tags, t)
	}

	return tags, nil
}

// GetTagsForStreams 批次取得多個歌回的標籤，避免 N+1
func (r *StreamRepository) GetTagsForStreams(streamIDs []string) (map[string][]models.StreamTag, error) {
	result := make(map[string][]models.StreamTag, len(streamIDs))
	if len(streamIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT sst.stream_id, st.id, st.display_name, st.color, st.created_at
		FROM stream_tags st
		JOIN stream_stream_tags sst ON st.id = sst.tag_id
		WHERE sst.stream_id = ANY($1)`

	rows, err := r.db.Query(query, pq.Array(streamIDs))
	if err != nil {
		return nil, fmt.Errorf("get tags for streams: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var streamID string
		var t models.StreamTag
		if err := rows.Scan(&streamID, &t.ID, &t.DisplayName, &t.Color, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan stream tag: %w", err)
		}
		result[streamID] = append(result[streamID], t)
	}
	return result, rows.Err()
}

// GetSingersForStreams 批次取得多個歌回的參與者與頻道擁有者，避免 N+1
func (r *StreamRepository) GetSingersForStreams(streamIDs []string) (participants map[string][]models.Singer, owners map[string]*models.Singer, err error) {
	participants = make(map[string][]models.Singer, len(streamIDs))
	owners = make(map[string]*models.Singer, len(streamIDs))
	if len(streamIDs) == 0 {
		return participants, owners, nil
	}

	query := `
		SELECT ss.stream_id, ss.is_owner, s.id, s.name, s.english_name, s.photo_url, s.organization, s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		JOIN stream_singers ss ON s.id = ss.singer_id
		WHERE ss.stream_id = ANY($1)`

	rows, err := r.db.Query(query, pq.Array(streamIDs))
	if err != nil {
		return nil, nil, fmt.Errorf("get singers for streams: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var streamID string
		var isOwner bool
		var sg models.Singer
		if err := rows.Scan(&streamID, &isOwner, &sg.ID, &sg.Name, &sg.EnglishName,
			&sg.PhotoURL, &sg.Organization, &sg.MetadataSource, &sg.CreatedAt, &sg.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan stream singer: %w", err)
		}
		participants[streamID] = append(participants[streamID], sg)
		if isOwner {
			owner := sg
			owners[streamID] = &owner
		}
	}
	return participants, owners, rows.Err()
}

// AddTag 為歌回添加標籤
func (r *StreamRepository) AddTag(streamID, tagID string) error {
	query := `INSERT INTO stream_stream_tags (stream_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(query, streamID, tagID)
	if err != nil {
		return fmt.Errorf("add stream tag: %w", err)
	}
	return nil
}

// RemoveTag 移除歌回標籤
func (r *StreamRepository) RemoveTag(streamID, tagID string) error {
	_, err := r.db.Exec("DELETE FROM stream_stream_tags WHERE stream_id = $1 AND tag_id = $2", streamID, tagID)
	if err != nil {
		return fmt.Errorf("remove stream tag: %w", err)
	}
	return nil
}

// ========== 分析快取（comment / holodex 正規化結果，以來源 hash 為鍵） ==========

// SaveCommentRaw 只更新 comment_raw（同步時用，不動 comment_songs/其他大欄位）
func (r *StreamRepository) SaveCommentRaw(id string, raw []byte) error {
	raw = util.SanitizeJSONB(raw)
	_, err := r.db.Exec(`UPDATE streams SET comment_raw = $2, updated_at = NOW() WHERE id = $1`, id, raw)
	if err != nil {
		return fmt.Errorf("save comment raw: %w", err)
	}
	return nil
}

// GetCommentSongsHash 取得 comment_songs 是從哪個 comment_raw hash 算出來的（快取有效性判斷用）
func (r *StreamRepository) GetCommentSongsHash(id string) (sql.NullString, error) {
	var h sql.NullString
	err := r.db.QueryRow(`SELECT comment_songs_hash FROM streams WHERE id = $1`, id).Scan(&h)
	if err == sql.ErrNoRows {
		return sql.NullString{}, nil
	}
	if err != nil {
		return sql.NullString{}, fmt.Errorf("get comment songs hash: %w", err)
	}
	return h, nil
}

// SaveCommentSongs 寫入分析後的 comment_songs 與其來源 hash（只動這兩欄）
func (r *StreamRepository) SaveCommentSongs(id string, songs []byte, hash string) error {
	songs = util.SanitizeJSONB(songs)
	_, err := r.db.Exec(`UPDATE streams SET comment_songs = $2, comment_songs_hash = $3, updated_at = NOW() WHERE id = $1`, id, songs, hash)
	if err != nil {
		return fmt.Errorf("save comment songs: %w", err)
	}
	return nil
}

// GetHolodexSongsCache 取得快取的 Holodex 正規化結果與其來源 holodex_hash
func (r *StreamRepository) GetHolodexSongsCache(id string) (normalized []byte, hash sql.NullString, err error) {
	var n []byte
	var h sql.NullString
	err = r.db.QueryRow(`SELECT holodex_songs_normalized, holodex_songs_hash FROM streams WHERE id = $1`, id).Scan(&n, &h)
	if err == sql.ErrNoRows {
		return nil, sql.NullString{}, nil
	}
	if err != nil {
		return nil, sql.NullString{}, fmt.Errorf("get holodex songs cache: %w", err)
	}
	return n, h, nil
}

// SaveHolodexSongs 寫入快取的 Holodex 正規化結果與其來源 holodex_hash（只動這兩欄）
func (r *StreamRepository) SaveHolodexSongs(id string, normalized []byte, hash string) error {
	normalized = util.SanitizeJSONB(normalized)
	_, err := r.db.Exec(`UPDATE streams SET holodex_songs_normalized = $2, holodex_songs_hash = $3, updated_at = NOW() WHERE id = $1`, id, normalized, hash)
	if err != nil {
		return fmt.Errorf("save holodex songs: %w", err)
	}
	return nil
}

// CheckHashChanged 檢查 Holodex 資料是否有變更
func (r *StreamRepository) CheckHashChanged(id, newHash string) (bool, error) {
	var currentHash sql.NullString
	err := r.db.QueryRow("SELECT holodex_hash FROM streams WHERE id = $1", id).Scan(&currentHash)
	if err == sql.ErrNoRows {
		return true, nil // 新資料
	}
	if err != nil {
		return false, fmt.Errorf("check hash: %w", err)
	}

	if !currentHash.Valid {
		return true, nil
	}

	return currentHash.String != newHash, nil
}

// ========== Stream Singers ==========

// GetSingers 取得參與此直播的所有歌手
func (r *StreamRepository) GetSingers(streamID string) ([]models.Singer, error) {
	query := `
		SELECT s.id, s.name, s.english_name, s.photo_url, s.organization, s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		JOIN stream_singers ss ON s.id = ss.singer_id
		WHERE ss.stream_id = $1`

	rows, err := r.db.Query(query, streamID)
	if err != nil {
		return nil, fmt.Errorf("get stream singers: %w", err)
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

// GetChannelOwner 取得此直播的頻道擁有者
func (r *StreamRepository) GetChannelOwner(streamID string) (*models.Singer, error) {
	query := `
		SELECT s.id, s.name, s.english_name, s.photo_url, s.organization, s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		JOIN stream_singers ss ON s.id = ss.singer_id
		WHERE ss.stream_id = $1 AND ss.is_owner = TRUE
		LIMIT 1`

	var s models.Singer
	err := r.db.QueryRow(query, streamID).Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL, &s.Organization, &s.MetadataSource, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil // 沒有設定頻道擁有者
	}
	if err != nil {
		return nil, fmt.Errorf("get channel owner: %w", err)
	}
	return &s, nil
}

// AddSinger 為直播添加參與者
func (r *StreamRepository) AddSinger(streamID, singerID string, isOwner bool) error {
	query := `INSERT INTO stream_singers (stream_id, singer_id, is_owner) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(query, streamID, singerID, isOwner)
	if err != nil {
		return fmt.Errorf("add stream singer: %w", err)
	}
	return nil
}

// RemoveSinger 移除直播參與者
func (r *StreamRepository) RemoveSinger(streamID, singerID string) error {
	_, err := r.db.Exec("DELETE FROM stream_singers WHERE stream_id = $1 AND singer_id = $2", streamID, singerID)
	if err != nil {
		return fmt.Errorf("remove stream singer: %w", err)
	}
	return nil
}

// SetSingers 設定直播的參與者（覆蓋現有的）
func (r *StreamRepository) SetSingers(streamID string, singerIDs []string, ownerID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 刪除現有的參與者
	_, err = tx.Exec("DELETE FROM stream_singers WHERE stream_id = $1", streamID)
	if err != nil {
		return fmt.Errorf("delete existing singers: %w", err)
	}

	// 插入新的參與者
	for _, singerID := range singerIDs {
		isOwner := singerID == ownerID
		_, err = tx.Exec(
			"INSERT INTO stream_singers (stream_id, singer_id, is_owner) VALUES ($1, $2, $3)",
			streamID, singerID, isOwner,
		)
		if err != nil {
			return fmt.Errorf("insert singer: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ClearTags 清除直播的所有標籤
func (r *StreamRepository) ClearTags(streamID string) error {
	_, err := r.db.Exec("DELETE FROM stream_stream_tags WHERE stream_id = $1", streamID)
	if err != nil {
		return fmt.Errorf("clear stream tags: %w", err)
	}
	return nil
}

// SetTags 設定直播的標籤（覆蓋現有的）
func (r *StreamRepository) SetTags(streamID string, tagIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 刪除現有的標籤
	_, err = tx.Exec("DELETE FROM stream_stream_tags WHERE stream_id = $1", streamID)
	if err != nil {
		return fmt.Errorf("delete existing tags: %w", err)
	}

	// 插入新的標籤
	for _, tagID := range tagIDs {
		_, err = tx.Exec(
			"INSERT INTO stream_stream_tags (stream_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			streamID, tagID,
		)
		if err != nil {
			return fmt.Errorf("insert tag: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// StreamFilter 用於篩選歌回的選項
type StreamFilter struct {
	ProcessedOnly *bool // nil=全部, true=只看已處理, false=只看未處理
	HiddenFilter  *bool // nil=全部, true=只看隱藏, false=不顯示隱藏（預設）
}

// FindBySingerID 取得演唱者參與的歌回（支援分頁和篩選）
func (r *StreamRepository) FindBySingerID(singerID string, limit, offset int, filter *StreamFilter) ([]models.Stream, int, error) {
	// 建構 WHERE 條件
	whereClause := "ss.singer_id = $1"
	args := []interface{}{singerID}
	argIndex := 2

	if filter != nil {
		if filter.ProcessedOnly != nil {
			whereClause += fmt.Sprintf(" AND s.is_processed = $%d", argIndex)
			args = append(args, *filter.ProcessedOnly)
			argIndex++
		}
		if filter.HiddenFilter != nil {
			if *filter.HiddenFilter {
				// 只看隱藏的
				whereClause += fmt.Sprintf(" AND s.is_hidden = $%d", argIndex)
				args = append(args, true)
			} else {
				// 不顯示隱藏的
				whereClause += fmt.Sprintf(" AND s.is_hidden = $%d", argIndex)
				args = append(args, false)
			}
			argIndex++
		}
	}

	var total int
	countQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT s.id)
		FROM streams s
		JOIN stream_singers ss ON s.id = ss.stream_id
		WHERE %s
	`, whereClause)
	err := r.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count streams: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT s.id, s.title, s.stream_date, s.duration_seconds, s.thumbnail_url, s.holodex_data, s.holodex_hash, s.is_processed, s.is_hidden, s.created_at, s.updated_at
		FROM streams s
		JOIN stream_singers ss ON s.id = ss.stream_id
		WHERE %s
		ORDER BY s.stream_date DESC
		LIMIT $%d OFFSET $%d`, whereClause, argIndex, argIndex+1)

	args = append(args, limit, offset)
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		err := rows.Scan(&s.ID, &s.Title, &s.StreamDate, &s.DurationSeconds,
			&s.ThumbnailURL, &s.HolodexData, &s.HolodexHash, &s.IsProcessed, &s.IsHidden, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}

	return streams, total, nil
}
