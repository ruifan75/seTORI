package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TaskRunRepository は背景処理の実行記録（task_runs、issue #22）。
type TaskRunRepository struct {
	db *sql.DB
}

func NewTaskRunRepository(db *sql.DB) *TaskRunRepository {
	return &TaskRunRepository{db: db}
}

// TaskFailure は失敗した対象 1 件と理由。
type TaskFailure struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// TaskRun は実行 1 回の記録。
type TaskRun struct {
	ID         uuid.UUID       `json:"id"`
	Kind       string          `json:"kind"`
	Phase      string          `json:"phase"`
	Status     string          `json:"status"`
	Total      int             `json:"total"`
	Done       int             `json:"done"`
	Succeeded  int             `json:"succeeded"`
	Skipped    int             `json:"skipped"`
	Failed     int             `json:"failed"`
	Params     json.RawMessage `json:"params"`
	Failures   []TaskFailure   `json:"failures"`
	Message    string          `json:"message"`
	StartedBy  *string         `json:"started_by_name,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

// Create は実行を running で作る。
func (r *TaskRunRepository) Create(id uuid.UUID, kind string, params any, startedBy *uuid.UUID) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal task params: %w", err)
	}
	_, err = r.db.Exec(`
		INSERT INTO task_runs (id, kind, status, params, started_by)
		VALUES ($1, $2, 'running', $3, $4)`, id, kind, raw, startedBy)
	if err != nil {
		return fmt.Errorf("create task run: %w", err)
	}
	return nil
}

// Progress は進捗を書く（失敗は直近の一部をまるごと置き換える）。
func (r *TaskRunRepository) Progress(id uuid.UUID, total, done, succeeded, skipped, failed int, failures []TaskFailure) error {
	if failures == nil {
		failures = []TaskFailure{}
	}
	raw, err := json.Marshal(failures)
	if err != nil {
		return fmt.Errorf("marshal task failures: %w", err)
	}
	_, err = r.db.Exec(`
		UPDATE task_runs
		SET total = $2, done = $3, succeeded = $4, skipped = $5, failed = $6, failures = $7
		WHERE id = $1`, id, total, done, succeeded, skipped, failed, raw)
	if err != nil {
		return fmt.Errorf("update task run progress: %w", err)
	}
	return nil
}

// Finish は実行を確定する。
func (r *TaskRunRepository) Finish(id uuid.UUID, status, message string) error {
	_, err := r.db.Exec(`
		UPDATE task_runs SET status = $2, message = $3, finished_at = NOW() WHERE id = $1`,
		id, status, message)
	if err != nil {
		return fmt.Errorf("finish task run: %w", err)
	}
	return nil
}

// MarkInterrupted は起動時に、前のプロセスが running のまま残した行を interrupted にする。
//
// **残すと「実行中」が永久に続いて見える。** backfill はメモリ上の goroutine なので、
// プロセスが落ちた時点で止まっている。running のまま置くと、画面は進まない進捗を
// 出し続け、運用者は待ち続ける。
func (r *TaskRunRepository) MarkInterrupted() (int64, error) {
	res, err := r.db.Exec(`
		UPDATE task_runs
		SET status = 'interrupted', finished_at = NOW(),
		    message = CASE WHEN message = '' THEN 'サーバーの再起動で中断されました' ELSE message END
		WHERE status = 'running'`)
	if err != nil {
		return 0, fmt.Errorf("mark interrupted task runs: %w", err)
	}
	return res.RowsAffected()
}

const taskRunSelect = `
	SELECT t.id, t.kind, t.status, t.total, t.done, t.succeeded, t.skipped, t.failed,
	       t.params, t.failures, t.message, u.username, t.started_at, t.finished_at, t.phase
	FROM task_runs t
	LEFT JOIN users u ON u.id = t.started_by`

func scanTaskRun(row interface{ Scan(...any) error }) (TaskRun, error) {
	var t TaskRun
	var failures []byte
	if err := row.Scan(&t.ID, &t.Kind, &t.Status, &t.Total, &t.Done, &t.Succeeded, &t.Skipped, &t.Failed,
		&t.Params, &failures, &t.Message, &t.StartedBy, &t.StartedAt, &t.FinishedAt, &t.Phase); err != nil {
		return t, err
	}
	if err := json.Unmarshal(failures, &t.Failures); err != nil || t.Failures == nil {
		t.Failures = []TaskFailure{}
	}
	return t, nil
}

// List は新しい順に返す。
func (r *TaskRunRepository) List(limit int) ([]TaskRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.Query(taskRunSelect+` ORDER BY t.started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list task runs: %w", err)
	}
	defer rows.Close()
	out := []TaskRun{}
	for rows.Next() {
		t, err := scanTaskRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task run: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FindByID は 1 件を返す。無ければ nil。
func (r *TaskRunRepository) FindByID(id uuid.UUID) (*TaskRun, error) {
	t, err := scanTaskRun(r.db.QueryRow(taskRunSelect+` WHERE t.id = $1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find task run: %w", err)
	}
	return &t, nil
}

func (r *TaskRunRepository) SetPhase(id uuid.UUID, phase string) error {
	_, err := r.db.Exec(`UPDATE task_runs SET phase = $2 WHERE id = $1`, id, phase)
	return err
}
