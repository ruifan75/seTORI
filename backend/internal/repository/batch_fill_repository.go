package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// BatchFillRepository は一括セットリスト作成の実行記録を扱う。
//
// 一括は performances（主データ）に直接書くので、後から**その回の分だけ**
// 戻せる必要がある。そのための台帳。
type BatchFillRepository struct {
	db *sql.DB
}

func NewBatchFillRepository(db *sql.DB) *BatchFillRepository {
	return &BatchFillRepository{db: db}
}

// BatchFillRun は実行 1 回。
type BatchFillRun struct {
	ID           uuid.UUID `json:"id"`
	Mode         string    `json:"mode"`
	SingerID     *string   `json:"singer_id,omitempty"`
	Status       string    `json:"status"`
	StreamsTotal int       `json:"streams_total"`
	StreamsDone  int       `json:"streams_done"`
	SongsCreated int       `json:"songs_created"`
	SongsReview  int       `json:"songs_review"`
	// SongsGap は「DB にあるが源に無い」歌唱の件数（force 実行のみ）。
	// 提案としては積まないので、ここが唯一の入口になる。
	SongsGap      int        `json:"songs_gap"`
	AIAsked       int        `json:"ai_asked"`
	Message       string     `json:"message"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	StartedByName *string    `json:"started_by_name,omitempty"`
}

// BatchFillGap は「DB にあるが源に無い」歌唱 1 件（表示用に曲名と時間を添えて返す）。
type BatchFillGap struct {
	StreamID      string `json:"stream_id"`
	StreamTitle   string `json:"stream_title"`
	PerformanceID string `json:"performance_id"`
	SongName      string `json:"song_name"`
	StartSeconds  int    `json:"start_seconds"`
}

// BatchOrigin は「この歌唱をどう作ったか」。歌唱は (stream_id, song_id, start_seconds) で一意。
type BatchOrigin struct {
	SongID       uuid.UUID
	StartSeconds int
	Via          string // rule / ai
	Confidence   float64
}

// CreateRun は実行を開始し、その ID を返す。
func (r *BatchFillRepository) CreateRun(mode string, singerID *string, startedBy *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(`
		INSERT INTO batch_fill_runs (mode, singer_id, started_by) VALUES ($1, $2, $3) RETURNING id`,
		mode, singerID, startedBy).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create batch fill run: %w", err)
	}
	return id, nil
}

// UpdateProgress は進捗を書く（実行中に何度も呼ばれる）。
func (r *BatchFillRepository) UpdateProgress(id uuid.UUID, total, done, created, review, gap, aiAsked int) error {
	_, err := r.db.Exec(`
		UPDATE batch_fill_runs
		SET streams_total = $2, streams_done = $3, songs_created = $4, songs_review = $5,
		    songs_gap = $6, ai_asked = $7
		WHERE id = $1`, id, total, done, created, review, gap, aiAsked)
	if err != nil {
		return fmt.Errorf("update batch fill progress: %w", err)
	}
	return nil
}

// RecordGaps は「DB にあるが源に無い」歌唱を実行に紐づけて残す。
//
// 提案としては積まない（源は欠けているのが普通で、欠落 1 件ごとに待ち行列を作ると
// 人が処理できない量になるうえ、「源に無い」だけでは何をすべきか決まらない）。
// それでもログだけでは誰も気付けないので、実行履歴から辿れるようにここへ書く。
func (r *BatchFillRepository) RecordGaps(runID uuid.UUID, streamID string, perfIDs []uuid.UUID) error {
	for _, pid := range perfIDs {
		if _, err := r.db.Exec(`
			INSERT INTO batch_fill_gaps (run_id, stream_id, performance_id)
			VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, runID, streamID, pid); err != nil {
			return fmt.Errorf("record batch fill gap: %w", err)
		}
	}
	return nil
}

// ListGaps は実行が見つけた「源に無い既存の歌唱」を返す。
// 歌唱が後から消されていれば CASCADE で行ごと消えるので、ここには出てこない。
func (r *BatchFillRepository) ListGaps(runID uuid.UUID) ([]BatchFillGap, error) {
	rows, err := r.db.Query(`
		SELECT g.stream_id, st.title, g.performance_id, s.name, p.start_seconds
		FROM batch_fill_gaps g
		JOIN performances p ON p.id = g.performance_id
		JOIN songs s ON s.id = p.song_id
		JOIN streams st ON st.id = g.stream_id
		WHERE g.run_id = $1
		ORDER BY st.stream_date DESC, p.start_seconds`, runID)
	if err != nil {
		return nil, fmt.Errorf("list batch fill gaps: %w", err)
	}
	defer rows.Close()

	out := []BatchFillGap{}
	for rows.Next() {
		var g BatchFillGap
		if err := rows.Scan(&g.StreamID, &g.StreamTitle, &g.PerformanceID, &g.SongName, &g.StartSeconds); err != nil {
			return nil, fmt.Errorf("scan batch fill gap: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// FinishRun は実行を終える。
func (r *BatchFillRepository) FinishRun(id uuid.UUID, status, message string) error {
	_, err := r.db.Exec(`
		UPDATE batch_fill_runs SET status = $2, message = $3, finished_at = NOW() WHERE id = $1`,
		id, status, message)
	if err != nil {
		return fmt.Errorf("finish batch fill run: %w", err)
	}
	return nil
}

// MarkOrigins は作った歌唱に実行 ID と由来を付ける。
//
// CreatePerformances は ID を返さないので、一意キー
// (stream_id, song_id, start_seconds) で引き当てる。
// **既に別の実行の印が付いている行は触らない** ── 上書きすると、
// 先に作った実行を撤回したときにこちらの行まで消える。
func (r *BatchFillRepository) MarkOrigins(streamID string, runID uuid.UUID, origins []BatchOrigin) error {
	for _, o := range origins {
		var conf any
		if o.Confidence > 0 {
			conf = o.Confidence
		}
		_, err := r.db.Exec(`
			UPDATE performances
			SET batch_run_id = $1, created_via = $2, match_confidence = $3
			WHERE stream_id = $4 AND song_id = $5 AND start_seconds = $6 AND batch_run_id IS NULL`,
			runID, o.Via, conf, streamID, o.SongID, o.StartSeconds)
		if err != nil {
			return fmt.Errorf("mark batch origin: %w", err)
		}
	}
	return nil
}

// DeleteByRun は実行が作った歌唱をすべて消す（撤回）。消した件数を返す。
func (r *BatchFillRepository) DeleteByRun(runID uuid.UUID) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM performances WHERE batch_run_id = $1`, runID)
	if err != nil {
		return 0, fmt.Errorf("delete batch performances: %w", err)
	}
	n, _ := res.RowsAffected()
	if _, err := r.db.Exec(
		`UPDATE batch_fill_runs SET status = 'reverted', message = $2 WHERE id = $1`,
		runID, fmt.Sprintf("%d 件の歌唱を撤回しました", n)); err != nil {
		return n, fmt.Errorf("mark run reverted: %w", err)
	}
	return n, nil
}

// ListRuns は新しい順に実行を返す。
func (r *BatchFillRepository) ListRuns(limit int) ([]BatchFillRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.Query(`
		SELECT b.id, b.mode, b.singer_id, b.status, b.streams_total, b.streams_done,
		       b.songs_created, b.songs_review, b.songs_gap, b.ai_asked, b.message,
		       b.started_at, b.finished_at, u.username
		FROM batch_fill_runs b
		LEFT JOIN users u ON u.id = b.started_by
		ORDER BY b.started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list batch fill runs: %w", err)
	}
	defer rows.Close()

	var out []BatchFillRun
	for rows.Next() {
		var b BatchFillRun
		if err := rows.Scan(&b.ID, &b.Mode, &b.SingerID, &b.Status, &b.StreamsTotal, &b.StreamsDone,
			&b.SongsCreated, &b.SongsReview, &b.SongsGap, &b.AIAsked, &b.Message,
			&b.StartedAt, &b.FinishedAt, &b.StartedByName); err != nil {
			return nil, fmt.Errorf("scan batch fill run: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
