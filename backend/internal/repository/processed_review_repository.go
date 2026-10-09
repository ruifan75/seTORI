package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"strings"
	"time"
)

var ErrProcessedConflict = errors.New("対象の状態が変わりました。一覧から選び直してください")
var ErrProcessedSelection = errors.New("配信を1〜500件選んでください（重複不可）")

type ProcessedReviewRepository struct{ db *sql.DB }

func NewProcessedReviewRepository(db *sql.DB) *ProcessedReviewRepository {
	return &ProcessedReviewRepository{db}
}

type ProcessedCandidate struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	StreamDate  time.Time `json:"stream_date"`
	IsHidden    bool      `json:"is_hidden"`
	IsProcessed bool      `json:"is_processed"`
}
type ProcessedReviewRun struct {
	ID             uuid.UUID  `json:"id"`
	Status         string     `json:"status"`
	Count          int        `json:"item_count"`
	AfterProcessed bool       `json:"after_processed"`
	Reverted       int        `json:"reverted_count"`
	CreatedAt      time.Time  `json:"created_at"`
	AppliedAt      *time.Time `json:"applied_at"`
	RevertedAt     *time.Time `json:"reverted_at"`
}

// 歌唱の有無は閲覧可能な歌唱だけから判断する。秘匿の中身を絞り込みで漏らさない。
type ProcessedReviewFilters struct {
	Query, ChannelID        string
	TagIDs                  []string
	Hidden, HasPerformances *bool
	From, Until             *time.Time // [From, Until)。画面の終了日は翌日を送る。
	AfterProcessed          bool
}

func processedCandidateWhere(f ProcessedReviewFilters, access ViewerAccess) (string, []any) {
	where := "s.is_processed <> $1"
	args := []any{f.AfterProcessed}
	add := func(expr string, value any) {
		args = append(args, value)
		where += " AND " + fmt.Sprintf(expr, len(args))
	}
	if f.Query != "" {
		// LIKE のワイルドカードと escape 文字自体を文字として検索する。
		query := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Query)
		add("s.title ILIKE '%%' || $%d || '%%' ESCAPE '!'", query)
	}
	if f.ChannelID != "" {
		add("EXISTS (SELECT 1 FROM stream_channels sc WHERE sc.stream_id = s.id AND sc.channel_id = $%d)", f.ChannelID)
	}
	if len(f.TagIDs) > 0 {
		// 同じタグを複数送っても AND の意味を変えない。
		seen := map[string]bool{}
		tags := []string{}
		for _, tag := range f.TagIDs {
			tag = strings.TrimSpace(tag)
			if tag != "" && !seen[tag] {
				tags = append(tags, tag)
				seen[tag] = true
			}
		}
		if len(tags) > 0 {
			add("s.id IN (SELECT st.stream_id FROM stream_stream_tags st WHERE st.tag_id = ANY($%d) GROUP BY st.stream_id HAVING COUNT(DISTINCT st.tag_id) = "+fmt.Sprint(len(tags))+")", pq.Array(tags))
		}
	}
	if f.Hidden != nil {
		add("s.is_hidden = $%d", *f.Hidden)
	}
	if f.From != nil {
		add("s.stream_date >= $%d", *f.From)
	}
	if f.Until != nil {
		add("s.stream_date < $%d", *f.Until)
	}
	if f.HasPerformances != nil {
		exists := "(EXISTS (SELECT 1 FROM performances p WHERE p.stream_id = s.id) AND " + NotRestrictedFor("s", access) + ")"
		if !*f.HasPerformances {
			exists = "NOT " + exists
		}
		where += " AND " + exists
	}
	return where, args
}

// 件数と一覧は同じ WHERE・引数。解析素材や歌唱の曲名は返さない。
func (r *ProcessedReviewRepository) Candidates(f ProcessedReviewFilters, limit, offset int, access ViewerAccess) ([]ProcessedCandidate, int, error) {
	where, args := processedCandidateWhere(f, access)
	var count int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM streams s WHERE "+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	n := len(args)
	rows, err := r.db.Query(fmt.Sprintf(`SELECT s.id, s.title, s.stream_date, s.is_hidden, s.is_processed
 FROM streams s WHERE %s ORDER BY s.stream_date DESC, s.id ASC LIMIT $%d OFFSET $%d`, where, n+1, n+2), append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ProcessedCandidate{}
	for rows.Next() {
		var c ProcessedCandidate
		if err := rows.Scan(&c.ID, &c.Title, &c.StreamDate, &c.IsHidden, &c.IsProcessed); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, count, rows.Err()
}

// Preview は前値と更新時刻を退避する。処理済み状態はまだ変えない。
func (r *ProcessedReviewRepository) Preview(ids []string, after bool, by *uuid.UUID) (uuid.UUID, error) {
	if len(ids) < 1 || len(ids) > 500 {
		return uuid.Nil, ErrProcessedSelection
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return uuid.Nil, ErrProcessedSelection
		}
		seen[id] = true
	}
	tx, err := r.db.Begin()
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback()
	id := uuid.New()
	if _, err := tx.Exec(`INSERT INTO processed_review_runs (id, status, item_count, after_processed, started_by) VALUES ($1, 'preview', $2, $3, $4)`, id, len(ids), after, by); err != nil {
		return uuid.Nil, err
	}
	result, err := tx.Exec(`INSERT INTO processed_review_items (run_id, stream_id, stream_title, before_processed, after_processed, before_updated_at)
 SELECT $1, s.id, s.title, s.is_processed, $2, s.updated_at FROM streams s
 WHERE s.is_processed <> $2 AND s.id = ANY($3)`, id, after, pq.Array(ids))
	if err != nil {
		return uuid.Nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return uuid.Nil, err
	}
	if count != int64(len(ids)) {
		return uuid.Nil, ErrProcessedConflict
	}
	return id, tx.Commit()
}

const processedLockRunSQL = `SELECT status, item_count FROM processed_review_runs WHERE id = $1 FOR UPDATE`

// Apply は処理済みの前値を全件確認し、1 件でも違っていれば全体を取り消す。
// 更新時刻は監査用に退避するが、同期・解析など対象外の変更は競合にしない。
func (r *ProcessedReviewRepository) Apply(id uuid.UUID) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var status string
	var count int
	if err := tx.QueryRow(processedLockRunSQL, id).Scan(&status, &count); err != nil {
		return 0, err
	}
	if status != "preview" {
		return 0, ErrProcessedConflict
	}
	rows, err := tx.Query(`SELECT s.id FROM streams s JOIN processed_review_items i ON i.stream_id = s.id
 WHERE i.run_id = $1 AND s.is_processed = i.before_processed
 ORDER BY s.id ASC FOR UPDATE OF s`, id)
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
		return 0, ErrProcessedConflict
	}
	result, err := tx.Exec(`WITH changed AS (
 UPDATE streams s SET is_processed = i.after_processed, updated_at = NOW()
 FROM processed_review_items i WHERE i.run_id = $1 AND i.stream_id = s.id
 AND s.is_processed = i.before_processed
 RETURNING s.id, s.updated_at)
 UPDATE processed_review_items i SET after_updated_at = c.updated_at
 FROM changed c WHERE i.run_id = $1 AND i.stream_id = c.id`, id)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n != int64(count) {
		return 0, ErrProcessedConflict
	}
	if _, err := tx.Exec(`UPDATE processed_review_runs SET status = 'applied', applied_at = NOW() WHERE id = $1`, id); err != nil {
		return 0, err
	}
	return count, tx.Commit()
}

// Revert は処理済みの後値が今も同じものだけを戻す。値が違う行・削除済みの行は見送る。
// 同期・解析など対象外の変更は保ち、処理済み状態だけを戻す。
// 戻せなかった件数も run の item_count - reverted_count として残す。再撤回は認めない。
func (r *ProcessedReviewRepository) Revert(id uuid.UUID) (int, int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var status string
	var count int
	if err := tx.QueryRow(processedLockRunSQL, id).Scan(&status, &count); err != nil {
		return 0, 0, err
	}
	if status != "applied" {
		return 0, 0, ErrProcessedConflict
	}
	result, err := tx.Exec(`WITH restored AS (
 UPDATE streams s SET is_processed = i.before_processed, updated_at = NOW()
 FROM processed_review_items i WHERE i.run_id = $1 AND i.stream_id = s.id
 AND s.is_processed = i.after_processed
 AND i.reverted_at IS NULL
 RETURNING s.id)
 UPDATE processed_review_items i SET reverted_at = NOW()
 FROM restored s WHERE i.run_id = $1 AND i.stream_id = s.id`, id)
	if err != nil {
		return 0, 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(`UPDATE processed_review_runs SET status = 'reverted', reverted_count = $2, reverted_at = NOW() WHERE id = $1`, id, n); err != nil {
		return 0, 0, err
	}
	return int(n), count - int(n), tx.Commit()
}
func (r *ProcessedReviewRepository) Runs() ([]ProcessedReviewRun, error) {
	rows, err := r.db.Query(`SELECT id, status, item_count, after_processed, reverted_count, created_at, applied_at, reverted_at FROM processed_review_runs ORDER BY created_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProcessedReviewRun{}
	for rows.Next() {
		var r ProcessedReviewRun
		if err := rows.Scan(&r.ID, &r.Status, &r.Count, &r.AfterProcessed, &r.Reverted, &r.CreatedAt, &r.AppliedAt, &r.RevertedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
