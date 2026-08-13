package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/pkg/util"
)

type StreamRepository struct {
	db *sql.DB
}

func NewStreamRepository(db *sql.DB) *StreamRepository {
	return &StreamRepository{db: db}
}

// FindAll 取得所有歌回（支援分頁，預設不顯示隱藏的）
func (r *StreamRepository) FindAll(limit, offset int, includeHidden bool, sort, dir string) ([]models.Stream, int, error) {
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

	// 既定は配信日の新しい順。"title" 指定でタイトルの五十音順。
	order := "stream_date " + dirOr(dir, "desc")
	if sort == "title" {
		order = nameSortOrderDir("title", "''", dir)
	}

	var query string
	if includeHidden {
		query = `
			SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, is_processed, is_hidden, created_at, updated_at
			FROM streams
			ORDER BY ` + order + `
			LIMIT $1 OFFSET $2`
	} else {
		query = `
			SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, is_processed, is_hidden, created_at, updated_at
			FROM streams
			WHERE is_hidden = FALSE
			ORDER BY ` + order + `
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
		SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, comment_songs_analyzed_at, is_processed, is_hidden, created_at, updated_at
		FROM streams WHERE id = $1`

	var s models.Stream
	err := r.db.QueryRow(query, id).Scan(
		&s.ID, &s.Title, &s.StreamDate, &s.DurationSeconds,
		&s.ThumbnailURL, &s.HolodexData, &s.HolodexHash, &s.CommentRaw, &s.CommentSongs, &s.CommentSongsAnalyzedAt, &s.IsProcessed, &s.IsHidden, &s.CreatedAt, &s.UpdatedAt)
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

// CommentHashRow は comment_songs_hash 補正用の最小行（id + 現在の comment_raw + 保存済み hash）。
type CommentHashRow struct {
	ID         string
	CommentRaw []byte
	Hash       sql.NullString
}

// FindCommentHashRows は comment_songs を持つ全歌回の (id, comment_raw, comment_songs_hash) を返す。
// comment_songs_hash のアルゴリズム移行（旧: 生bytes sha → 新: 正規化 sha）補正に使う。
func (r *StreamRepository) FindCommentHashRows() ([]CommentHashRow, error) {
	rows, err := r.db.Query(`
		SELECT id, comment_raw, comment_songs_hash FROM streams
		WHERE comment_songs IS NOT NULL AND comment_songs::text NOT IN ('null', '[]')`)
	if err != nil {
		return nil, fmt.Errorf("query comment hash rows: %w", err)
	}
	defer rows.Close()

	var out []CommentHashRow
	for rows.Next() {
		var row CommentHashRow
		if err := rows.Scan(&row.ID, &row.CommentRaw, &row.Hash); err != nil {
			return nil, fmt.Errorf("scan comment hash row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateCommentSongsHash は comment_songs_hash のみを書き換える（comment_songs / comment_raw は不変）。
func (r *StreamRepository) UpdateCommentSongsHash(id, hash string) error {
	_, err := r.db.Exec(`UPDATE streams SET comment_songs_hash = $2 WHERE id = $1`, id, hash)
	if err != nil {
		return fmt.Errorf("update comment_songs_hash: %w", err)
	}
	return nil
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
		SELECT ss.stream_id, ss.is_owner, s.id, s.name, s.english_name, s.photo_url, COALESCE(s.organization_override, s.organization), o.display_name, COALESCE(o.is_unaffiliated, FALSE), s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		LEFT JOIN organizations o ON COALESCE(s.organization_override, s.organization) = o.key
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
			&sg.PhotoURL, &sg.Organization, &sg.OrganizationName, &sg.OrganizationUnaffil, &sg.MetadataSource, &sg.CreatedAt, &sg.UpdatedAt); err != nil {
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

// SetInitialVisibility は新規配信のタグ付け完了後に、初回の表示判定を確定する。
// 呼び出し側は既存配信には使わない。以後の表示状態は手動編集だけが変更する。
func (r *StreamRepository) SetInitialVisibility(streamID string, hidden bool) error {
	_, err := r.db.Exec(`
		UPDATE streams
		SET is_hidden = $2, updated_at = NOW()
		WHERE id = $1
		  AND is_hidden IS DISTINCT FROM $2`, streamID, hidden)
	if err != nil {
		return fmt.Errorf("set initial stream visibility: %w", err)
	}
	return nil
}

// FindByTagID は指定タグが付いた配信を新しい順で返す（タグ検索用、非表示は除外）。
func (r *StreamRepository) FindByTagID(tagID string, limit, offset int) ([]models.Stream, int, error) {
	var total int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM streams s
		JOIN stream_stream_tags sst ON sst.stream_id = s.id
		WHERE sst.tag_id = $1 AND s.is_hidden = FALSE`, tagID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count streams by tag: %w", err)
	}

	rows, err := r.db.Query(`
		SELECT s.id, s.title, s.stream_date, s.duration_seconds, s.thumbnail_url, s.holodex_data, s.holodex_hash, s.comment_raw, s.comment_songs, s.is_processed, s.is_hidden, s.created_at, s.updated_at
		FROM streams s
		JOIN stream_stream_tags sst ON sst.stream_id = s.id
		WHERE sst.tag_id = $1 AND s.is_hidden = FALSE
		ORDER BY s.stream_date DESC
		LIMIT $2 OFFSET $3`, tagID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query streams by tag: %w", err)
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
	return streams, total, rows.Err()
}

// FindStreamsForBatch は一括分析の対象配信（id/title のみ）を mode と（任意の）歌手で
// 絞り込んで古い順に返す。singerID が空なら全チャンネルが対象。
//
// mode 別の対象範囲（いずれも非隠し・comment_raw あり）:
//   - "unanalyzed"           : 分析結果（comment_songs）が一度も無い配信のみ
//   - "unprocessed"/"refresh": 未処理（ユーザー未確認）の配信すべて
//   - "reanalyze"            : comment_raw を持つ配信すべて（分析済みも対象。force で作り直す）
//
// singerID 指定時は stream_singers を EXISTS で絞る（owner / 参加者どちらでも一致）。
func (r *StreamRepository) FindStreamsForBatch(mode, singerID string) ([]models.Stream, error) {
	where := "is_hidden = FALSE AND comment_raw IS NOT NULL AND comment_raw != 'null'"
	switch mode {
	case "unanalyzed":
		where += " AND (comment_songs IS NULL OR comment_songs = 'null')"
	case "unprocessed", "refresh":
		where += " AND is_processed = FALSE"
	case "reanalyze":
		// 追加条件なし：comment_raw を持つ配信すべてを再分析対象にする
	default:
		return nil, fmt.Errorf("unknown batch mode: %s", mode)
	}

	args := []any{}
	if singerID != "" {
		args = append(args, singerID)
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = $%d)", len(args))
	}

	rows, err := r.db.Query(`SELECT s.id, s.title FROM streams s WHERE `+where+` ORDER BY s.stream_date ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query batch streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		if err := rows.Scan(&s.ID, &s.Title); err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}
	return streams, rows.Err()
}

// SearchStreams は配信元・参加者・ボーカル・タグを AND で組み合わせて検索する。
// StreamTagIDs / PerformanceTagIDs の各配列内も AND 条件。
// 検索は明示的な操作なので、非表示の配信も対象に含める。
func (r *StreamRepository) SearchStreams(filters models.StreamSearchFilters, limit, offset int) ([]models.Stream, int, error) {
	where := "WHERE TRUE"
	args := []any{}
	i := 1
	if filters.Query != "" {
		where += fmt.Sprintf(" AND s.title ILIKE '%%' || $%d || '%%'", i)
		args = append(args, filters.Query)
		i++
	}
	if filters.OwnerID != "" {
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = $%d AND ss.is_owner = TRUE)", i)
		args = append(args, filters.OwnerID)
		i++
	}
	if len(filters.ParticipantIDs) > 0 {
		where += fmt.Sprintf(" AND (SELECT COUNT(DISTINCT ss.singer_id) FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = ANY($%d)) = %d", i, len(filters.ParticipantIDs))
		args = append(args, pq.Array(filters.ParticipantIDs))
		i++
	}
	if len(filters.VocalistIDs) > 0 {
		where += fmt.Sprintf(" AND (SELECT COUNT(DISTINCT ps.singer_id) FROM performances p JOIN performance_singers ps ON ps.performance_id = p.id WHERE p.stream_id = s.id AND ps.singer_id = ANY($%d)) = %d", i, len(filters.VocalistIDs))
		args = append(args, pq.Array(filters.VocalistIDs))
		i++
	}
	if len(filters.StreamTagIDs) > 0 {
		where += fmt.Sprintf(" AND (SELECT COUNT(DISTINCT sst.tag_id) FROM stream_stream_tags sst WHERE sst.stream_id = s.id AND sst.tag_id = ANY($%d)) = %d", i, len(filters.StreamTagIDs))
		args = append(args, pq.Array(filters.StreamTagIDs))
		i++
	}
	if len(filters.PerformanceTagIDs) > 0 {
		where += fmt.Sprintf(" AND (SELECT COUNT(DISTINCT ppt.tag_id) FROM performances p JOIN performance_performance_tags ppt ON ppt.performance_id = p.id WHERE p.stream_id = s.id AND ppt.tag_id = ANY($%d)) = %d", i, len(filters.PerformanceTagIDs))
		args = append(args, pq.Array(filters.PerformanceTagIDs))
		i++
	}

	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM streams s "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count searched streams: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT s.id, s.title, s.stream_date, s.duration_seconds, s.thumbnail_url, s.holodex_data, s.holodex_hash, s.comment_raw, s.comment_songs, s.is_processed, s.is_hidden, s.created_at, s.updated_at
		FROM streams s
		%s
		ORDER BY s.stream_date DESC
		LIMIT $%d OFFSET $%d`, where, i, i+1)
	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search streams: %w", err)
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
	return streams, total, rows.Err()
}

// SearchByTitle はタイトル部分一致（大小無視）で配信を検索する（グローバル検索用）。
// 非表示の配信も対象に含める（検索は明示的な操作のため）。
func (r *StreamRepository) SearchByTitle(query string, limit int) ([]models.Stream, error) {
	rows, err := r.db.Query(`
		SELECT id, title, stream_date, thumbnail_url, is_processed, is_hidden
		FROM streams
		WHERE title ILIKE '%' || $1 || '%'
		ORDER BY stream_date DESC
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search streams by title: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		if err := rows.Scan(&s.ID, &s.Title, &s.StreamDate, &s.ThumbnailURL, &s.IsProcessed, &s.IsHidden); err != nil {
			return nil, fmt.Errorf("scan stream search result: %w", err)
		}
		streams = append(streams, s)
	}
	return streams, rows.Err()
}

// applyTagRulesSQL は tag_keyword_rules を配信タイトルへ ILIKE 部分一致で照合し、
// 一致したタグを stream_stream_tags に「追加」する（既存タグは消さない・冪等）。
const applyTagRulesSQL = `
	INSERT INTO stream_stream_tags (stream_id, tag_id)
	SELECT s.id, r.tag_id
	FROM streams s
	JOIN tag_keyword_rules r ON s.title ILIKE '%' || r.keyword || '%'`

// ApplyTagRulesToStream 單一配信套用自動標籤規則，回傳新增的標籤數。
func (r *StreamRepository) ApplyTagRulesToStream(streamID string) (int64, error) {
	res, err := r.db.Exec(applyTagRulesSQL+`
		WHERE s.id = $1
		ON CONFLICT (stream_id, tag_id) DO NOTHING`, streamID)
	if err != nil {
		return 0, fmt.Errorf("apply tag rules to stream: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ApplyTagRulesToAll 全配信に自動標籤規則を套用し、新增的標籤總數を回傳（バックフィル用）。
func (r *StreamRepository) ApplyTagRulesToAll() (int64, error) {
	res, err := r.db.Exec(applyTagRulesSQL + `
		ON CONFLICT (stream_id, tag_id) DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("apply tag rules to all: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ========== 分析快取（comment / holodex 正規化結果，以來源 hash 為鍵） ==========

// SaveCommentRaw 更新 comment_raw；內容有變時一併清除由舊留言產生的分析快取。
func (r *StreamRepository) SaveCommentRaw(id string, raw []byte) error {
	raw = util.SanitizeJSONB(raw)
	_, err := r.db.Exec(`
		UPDATE streams
		SET comment_songs = CASE WHEN comment_raw IS DISTINCT FROM $2 THEN NULL ELSE comment_songs END,
		    comment_songs_hash = CASE WHEN comment_raw IS DISTINCT FROM $2 THEN NULL ELSE comment_songs_hash END,
		    comment_raw = $2,
		    updated_at = NOW()
		WHERE id = $1`, id, raw)
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
// UpdateCommentSongs は解析結果の欄だけを書き換える。hash は触らない。
//
// hash が指すのは「どの comment_raw から作ったか」なので、抽出をやり直していない
// 更新（拍手 end の付与など）では据え置くのが正しい。ここで動かすと次回の解析が
// キャッシュを外して AI を呼ぶ。
//
// 行まるごとの更新（旧 Update）を置き換えたもの。あちらは title / holodex_data /
// comment_raw / is_hidden まで巻き込んで書き戻すので、呼び出し側が完全な行を
// 持っていることが暗黙の前提になっていた。部分的に組み立てた model を渡すと
// comment_songs が黙って空になる ── 失敗してもエラーもログも出ない形だった。
func (r *StreamRepository) UpdateCommentSongs(id string, songs []byte) error {
	_, err := r.db.Exec(`UPDATE streams SET comment_songs = $2, updated_at = NOW() WHERE id = $1`,
		id, util.SanitizeJSONB(songs))
	if err != nil {
		return fmt.Errorf("update comment songs: %w", err)
	}
	return nil
}

func (r *StreamRepository) SaveCommentSongs(id string, songs []byte, hash string) error {
	songs = util.SanitizeJSONB(songs)
	// comment_songs_analyzed_at は**ここでだけ**刻む。updated_at では代用できない
	// （毎日回る Holodex 同期が全配信の updated_at を今日に押し上げる）。
	_, err := r.db.Exec(`UPDATE streams
		SET comment_songs = $2, comment_songs_hash = $3, comment_songs_analyzed_at = NOW(), updated_at = NOW()
		WHERE id = $1`, id, songs, hash)
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
		SELECT s.id, s.name, s.english_name, s.photo_url, COALESCE(s.organization_override, s.organization), o.display_name, COALESCE(o.is_unaffiliated, FALSE), s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		LEFT JOIN organizations o ON COALESCE(s.organization_override, s.organization) = o.key
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
		err := rows.Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL, &s.Organization, &s.OrganizationName, &s.OrganizationUnaffil, &s.MetadataSource, &s.CreatedAt, &s.UpdatedAt)
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
		SELECT s.id, s.name, s.english_name, s.photo_url, COALESCE(s.organization_override, s.organization), o.display_name, COALESCE(o.is_unaffiliated, FALSE), s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		LEFT JOIN organizations o ON COALESCE(s.organization_override, s.organization) = o.key
		JOIN stream_singers ss ON s.id = ss.singer_id
		WHERE ss.stream_id = $1 AND ss.is_owner = TRUE
		LIMIT 1`

	var s models.Singer
	err := r.db.QueryRow(query, streamID).Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL, &s.Organization, &s.OrganizationName, &s.OrganizationUnaffil, &s.MetadataSource, &s.CreatedAt, &s.UpdatedAt)
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

// FindStreamsForFill は一括セットリスト作成の対象を返す。
//
// 一括プレ分析（FindStreamsForBatch）とは対象が違う。あちらは comment_raw を持つ配信だが、
// こちらは**曲の源を持つ配信**（Holodex の曲か、解析済みのコメント）が対象になる。
//
//	unprocessed … まだ歌唱が 1 つも無い配信だけ。既にあるものは触らない
//	force       … 源を持つ配信すべて。既存と食い違う分は人の審査へ回す
func (r *StreamRepository) FindStreamsForFill(mode, singerID string) ([]models.Stream, error) {
	// jsonb_typeof で配列だけを見る。comment_songs には JSON のスカラー 'null' が入っている
	// 行があり、jsonb_array_length に直接渡すと「cannot get array length of a scalar」で落ちる。
	where := `s.is_hidden = FALSE AND (
		(jsonb_typeof(s.holodex_data->'songs') = 'array' AND jsonb_array_length(s.holodex_data->'songs') > 0)
		OR (jsonb_typeof(s.comment_songs) = 'array' AND jsonb_array_length(s.comment_songs) > 0))`
	if mode == "unprocessed" {
		where += " AND NOT EXISTS (SELECT 1 FROM performances p WHERE p.stream_id = s.id)"
	}

	args := []any{}
	if singerID != "" {
		args = append(args, singerID)
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = $%d)", len(args))
	}

	rows, err := r.db.Query(`SELECT s.id, s.title FROM streams s WHERE `+where+` ORDER BY s.stream_date ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query fill streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		if err := rows.Scan(&s.ID, &s.Title); err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}
		streams = append(streams, s)
	}
	return streams, rows.Err()
}
