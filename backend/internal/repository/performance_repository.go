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

// PerformanceWithDetails は歌唱と関連情報を含む構造。
type PerformanceWithDetails struct {
	models.Performance
	StreamTitle    string                   `json:"stream_title"`
	StreamDate     string                   `json:"stream_date"`
	ThumbnailURL   sql.NullString           `json:"thumbnail_url"`
	SongName       string                   `json:"song_name"`
	OriginalArtist string                   `json:"original_artist"`
	Artists        []models.ArtistReference `json:"artists"`
	Arts           sql.NullString           `json:"arts"`
	// ItunesID は「この曲に紐付いている primary な iTunes ID」。
	// **FindByStreamID でのみ埋まる**（編集画面が紐付けの有無を出すために要る）。
	// 一覧系まで JOIN を広げていないのは、そこでは使われないため。
	ItunesID sql.NullInt64           `json:"itunes_id"`
	Tags     []models.PerformanceTag `json:"tags"`
	Singers  []models.Singer         `json:"singers"`
}

// attachArtistReferences は歌唱一覧に song_artists の安定した UUID 参照を一括で付与する。
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

// attachTagsAndSingers は歌唱一覧にバージョンタグと歌手を一括で付ける。
//
// 1 件ずつ GetTags / GetSingers を呼ぶと 100 曲の一覧で 200 回問い合わせることになる。
// 本番機（1 vCPU）ではこれがそのまま応答時間に出るので、まとめて 2 回で引く。
// 空の場合に nil のままにするのは 1 件ずつのときと同じ（JSON では null になる）。
func (r *PerformanceRepository) attachTagsAndSingers(performances []PerformanceWithDetails) error {
	if len(performances) == 0 {
		return nil
	}

	ids := make([]string, len(performances))
	for i, perf := range performances {
		ids[i] = perf.ID.String()
	}

	tagRows, err := r.db.Query(`
		SELECT ppt.performance_id, pt.id, pt.display_name, pt.color, pt.created_at
		FROM performance_tags pt
		JOIN performance_performance_tags ppt ON pt.id = ppt.tag_id
		WHERE ppt.performance_id = ANY($1::uuid[])
		ORDER BY ppt.performance_id, pt.id`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("get performance tags: %w", err)
	}
	defer tagRows.Close()

	tagsByPerf := make(map[uuid.UUID][]models.PerformanceTag, len(performances))
	for tagRows.Next() {
		var perfID uuid.UUID
		var t models.PerformanceTag
		if err := tagRows.Scan(&perfID, &t.ID, &t.DisplayName, &t.Color, &t.CreatedAt); err != nil {
			return fmt.Errorf("scan performance tag: %w", err)
		}
		tagsByPerf[perfID] = append(tagsByPerf[perfID], t)
	}
	if err := tagRows.Err(); err != nil {
		return err
	}

	singerRows, err := r.db.Query(`
		SELECT ps.performance_id,
		       s.id, s.name, s.english_name, s.photo_url,
		       COALESCE(s.organization_override, s.organization), o.display_name,
		       COALESCE(o.is_unaffiliated, FALSE), s.metadata_source, s.created_at, s.updated_at
		FROM singers s
		LEFT JOIN organizations o ON COALESCE(s.organization_override, s.organization) = o.key
		JOIN performance_singers ps ON s.id = ps.singer_id
		WHERE ps.performance_id = ANY($1::uuid[])
		ORDER BY ps.performance_id, s.name`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("get performance singers: %w", err)
	}
	defer singerRows.Close()

	singersByPerf := make(map[uuid.UUID][]models.Singer, len(performances))
	for singerRows.Next() {
		var perfID uuid.UUID
		var s models.Singer
		if err := singerRows.Scan(&perfID, &s.ID, &s.Name, &s.EnglishName, &s.PhotoURL,
			&s.Organization, &s.OrganizationName, &s.OrganizationUnaffil,
			&s.MetadataSource, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return fmt.Errorf("scan singer: %w", err)
		}
		singersByPerf[perfID] = append(singersByPerf[perfID], s)
	}
	if err := singerRows.Err(); err != nil {
		return err
	}

	for i := range performances {
		performances[i].Tags = tagsByPerf[performances[i].ID]
		performances[i].Singers = singersByPerf[performances[i].ID]
	}
	return nil
}

// FindByStreamID は配信 ID に紐付くすべての歌唱を取得する（歌枠詳細ページ用）。
func (r *PerformanceRepository) FindByStreamID(streamID string, access ViewerAccess) ([]PerformanceWithDetails, error) {
	query := `
		SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index,
		       p.holodex_song_id, p.custom_tags, p.created_at, p.end_source, p.end_confirmed,
		       s.name AS song_name, s.original_artist, s.arts, si.itunes_id
		FROM performances p
		JOIN songs s ON p.song_id = s.id
		JOIN streams st ON st.id = p.stream_id
		LEFT JOIN song_itunes si ON si.song_id = s.id AND si.is_primary
		WHERE p.stream_id = $1` + access.restrictClause() + `
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
			&p.OrderIndex, &p.HolodexSongID, &p.CustomTags, &p.CreatedAt, &p.EndSource, &p.EndConfirmed, &p.SongName, &p.OriginalArtist, &p.Arts, &p.ItunesID)
		if err != nil {
			return nil, fmt.Errorf("scan performance: %w", err)
		}

		// タグを取得する
		tags, err := r.GetTags(p.ID)
		if err != nil {
			return nil, err
		}
		p.Tags = tags

		// 歌手を取得する
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

// FindBySongID は楽曲 ID に紐付くすべての歌唱を取得する（逆引きの中核機能）。
// 非表示でない配信の歌唱だけを表示する。
func (r *PerformanceRepository) FindBySongID(songID uuid.UUID, limit, offset int, access ViewerAccess) ([]PerformanceWithDetails, int, error) {
	var total int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		WHERE p.song_id = $1 AND st.is_hidden = FALSE`+access.restrictClause()+`
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
		WHERE p.song_id = $1 AND st.is_hidden = FALSE` + access.restrictClause() + `
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

		// タグを取得する
		tags, err := r.GetTags(p.ID)
		if err != nil {
			return nil, 0, err
		}
		p.Tags = tags

		// 歌手を取得する
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
func (r *PerformanceRepository) FindByTagID(tagID string, limit, offset int, access ViewerAccess) ([]PerformanceWithDetails, int, error) {
	var total int
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM performances p
		JOIN streams st ON p.stream_id = st.id
		JOIN performance_performance_tags ppt ON ppt.performance_id = p.id
		WHERE ppt.tag_id = $1 AND st.is_hidden = FALSE`+access.restrictClause()+`
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
		WHERE ppt.tag_id = $1 AND st.is_hidden = FALSE` + access.restrictClause() + `
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

// Create は新しい歌唱記録を作成する。
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
// perfDetailSelect を使うため、**非表示**配信の歌唱も返る（編集・提案の対象になりうるため）。
// **秘匿**された配信は access で落とす ── この端点は公開なので、既定に頼らない。
func (r *PerformanceRepository) FindByID(id uuid.UUID, access ViewerAccess) (*PerformanceWithDetails, error) {
	perfs, err := r.queryPerformanceDetails(perfDetailSelect+` WHERE p.id = $1`+access.restrictClause(), id)
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
// access を取るのは**10 個目の reader だから**。提案の重なり警告からここへ到達でき、
// その端点（POST /api/suggestions と GET /api/suggestions/mine）は**ログインだけで通る**。
// access が無かった頃は、権限の無い利用者が end_seconds=0 の提案を投げるだけで
// 秘匿された配信の曲名と時刻を overlaps として受け取れた（実測）。
func (r *PerformanceRepository) FindOverlapping(streamID string, start, end int, excludeID uuid.UUID, access ViewerAccess) ([]PerformanceWithDetails, error) {
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
		ORDER BY p.start_seconds`+access.restrictClause(), streamID, start, proposedEnd, excludeID)
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

// Delete は歌唱記録を削除する。
func (r *PerformanceRepository) Delete(id uuid.UUID) error {
	// 先に関連を削除する
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

// GetTags は歌唱に付いたすべてのタグを取得する。
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

// AddTag は歌唱にタグを追加する。
func (r *PerformanceRepository) AddTag(performanceID uuid.UUID, tagID string) error {
	query := `INSERT INTO performance_performance_tags (performance_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(query, performanceID, tagID)
	if err != nil {
		return fmt.Errorf("add performance tag: %w", err)
	}
	return nil
}

// RemoveTag は歌唱からタグを外す。
func (r *PerformanceRepository) RemoveTag(performanceID uuid.UUID, tagID string) error {
	_, err := r.db.Exec("DELETE FROM performance_performance_tags WHERE performance_id = $1 AND tag_id = $2", performanceID, tagID)
	if err != nil {
		return fmt.Errorf("remove performance tag: %w", err)
	}
	return nil
}

// GetValidTagIDs は有効なタグ ID を取得する（存在しないタグを除外）。
func (r *PerformanceRepository) GetValidTagIDs(tagIDs []string) ([]string, error) {
	if len(tagIDs) == 0 {
		return []string{}, nil
	}

	// 有効なタグ ID をすべて照会する
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

// SetTags は歌唱のタグをすべて設定する（先に削除してから追加し、無効なタグは自動で除外）。
func (r *PerformanceRepository) SetTags(performanceID uuid.UUID, tagIDs []string) error {
	_, err := r.db.Exec("DELETE FROM performance_performance_tags WHERE performance_id = $1", performanceID)
	if err != nil {
		return fmt.Errorf("clear performance tags: %w", err)
	}

	// 有効なタグだけを残す
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

// GetSingers は歌唱の歌手をすべて取得する。
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

// AddSinger は歌唱に歌手を追加する。
func (r *PerformanceRepository) AddSinger(performanceID uuid.UUID, singerID string) error {
	query := `INSERT INTO performance_singers (performance_id, singer_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(query, performanceID, singerID)
	if err != nil {
		return fmt.Errorf("add performance singer: %w", err)
	}
	return nil
}

// SetSingers は歌唱の歌手をすべて設定する。
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

// DeleteByStreamID は指定した配信の歌唱記録をすべて削除する。
func (r *PerformanceRepository) DeleteByStreamID(streamID string) error {
	// 先にすべての performance ID を取得する
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

	// 各歌唱の関連データを削除する
	for _, id := range ids {
		if err := r.Delete(id); err != nil {
			return err
		}
	}

	return nil
}

// FindBySingerID は歌手 ID に紐付くすべての歌唱を取得する（ページング対応）。
// 非表示でない配信の歌唱だけを表示する。
func (r *PerformanceRepository) FindBySingerID(singerID string, limit, offset int, sort, dir string, access ViewerAccess) ([]PerformanceWithDetails, int, error) {
	var total int
	err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT p.id)
		FROM performances p
		JOIN performance_singers ps ON p.id = ps.performance_id
		JOIN streams st ON p.stream_id = st.id
		WHERE ps.singer_id = $1 AND st.is_hidden = FALSE`+access.restrictClause()+`
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
		WHERE ps.singer_id = $1 AND st.is_hidden = FALSE` + access.restrictClause() + `
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

		// タグを取得する
		tags, err := r.GetTags(p.ID)
		if err != nil {
			return nil, 0, err
		}
		p.Tags = tags

		// 歌手を取得する
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

// ========== ホーム：ランダム再生 ==========

// perfDetailSelect は配信・楽曲情報付きで歌唱を引く共通 SELECT（FindByTagID と同形）。
// ViewerAccess は歌唱を読む側の立場。**秘匿された配信（streams.is_restricted）の中身を
// 返してよいか**を決める。
//
// 引数として必須にしてあるのは、**新しい読み取りを足した人が決めずには
// コンパイルできないようにする**ため（docs/STREAM_VISIBILITY.md）。
// 「共通の変換層で落とす」では足りない ── 歌唱を返すクエリはここだけで 9 つあり、
// SELECT を各自で書いているものが混ざっているうえ、DTO まで持っていくと
// **total とページングを計算した後**なので件数から存在が漏れる。
type ViewerAccess int

const (
	// PublicAccess … 未ログインを含む一般の閲覧者。秘匿された配信の歌唱は返さない。
	PublicAccess ViewerAccess = iota
	// EditorAccess … content:edit を持つ利用者。秘匿された配信も編集対象なので返す。
	EditorAccess
)

// NotRestricted は「実効的に秘匿されていない」を表す SQL 式を返す。
//
// **人の裁定が自動判定に勝つ。** restriction_override は NULL＝未裁定 /
// TRUE＝伏せる / FALSE＝公開してよい で、is_restricted（自動判定の候補）より優先する。
// 1 列で兼ねていた頃は、人が解除しても次の availability 取得で戻っていた
// （会限は chapters / live chat / backfill から繰り返し取り直される）。
//
// alias は streams のテーブル別名。**別名を引数にしているのは、歌唱を数える SQL が
// st / s / 別名なしと揃っていないため** ── 揃えるより、渡し忘れをコンパイルで
// 止めるほうが確実。
func NotRestricted(alias string) string {
	return "NOT COALESCE(" + alias + ".restriction_override, " + alias + ".is_restricted)"
}

// restrictClause は WHERE / JOIN の条件へ足す文字列を返す。
// **プレースホルダを含めない**（クエリごとに $N の番号が違うため、
// 番号を持ち込むと足す場所ごとにずれる）。
func (a ViewerAccess) restrictClause() string {
	if a == EditorAccess {
		return ""
	}
	return " AND " + NotRestricted("st")
}

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
		performances = append(performances, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.attachTagsAndSingers(performances); err != nil {
		return nil, err
	}
	if err := r.attachArtistReferences(performances); err != nil {
		return nil, err
	}
	return performances, nil
}

// FindRandom は曲単位で重複しないランダムな歌唱を返す。
// 非表示に加え、再生できない可能性が高い メン限・アーカイブなし の配信も除外する。
func (r *PerformanceRepository) FindRandom(limit int, excludedSongIDs []string, access ViewerAccess) ([]PerformanceWithDetails, error) {
	if excludedSongIDs == nil {
		excludedSongIDs = []string{}
	}
	query := `
		WITH random_per_song AS (
			SELECT DISTINCT ON (p.song_id) p.id
			FROM performances p
			JOIN streams st ON p.stream_id = st.id
			WHERE st.is_hidden = FALSE` + access.restrictClause() + `
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

// ========== プリセットプレイリスト ==========

// PresetFilter はプリセットプレイリストの抽出条件。
// 定義そのものは service.Presets にあり、ここは「条件をどう SQL にするか」だけを持つ。
type PresetFilter struct {
	SingerID    string   // このチャンネルが歌っている歌唱だけ（空なら問わない）
	IncludeTags []string // いずれかの配信タグを持つ配信の歌唱だけ（空なら問わない）
	ExcludeTags []string // いずれかの配信タグを持つ配信は除く
	MultiSinger bool     // その歌唱を 2 人以上で歌っている（コラボ）
}

// presetWhere は FindRandom と同じ可視性の除外（非表示・メン限・アーカイブなし）に
// PresetFilter の条件を重ねた WHERE 句。$1〜$4 を使う。
//
// 条件の有無を SQL 文字列の組み立てではなく引数で表しているのは、プリセットが
// 「歌手あり・タグなし」「タグあり・歌手あり」などの組み合わせで増えるため。
// 分岐で文を作ると、増えるたびに検証していない SQL が生まれる。
// **const ではなく関数**にしてある：秘匿の条件は読む側の立場で変わるため。
func presetWhere(access ViewerAccess) string {
	return `
	WHERE st.is_hidden = FALSE` + access.restrictClause() + `
	  AND NOT EXISTS (
		SELECT 1 FROM stream_stream_tags hid
		WHERE hid.stream_id = st.id AND hid.tag_id IN ('members_only', 'unarchived')
	  )
	  AND ($1 = '' OR EXISTS (
		SELECT 1 FROM performance_singers fs
		WHERE fs.performance_id = p.id AND fs.singer_id = $1
	  ))
	  AND (cardinality($2::text[]) = 0 OR EXISTS (
		SELECT 1 FROM stream_stream_tags inc
		WHERE inc.stream_id = st.id AND inc.tag_id = ANY($2::text[])
	  ))
	  AND NOT EXISTS (
		SELECT 1 FROM stream_stream_tags exc
		WHERE exc.stream_id = st.id AND exc.tag_id = ANY($3::text[])
	  )
	  AND (NOT $4 OR (
		SELECT count(*) FROM performance_singers ms WHERE ms.performance_id = p.id
	  ) > 1)`
}

// presetArgs は presetWhere へ渡す $1〜$4 を作る。
//
// nil のスライスを pq.Array に渡すと SQL 上は NULL になり、cardinality(NULL) が NULL、
// つまり「タグを問わない」はずの条件が全行を落とす。ここで空スライスに正規化しておく。
func presetArgs(f PresetFilter) []interface{} {
	include := f.IncludeTags
	if include == nil {
		include = []string{}
	}
	exclude := f.ExcludeTags
	if exclude == nil {
		exclude = []string{}
	}
	return []interface{}{f.SingerID, pq.Array(include), pq.Array(exclude), f.MultiSinger}
}

// presetOrder はプリセットの並び順。新しい配信から、配信内は歌った順。
//
// **必ず一意に決まるところまで指定すること。** order_index は 0 のままの歌唱が多く
// （コメント解析で作られた歌唱は順序を持たない）、そこで打ち止めにすると同着だらけになる。
// 同着があると Postgres は都合のいい順で返すので、LIMIT の有無で並びが変わり、
// 「ホームの列とプレイリスト画面で曲順が違う」「コピーすると別の順で入る」が起きる。
const presetOrder = `st.stream_date DESC, p.order_index, p.start_seconds, p.id`

// presetLatestPerSong は条件に合う歌唱を曲ごとに 1 件（最新の配信のもの）へ畳む CTE。
// 同じ曲を何度も歌っているため、畳まないと一覧が同じ曲で埋まる。
func presetLatestPerSong(access ViewerAccess) string {
	return `
	WITH latest_per_song AS (
		SELECT DISTINCT ON (p.song_id) p.id
		FROM performances p
		JOIN streams st ON st.id = p.stream_id
	` + presetWhere(access) + `
		ORDER BY p.song_id, ` + presetOrder + `
	)`
}

// FindByPreset は条件に合う歌唱を新しい配信の順で返す（曲ごとに最新の 1 件）。
func (r *PerformanceRepository) FindByPreset(f PresetFilter, limit int, access ViewerAccess) ([]PerformanceWithDetails, error) {
	query := presetLatestPerSong(access) + perfDetailSelect + `
		JOIN latest_per_song lps ON lps.id = p.id
		ORDER BY ` + presetOrder + `
		LIMIT $5`
	return r.queryPerformanceDetails(query, append(presetArgs(f), limit)...)
}

// FindIDsByPreset は FindByPreset と同じ並びの歌唱 ID だけを返す（プレイリストへのコピー用）。
// 明細を組み立てると歌手・タグを 1 件ずつ引くことになるので、ID で足りる経路は分けている。
func (r *PerformanceRepository) FindIDsByPreset(f PresetFilter, limit int, access ViewerAccess) ([]uuid.UUID, error) {
	query := presetLatestPerSong(access) + `
		SELECT p.id
		FROM performances p
		JOIN streams st ON st.id = p.stream_id
		JOIN latest_per_song lps ON lps.id = p.id
		ORDER BY ` + presetOrder + `
		LIMIT $5`
	rows, err := r.db.Query(query, append(presetArgs(f), limit)...)
	if err != nil {
		return nil, fmt.Errorf("find preset performance ids: %w", err)
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan preset performance id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CountByPreset は条件に合う曲数を返す（FindByPreset と同じく曲単位で数える）。
func (r *PerformanceRepository) CountByPreset(f PresetFilter, access ViewerAccess) (int, error) {
	query := `
		SELECT count(DISTINCT p.song_id)
		FROM performances p
		JOIN streams st ON st.id = p.stream_id
	` + presetWhere(access)

	var count int
	if err := r.db.QueryRow(query, presetArgs(f)...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count preset performances: %w", err)
	}
	return count, nil
}

// EndSource の取りうる値。migration 030 の CHECK 制約と一致させること。
const (
	EndSourceManual    = "manual"     // 人が編集画面で入力・変更した
	EndSourceHolodex   = "holodex"    // Holodex が提供
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
