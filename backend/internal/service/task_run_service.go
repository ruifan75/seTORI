package service

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
)

// 背景処理の種類（task_runs.kind）。
const (
	TaskChatEndBackfill  = "chat_end_backfill"
	TaskChapterBackfill  = "chapter_backfill"
	TaskStreamPrepare    = "stream_prepare"
	TaskReadingsBackfill = "readings_backfill"
	TaskDuplicateScan    = "duplicate_scan"
)

// ErrTaskRunning は同じ種類、または準備と競合する backfill が既に走っていること。
var ErrTaskRunning = errors.New("同じ処理または競合する背景処理が実行中です")

// maxTaskFailures は残す失敗の件数。全件を JSONB に積むと、BOT 判定で全滅した
// 1300 件の実行が 1 行で数百 KB になる。「何が起きたか」が分かれば足りる。
const maxTaskFailures = 200

// TaskRunService は背景処理の「実行 1 回」を記録する（issue #22）。
//
// 汎用の job framework ではない。走らせるのは今までどおり呼び出し側の goroutine で、
// ここは開始・進捗・失敗・終了を DB に残すだけ。**同じ種類は同時に 1 つまで**
// ── 2 つ走らせると、同じ配信へ yt-dlp を二重に起動する。
// 準備は章節・拍手 end backfill とも相互排他にする（issue #27）。
type TaskRunService struct {
	repo *repository.TaskRunRepository

	mu      sync.Mutex
	running map[string]bool
	active  map[uuid.UUID]*TaskRun
}

func NewTaskRunService(repo *repository.TaskRunRepository) *TaskRunService {
	return &TaskRunService{repo: repo, running: map[string]bool{}, active: map[uuid.UUID]*TaskRun{}}
}

// MarkInterrupted は前のプロセスが running のまま残した行を片付ける（起動時に呼ぶ）。
func (s *TaskRunService) MarkInterrupted() {
	n, err := s.repo.MarkInterrupted()
	if err != nil {
		logger.Warnf("[task] 中断された実行の片付けに失敗: %v", err)
		return
	}
	if n > 0 {
		logger.Infof("[task] 前回の起動で実行中だった %d 件を中断として記録しました", n)
	}
}

// Start は実行を始める。同じ種類、または準備と競合する backfill が走っていれば ErrTaskRunning。
// 戻り値の TaskRun は**必ず Finish すること**（しないと以後その種類が開始できない）。
func (s *TaskRunService) Start(kind string, params any, startedBy *uuid.UUID) (*TaskRun, error) {
	s.mu.Lock()
	if s.running[kind] || (kind == TaskStreamPrepare && (s.running[TaskChapterBackfill] || s.running[TaskChatEndBackfill])) ||
		(kind != TaskStreamPrepare && s.running[TaskStreamPrepare] && (kind == TaskChapterBackfill || kind == TaskChatEndBackfill)) {
		s.mu.Unlock()
		return nil, ErrTaskRunning
	}
	s.running[kind] = true
	s.mu.Unlock()

	id := uuid.New()
	if err := s.repo.Create(id, kind, params, startedBy); err != nil {
		s.release(kind)
		return nil, err
	}
	run := &TaskRun{svc: s, ID: id, kind: kind}
	s.mu.Lock()
	s.active[id] = run
	s.mu.Unlock()
	return run, nil
}

func (s *TaskRunService) release(kind string) {
	s.mu.Lock()
	delete(s.running, kind)
	for id, run := range s.active {
		if run.kind == kind {
			delete(s.active, id)
		}
	}
	s.mu.Unlock()
}

// List / Get は記録を読む。
func (s *TaskRunService) List(limit int) ([]repository.TaskRun, error) { return s.repo.List(limit) }
func (s *TaskRunService) Get(id uuid.UUID) (*repository.TaskRun, error) {
	return s.repo.FindByID(id)
}

// TaskRun は実行中の 1 回。並行に呼ばれてよい（backfill は複数の goroutine で回す）。
//
// 結果は 3 つに分ける ── **成功・見送り・失敗を 1 つの数に入れない**（CLAUDE.md §6.1）。
// 以前の完了ログ「N 件」は試行数で、成功数ではなかった。
type TaskRun struct {
	svc  *TaskRunService
	ID   uuid.UUID
	kind string

	flushMu   sync.Mutex // snapshot と UPDATE の順序を揃える
	mu        sync.Mutex
	total     int
	done      int
	succeeded int
	skipped   int
	failed    int
	failures  []repository.TaskFailure
	lastFlush time.Time
	finished  bool
	cancelled atomic.Bool
}

// SetTotal は対象の件数を決める（対象を列挙したあとに呼ぶ）。
func (t *TaskRun) SetTotal(n int) {
	t.mu.Lock()
	t.total = n
	t.mu.Unlock()
	t.flush(true)
}

// Succeed / Skip / Fail は 1 件ぶんの結果を記録する。
func (t *TaskRun) Succeed() { t.record(func() { t.succeeded++ }) }

// Skip は「失敗ではないが、今回は結論を出していない」（replay がまだ無い等）。
func (t *TaskRun) Skip() { t.record(func() { t.skipped++ }) }

// Fail は失敗と理由を残す。理由は log と違って消えない。
func (t *TaskRun) Fail(target, reason string) {
	t.record(func() {
		t.failed++
		t.failures = append(t.failures, repository.TaskFailure{Target: target, Reason: reason})
		if len(t.failures) > maxTaskFailures {
			copy(t.failures, t.failures[1:])
			t.failures = t.failures[:maxTaskFailures]
		}
	})
}

func (t *TaskRun) record(apply func()) {
	t.mu.Lock()
	apply()
	t.done++
	t.mu.Unlock()
	t.flush(false)
}

// flush は進捗を DB へ書く。**毎件は書かない**（2 秒に 1 回まで）── 並行 3 本の
// backfill が 1 件ごとに UPDATE すると、それだけで 1 vCPU の本番機の DB を叩き続ける。
// Finish は最終値の保存を試み、失敗した場合は実行を failed として記録する。
func (t *TaskRun) flush(force bool) error {
	t.flushMu.Lock()
	defer t.flushMu.Unlock()
	t.mu.Lock()
	if !force && time.Since(t.lastFlush) < 2*time.Second {
		t.mu.Unlock()
		return nil
	}
	t.lastFlush = time.Now()
	total, done, ok, sk, ng := t.total, t.done, t.succeeded, t.skipped, t.failed
	failures := append([]repository.TaskFailure(nil), t.failures...)
	t.mu.Unlock()

	if err := t.svc.repo.Progress(t.ID, total, done, ok, sk, ng, failures); err != nil {
		logger.Warnf("[task] %s の進捗の保存に失敗: %v", t.kind, err)
		return err
	}
	return nil
}

// FailedCount は今までの失敗の件数（完了の文言を作るため）。
func (t *TaskRun) FailedCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failed
}

// Finish は実行を確定し、同じ種類を再び開始できるようにする。2 回目以降は何もしない。
// message を空にすると件数から作る。
func (t *TaskRun) Finish(status, message string) {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	t.finished = true
	if message == "" {
		message = fmt.Sprintf("%d 件中 成功 %d・見送り %d・失敗 %d", t.total, t.succeeded, t.skipped, t.failed)
	}
	t.mu.Unlock()

	if err := t.flush(true); err != nil {
		status = "failed"
		message = "進捗の保存に失敗しました: " + err.Error()
	}
	if err := t.svc.repo.Finish(t.ID, status, message); err != nil {
		logger.Warnf("[task] %s の終了の記録に失敗: %v", t.kind, err)
	}
	t.svc.release(t.kind)
	logger.Infof("[task] %s 終了（%s）: %s", t.kind, status, message)
}

// Execute は予約済みの実行を完了させる。呼び出し側が goroutine で呼ぶ。
// 一部失敗も failed にし、エラー・panic の場合も記録と枠の解放を試みる。
func (t *TaskRun) Execute(work func() (string, error)) {
	status, message := "failed", "処理が完了しませんでした"
	defer func() {
		if v := recover(); v != nil {
			status, message = "failed", fmt.Sprintf("処理中に panic が発生しました: %v", v)
		}
		t.Finish(status, message)
	}()
	result, err := work()
	message = result
	if err != nil {
		if message != "" {
			message += " "
		}
		message += "失敗: " + err.Error()
		return
	}
	if t.FailedCount() == 0 {
		status = "done"
	}
}
