package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ruifan75/setori/pkg/streamtag"
	"time"
)

var ErrVisibilityConflict = errors.New("対象の状態が変わりました。一覧から選び直してください")
var ErrVisibilitySelection = errors.New("配信を1〜500件選んでください（重複不可）")

type VisibilityReviewRepository struct{ db *sql.DB }

func NewVisibilityReviewRepository(db *sql.DB) *VisibilityReviewRepository {
	return &VisibilityReviewRepository{db}
}

type VisibilityCandidate struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	StreamDate time.Time `json:"stream_date"`
	Duration   int       `json:"duration_seconds"`
	Tags       []string  `json:"tags"`
}
type VisibilityReviewRun struct {
	ID         uuid.UUID  `json:"id"`
	Status     string     `json:"status"`
	Count      int        `json:"item_count"`
	Reverted   int        `json:"reverted_count"`
	CreatedAt  time.Time  `json:"created_at"`
	AppliedAt  *time.Time `json:"applied_at"`
	RevertedAt *time.Time `json:"reverted_at"`
}

// 通常の候補と実行直前の検査で同じ判定を使う。長さ不明・短尺は候補にしない。
func visibilityCandidateWhere(musicArg, durationArg string) string {
	return `s.is_hidden = TRUE AND s.duration_seconds > ` + durationArg + `
 AND EXISTS (SELECT 1 FROM stream_stream_tags t WHERE t.stream_id = s.id AND t.tag_id = ANY(` + musicArg + `))
 AND NOT EXISTS (SELECT 1 FROM non_singing_checks c WHERE c.stream_id = s.id)`
}
func (r *VisibilityReviewRepository) Candidates(dismissed bool, limit, offset int) ([]VisibilityCandidate, int, error) {
	where := visibilityCandidateWhere("$1", "$2")
	args := []any{pq.Array(streamtag.MusicIDs()), streamtag.ShortFormMaxDurationSeconds}
	if dismissed {
		// 判断は候補の条件が変わっても一覧・取り消しできる。
		where = `EXISTS (SELECT 1 FROM non_singing_checks c WHERE c.stream_id = s.id)`
		args = nil
	}
	var count int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM streams s WHERE `+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	n := len(args)
	args = append(args, limit, offset)
	rows, err := r.db.Query(fmt.Sprintf(`SELECT s.id, s.title, s.stream_date, COALESCE(s.duration_seconds, 0),
 COALESCE((SELECT array_agg(t.tag_id ORDER BY t.tag_id) FROM stream_stream_tags t WHERE t.stream_id = s.id), '{}')
 FROM streams s WHERE %s ORDER BY s.stream_date DESC, s.id ASC LIMIT $%d OFFSET $%d`, where, n+1, n+2), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []VisibilityCandidate{}
	for rows.Next() {
		var c VisibilityCandidate
		if err := rows.Scan(&c.ID, &c.Title, &c.StreamDate, &c.Duration, pq.Array(&c.Tags)); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, count, rows.Err()
}

// Preview は前値と更新時刻を退避する。配信の表示はまだ変えない。
func (r *VisibilityReviewRepository) Preview(ids []string, by *uuid.UUID) (uuid.UUID, error) {
	if len(ids) < 1 || len(ids) > 500 {
		return uuid.Nil, ErrVisibilitySelection
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return uuid.Nil, ErrVisibilitySelection
		}
		seen[id] = true
	}
	tx, err := r.db.Begin()
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback()
	id := uuid.New()
	if _, err := tx.Exec(`INSERT INTO visibility_review_runs (id, status, item_count, started_by) VALUES ($1, 'preview', $2, $3)`, id, len(ids), by); err != nil {
		return uuid.Nil, err
	}
	result, err := tx.Exec(`INSERT INTO visibility_review_items (run_id, stream_id, stream_title, before_hidden, after_hidden, before_updated_at)
 SELECT $3, s.id, s.title, s.is_hidden, FALSE, s.updated_at FROM streams s
 WHERE `+visibilityCandidateWhere("$1", "$2")+` AND s.id = ANY($4)`, pq.Array(streamtag.MusicIDs()), streamtag.ShortFormMaxDurationSeconds, id, pq.Array(ids))
	if err != nil {
		return uuid.Nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return uuid.Nil, err
	}
	if count != int64(len(ids)) {
		return uuid.Nil, ErrVisibilityConflict
	}
	return id, tx.Commit()
}

const visibilityLockRunSQL = `SELECT status, item_count FROM visibility_review_runs WHERE id = $1 FOR UPDATE`

// Apply は表示の前値・候補条件を全件確認し、1 件でも違っていれば全体を取り消す。
// 更新時刻は監査用に退避するが、同期・解析など対象外の変更は競合にしない。
func (r *VisibilityReviewRepository) Apply(id uuid.UUID) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var status string
	var count int
	if err := tx.QueryRow(visibilityLockRunSQL, id).Scan(&status, &count); err != nil {
		return 0, err
	}
	if status != "preview" {
		return 0, ErrVisibilityConflict
	}
	rows, err := tx.Query(`SELECT s.id FROM streams s JOIN visibility_review_items i ON i.stream_id = s.id
 WHERE `+visibilityCandidateWhere("$1", "$2")+` AND i.run_id = $3
 AND s.is_hidden = i.before_hidden
 ORDER BY s.id ASC FOR UPDATE OF s`, pq.Array(streamtag.MusicIDs()), streamtag.ShortFormMaxDurationSeconds, id)
	if err != nil {
		return 0, err
	}
	found := 0
	for rows.Next() {
		var streamID string
		if err := rows.Scan(&streamID); err != nil {
			rows.Close()
			return 0, err
		}
		found++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if found != count {
		return 0, ErrVisibilityConflict
	}
	result, err := tx.Exec(`WITH changed AS (
 UPDATE streams s SET is_hidden = i.after_hidden, updated_at = NOW()
 FROM visibility_review_items i WHERE i.run_id = $1 AND i.stream_id = s.id
 AND s.is_hidden = i.before_hidden
 AND `+visibilityCandidateWhere("$2", "$3")+`
 RETURNING s.id, s.updated_at)
 UPDATE visibility_review_items i SET after_updated_at = c.updated_at
 FROM changed c WHERE i.run_id = $1 AND i.stream_id = c.id`, id, pq.Array(streamtag.MusicIDs()), streamtag.ShortFormMaxDurationSeconds)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n != int64(count) {
		return 0, ErrVisibilityConflict
	}
	if _, err := tx.Exec(`UPDATE visibility_review_runs SET status = 'applied', applied_at = NOW() WHERE id = $1`, id); err != nil {
		return 0, err
	}
	return count, tx.Commit()
}

// Revert は表示の後値が今も同じものだけを戻す。値が違う行・削除済みの行は見送る。
// 同期・解析など対象外の変更は保ち、表示状態だけを戻す。
// 戻せなかった件数も run の item_count - reverted_count として残す。再撤回は認めない。
func (r *VisibilityReviewRepository) Revert(id uuid.UUID) (int, int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var status string
	var count int
	if err := tx.QueryRow(visibilityLockRunSQL, id).Scan(&status, &count); err != nil {
		return 0, 0, err
	}
	if status != "applied" {
		return 0, 0, ErrVisibilityConflict
	}
	result, err := tx.Exec(`WITH restored AS (
 UPDATE streams s SET is_hidden = i.before_hidden, updated_at = NOW()
 FROM visibility_review_items i WHERE i.run_id = $1 AND i.stream_id = s.id
 AND s.is_hidden = i.after_hidden
 AND i.reverted_at IS NULL
 RETURNING s.id)
 UPDATE visibility_review_items i SET reverted_at = NOW()
 FROM restored s WHERE i.run_id = $1 AND i.stream_id = s.id`, id)
	if err != nil {
		return 0, 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(`UPDATE visibility_review_runs SET status = 'reverted', reverted_count = $2, reverted_at = NOW() WHERE id = $1`, id, n); err != nil {
		return 0, 0, err
	}
	return int(n), count - int(n), tx.Commit()
}
func (r *VisibilityReviewRepository) Runs() ([]VisibilityReviewRun, error) {
	rows, err := r.db.Query(`SELECT id, status, item_count, reverted_count, created_at, applied_at, reverted_at FROM visibility_review_runs ORDER BY created_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VisibilityReviewRun{}
	for rows.Next() {
		var r VisibilityReviewRun
		if err := rows.Scan(&r.ID, &r.Status, &r.Count, &r.Reverted, &r.CreatedAt, &r.AppliedAt, &r.RevertedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
