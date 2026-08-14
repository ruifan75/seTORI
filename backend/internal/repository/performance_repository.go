package repository

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
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
	StreamTitle    string                   `json:"stream_title"`
	StreamDate     string                   `json:"stream_date"`
	ThumbnailURL   sql.NullString           `json:"thumbnail_url"`
	SongName       string                   `json:"song_name"`
	OriginalArtist string                   `json:"original_artist"`
	Artists        []models.ArtistReference `json:"artists"`
	Arts           sql.NullString           `json:"arts"`
	Tags           []models.PerformanceTag  `json:"tags"`
	Singers        []models.Singer          `json:"singers"`
}

// attachArtistReferences は演唱一覧に song_artists の安定した UUID 参照を一括で付与する。
func (r *PerformanceRepository) attachArtistReferences(performances []PerformanceWithDetails) error {
	if len(performances) == 0 {
		return nil
	}

	seen := make(map[uuid.UUID]struct{}, len(performances))
	ids := make([]string, 0, len(performances))
	for _, perf := range performances {
		if _, ok := seen[perf.SongID]; ok {
			continue
		}
		seen[perf.SongID] = struct{}{}
		ids = append(ids, perf.SongID.String())
	}

	rows, err := r.db.Query(`
		SELECT sa.song_id, a.id, a.name
		FROM song_artists sa
		JOIN artists a ON a.id = sa.artist_id
		WHERE sa.song_id = ANY($1::uuid[])
		ORDER BY sa.song_id, a.name`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("get performance artist references: %w", err)
	}
	defer rows.Close()

	bySong := make(map[uuid.UUID][]models.ArtistReference, len(ids))
	for rows.Next() {
		var songID uuid.UUID
		var artist models.ArtistReference
		if err := rows.Scan(&songID, &artist.ID, &artist.Name); err != nil {
			return fmt.Errorf("scan performance artist reference: %w", err)
		}
		bySong[songID] = append(bySong[songID], artist)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range performances {
		performances[i].Artists = bySong[performances[i].SongID]
	}
	return nil
}

// FindByStreamID 根據 Stream ID 取得所有演出（用於歌回詳情頁）
func (r *PerformanceRepository) FindByStreamID(streamID string) ([]PerformanceWithDetails, error) {
	query := `
		SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index,
		       p.holodex_song_id, p.custom_tags, p.created_at, p.end_source, p.end_confirmed,
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
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt, &p.EndSource, &p.EndConfirmed, &p.SongName, &p.OriginalArtist, &p.Arts)
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

	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachArtistReferences(performances); err != nil {
		return nil, err
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
		       p.holodex_song_id, p.custom_tags, p.created_at, p.end_source, p.end_confirmed,
		       st.title AS stream_title, st.stream_date, st.thumbnail_url,
		       s.name AS song_name, s.original_artist, s.arts
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		JOIN songs s ON p.song_id = s.id
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
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt, &p.EndSource, &p.EndConfirmed,
			&p.StreamTitle, &p.StreamDate, &p.ThumbnailURL,
			&p.SongName, &p.OriginalArtist, &p.Arts)
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

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := r.attachArtistReferences(performances); err != nil {
		return nil, 0, err
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
		       p.holodex_song_id, p.custom_tags, p.created_at, p.end_source, p.end_confirmed,
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
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt, &p.EndSource, &p.EndConfirmed,
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

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := r.attachArtistReferences(performances); err != nil {
		return nil, 0, err
	}
	return performances, total, nil
}

// Create 建立新演出
func (r *PerformanceRepository) Create(p *models.Performance) error {
	p.ID = uuid.New()
	query := `
		INSERT INTO performances (id, stream_id, song_id, start_seconds, end_seconds, order_index, holodex_song_id, custom_tags, end_source, end_confirmed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at`

	err := r.db.QueryRow(query, p.ID, p.StreamID, p.SongID, p.StartSeconds, p.EndSeconds,
		p.OrderIndex, p.HolodexSongID, p.CustomTags, normalizeEndSource(p.EndSource), p.EndConfirmed).Scan(&p.CreatedAt)
	if err != nil {
		return fmt.Errorf("create performance: %w", err)
	}
	return nil
}

// FindByID は歌唱1件を配信・楽曲情報付きで取得する。見つからなければ nil。
// perfDetailSelect を使うため、非表示配信の歌唱も返る（編集・提案の対象になりうるため）。
func (r *PerformanceRepository) FindByID(id uuid.UUID) (*PerformanceWithDetails, error) {
	perfs, err := r.queryPerformanceDetails(perfDetailSelect+` WHERE p.id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(perfs) == 0 {
		return nil, nil
	}
	return &perfs[0], nil
}

// FindOverlapping は配信内で [start, end) と時間が重なる歌唱を返す。
// end = 0 は「動画の最後まで」なので終端なしとして扱う。
// 未登録曲の追加提案をレビューするとき、既に登録済みの曲を指していないか気づくために使う。
func (r *PerformanceRepository) FindOverlapping(streamID string, start, end int, excludeID uuid.UUID) ([]PerformanceWithDetails, error) {
	// 区間の重なり判定：既存の開始 < 提案の終了 && 提案の開始 < 既存の終了
	// 0（終端なし）は非常に大きい値として扱う
	const openEnd = 1 << 30
	proposedEnd := end
	if proposedEnd == 0 {
		proposedEnd = openEnd
	}
	return r.queryPerformanceDetails(perfDetailSelect+`
		WHERE p.stream_id = $1
		  AND p.id <> $4
		  AND p.start_seconds < $3
		  AND $2 < (CASE WHEN p.end_seconds = 0 THEN `+fmt.Sprint(openEnd)+` ELSE p.end_seconds END)
		ORDER BY p.start_seconds`, streamID, start, proposedEnd, excludeID)
}

// Update 既存の演出を ID を保ったまま更新する。
// ID を維持することが重要：プレイリスト項目が performance_id を参照しているため、
// 編集のたびに ID が変わると利用者のプレイリストから曲が消えてしまう。
//
// **batch_run_id を外す。** 一括が作った歌唱を人が直したなら、それはもう人のもの。
// 印を残したままにすると、その実行を撤回したときに人の編集ごと消える。
// created_via / match_confidence は残す（「元は AI が作った行」という履歴は消さない）。
func (r *PerformanceRepository) Update(p *models.Performance) error {
	query := `
		UPDATE performances
		SET stream_id = $2, song_id = $3, start_seconds = $4, end_seconds = $5,
		    order_index = $6, holodex_song_id = $7, custom_tags = $8,
		    end_source = $9, end_confirmed = $10, batch_run_id = NULL
		WHERE id = $1`

	res, err := r.db.Exec(query, p.ID, p.StreamID, p.SongID, p.StartSeconds, p.EndSeconds,
		p.OrderIndex, p.HolodexSongID, p.CustomTags,
		normalizeEndSource(p.EndSource), p.EndConfirmed)
	if err != nil {
		return fmt.Errorf("update performance: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("update performance: not found: %s", p.ID)
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
		SELECT s.id, s.name, s.english_name, s.photo_url, COALESCE(s.organization_override, s.organization), o.display_name, COALESCE(o.is_unaffiliated, FALSE), s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		LEFT JOIN organizations o ON COALESCE(s.organization_override, s.organization) = o.key
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
		err := rows.Scan(&s.ID, &s.Name, &s.EnglishName, &s.PhotoURL, &s.Organization, &s.OrganizationName, &s.OrganizationUnaffil, &s.MetadataSource, &s.CreatedAt, &s.UpdatedAt)
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

// ReconcilePerformances は stream の演出を desired の状態へ差分更新し、
// desired と同じ並びの performance ID を返す（呼び出し側がタグ・歌手の設定に使う）。
//
// 全削除→再作成をしないのは ID を保つため。プレイリスト項目が performance_id を
// 参照するので、編集のたびに ID が変わると利用者のプレイリストから曲が消える。
// 既存との対応付けは (song_id, start_seconds) の完全一致を優先し、余った分は
// 同一 song_id で開始秒が最も近いものに割り当てる（開始時間の微調整で ID を失わないため）。
//
// UNIQUE(stream_id, song_id, start_seconds) があるため、開始秒を入れ替えるような
// 編集では更新途中に一時的な衝突が起きうる。これを避けるため、更新対象をいったん
// 負の開始秒へ退避してから最終値を書く。全体を1トランザクションで行う。
func (r *PerformanceRepository) ReconcilePerformances(streamID string, desired []models.Performance) ([]uuid.UUID, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	type existingPerf struct {
		id     uuid.UUID
		songID uuid.UUID
		start  int
	}

	rows, err := tx.Query("SELECT id, song_id, start_seconds FROM performances WHERE stream_id = $1", streamID)
	if err != nil {
		return nil, fmt.Errorf("query existing performances: %w", err)
	}
	var existing []existingPerf
	for rows.Next() {
		var e existingPerf
		if err := rows.Scan(&e.id, &e.songID, &e.start); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan existing performance: %w", err)
		}
		existing = append(existing, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing performances: %w", err)
	}

	// 対応付け：desired の各要素へ既存 ID を割り当てる（未割り当ては uuid.Nil）
	assigned := make([]uuid.UUID, len(desired))
	used := make(map[uuid.UUID]bool, len(existing))

	// 1巡目：(song_id, start_seconds) 完全一致
	for i, d := range desired {
		for _, e := range existing {
			if !used[e.id] && e.songID == d.SongID && e.start == d.StartSeconds {
				assigned[i] = e.id
				used[e.id] = true
				break
			}
		}
	}
	// 2巡目：同一 song_id のうち開始秒が最も近いもの（開始時間の微調整を吸収）
	for i, d := range desired {
		if assigned[i] != uuid.Nil {
			continue
		}
		best, bestDiff := uuid.Nil, 0
		for _, e := range existing {
			if used[e.id] || e.songID != d.SongID {
				continue
			}
			diff := e.start - d.StartSeconds
			if diff < 0 {
				diff = -diff
			}
			if best == uuid.Nil || diff < bestDiff {
				best, bestDiff = e.id, diff
			}
		}
		if best != uuid.Nil {
			assigned[i] = best
			used[best] = true
		}
	}

	// 対応の付かなかった既存は削除（関連も含めて）
	for _, e := range existing {
		if used[e.id] {
			continue
		}
		for _, q := range []string{
			"DELETE FROM performance_performance_tags WHERE performance_id = $1",
			"DELETE FROM performance_singers WHERE performance_id = $1",
			"DELETE FROM performances WHERE id = $1",
		} {
			if _, err := tx.Exec(q, e.id); err != nil {
				return nil, fmt.Errorf("delete obsolete performance: %w", err)
			}
		}
	}

	// 維持する行を一時的に負の開始秒へ退避（UNIQUE 制約の一時衝突を回避）
	park := 0
	for _, id := range assigned {
		if id == uuid.Nil {
			continue
		}
		park++
		if _, err := tx.Exec("UPDATE performances SET start_seconds = $2 WHERE id = $1", id, -park); err != nil {
			return nil, fmt.Errorf("park performance: %w", err)
		}
	}

	// 最終値を書き込む（既存は更新、無ければ新規作成）
	result := make([]uuid.UUID, len(desired))
	for i := range desired {
		d := desired[i]
		if id := assigned[i]; id != uuid.Nil {
			// 既存行の更新＝人が編集画面から保存した経路（一括は歌唱が無い配信にしか書かない）。
			// batch_run_id を外して「もう人のもの」にする ── 残すと、その実行を
			// 撤回したときに人が直した行まで消える。
			_, err := tx.Exec(`
				UPDATE performances
				SET song_id = $2, start_seconds = $3, end_seconds = $4,
				    order_index = $5, holodex_song_id = $6, custom_tags = $7,
				    end_source = $8, end_confirmed = $9, batch_run_id = NULL
				WHERE id = $1`,
				id, d.SongID, d.StartSeconds, d.EndSeconds, d.OrderIndex, d.HolodexSongID, d.CustomTags,
				normalizeEndSource(d.EndSource), d.EndConfirmed)
			if err != nil {
				return nil, fmt.Errorf("update performance: %w", err)
			}
			result[i] = id
			continue
		}
		newID := uuid.New()
		_, err := tx.Exec(`
			INSERT INTO performances (id, stream_id, song_id, start_seconds, end_seconds, order_index, holodex_song_id, custom_tags, end_source, end_confirmed)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			newID, streamID, d.SongID, d.StartSeconds, d.EndSeconds, d.OrderIndex, d.HolodexSongID, d.CustomTags,
			normalizeEndSource(d.EndSource), d.EndConfirmed)
		if err != nil {
			return nil, fmt.Errorf("insert performance: %w", err)
		}
		result[i] = newID
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reconcile: %w", err)
	}
	return result, nil
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
		       p.holodex_song_id, p.custom_tags, p.created_at, p.end_source, p.end_confirmed,
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
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt, &p.EndSource, &p.EndConfirmed,
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

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := r.attachArtistReferences(performances); err != nil {
		return nil, 0, err
	}
	return performances, total, nil
}

// ========== 首頁：ランダム再生 ==========

// perfDetailSelect は配信・楽曲情報付きで歌唱を引く共通 SELECT（FindByTagID と同形）。
const perfDetailSelect = `
	SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index,
	       p.holodex_song_id, p.custom_tags, p.created_at, p.end_source, p.end_confirmed,
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
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt, &p.EndSource, &p.EndConfirmed,
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

	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachArtistReferences(performances); err != nil {
		return nil, err
	}
	return performances, nil
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

// EndSource の取りうる値。migration 030 の CHECK 制約と一致させること。
const (
	EndSourceManual    = "manual"     // 人が編集画面で入力・変更した
	EndSourceHolodex   = "holodex"    // Holodex 提供
	EndSourceComment   = "comment"    // コメントに明示されていた
	EndSourceChat      = "chat"       // live chat の拍手検出
	EndSourceItunes    = "itunes"     // iTunes の再生時間から逆算
	EndSourceNextStart = "next_start" // 次の曲の開始時間
	EndSourceDefault   = "default"    // 240 秒の既定値
	EndSourceUnknown   = "unknown"    // 由来を記録する前に作られた行
)

var validEndSources = map[string]bool{
	EndSourceManual: true, EndSourceHolodex: true, EndSourceComment: true,
	EndSourceChat: true, EndSourceItunes: true, EndSourceNextStart: true,
	EndSourceDefault: true, EndSourceUnknown: true,
}

// normalizeEndSource は語彙外の値を unknown に丸める。
//
// DB 側の CHECK 制約に任せると、未知の値が来たときに保存そのものが失敗し、
// 「由来が分からない」だけで済むはずの話が「歌唱記録を保存できない」になってしまう。
// 由来はあくまで補助情報なので、ここで吸収して本体の保存を守る。
func normalizeEndSource(s string) string {
	if validEndSources[s] {
		return s
	}
	return EndSourceUnknown
}
