package service

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/repository"
)

// **同じ種類は同時に 1 つまで。** 2 つ走らせると同じ配信へ yt-dlp を二重に起動する。
// 終われば再び開始できること（解放し忘れると以後ずっと 409 になる）も見る。
func TestTaskRunOnePerKind(t *testing.T) {
	db, _ := newTaskDB(t)
	svc := NewTaskRunService(repository.NewTaskRunRepository(db))

	first, err := svc.Start(TaskChapterBackfill, nil, nil)
	if err != nil {
		t.Fatalf("1 本目が開始できない: %v", err)
	}
	if _, err := svc.Start(TaskChapterBackfill, nil, nil); !errors.Is(err, ErrTaskRunning) {
		t.Fatalf("同じ種類の 2 本目が開始できてしまった（err=%v）", err)
	}
	// 別の種類は並行してよい
	other, err := svc.Start(TaskChatEndBackfill, nil, nil)
	if err != nil {
		t.Fatalf("別の種類が開始できない: %v", err)
	}
	other.Finish("done", "")

	first.Finish("done", "")
	first.Finish("done", "") // 2 回目は何もしない（解放を二重にしない）
	again, err := svc.Start(TaskChapterBackfill, nil, nil)
	if err != nil {
		t.Fatalf("終わったあとに再開始できない: %v", err)
	}
	again.Finish("done", "")
}

// **作成に失敗したら枠を返す。** 返さないと、DB の一時的な失敗 1 回で
// その種類が二度と開始できなくなる。
func TestTaskRunReleasesOnCreateFailure(t *testing.T) {
	db, d := newTaskDB(t)
	d.failInsert = true
	svc := NewTaskRunService(repository.NewTaskRunRepository(db))
	if _, err := svc.Start(TaskChapterBackfill, nil, nil); err == nil {
		t.Fatal("作成に失敗したのに開始できた")
	}
	d.failInsert = false
	run, err := svc.Start(TaskChapterBackfill, nil, nil)
	if err != nil {
		t.Fatalf("作成の失敗で枠が戻っていない: %v", err)
	}
	run.Finish("done", "")
}

// 成功・見送り・失敗を**別々に**数え、失敗は理由ごと最後に必ず書く。
func TestTaskRunRecordsOutcomesSeparately(t *testing.T) {
	db, d := newTaskDB(t)
	svc := NewTaskRunService(repository.NewTaskRunRepository(db))
	run, err := svc.Start(TaskChatEndBackfill, map[string]any{"concurrency": 3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	run.SetTotal(4)
	run.Succeed()
	run.Skip()
	run.Fail("vid1", "BOT 判定")
	run.Fail("vid2", "timeout")
	run.Finish("done", "")

	progress := d.lastExec("UPDATE task_runs SET total")
	if progress == nil {
		t.Fatalf("進捗が書かれていない: %q", d.queries())
	}
	// $2..$6 = total, done, succeeded, skipped, failed
	got := fmt.Sprint(progress[1:6])
	if got != "[4 4 1 1 2]" {
		t.Errorf("total/done/succeeded/skipped/failed = %s, want [4 4 1 1 2]", got)
	}
	var failures []repository.TaskFailure
	if err := json.Unmarshal(progress[6].([]byte), &failures); err != nil {
		t.Fatalf("失敗の JSON が読めない: %v", err)
	}
	if len(failures) != 2 || failures[0].Target != "vid1" || failures[1].Reason != "timeout" {
		t.Errorf("失敗の記録 = %+v", failures)
	}
	finish := d.lastExec("UPDATE task_runs SET status")
	if finish == nil || !strings.Contains(fmt.Sprint(finish[2]), "成功 1・見送り 1・失敗 2") {
		t.Errorf("完了の文言 = %v", finish)
	}
}

// 失敗の記録は上限で止める（全滅した長い実行で 1 行が数百 KB にならないように）。
// 件数そのものは上限を超えても数え続ける。
func TestTaskRunCapsFailureList(t *testing.T) {
	db, d := newTaskDB(t)
	svc := NewTaskRunService(repository.NewTaskRunRepository(db))
	run, _ := svc.Start(TaskChapterBackfill, nil, nil)
	for i := 0; i < maxTaskFailures+50; i++ {
		run.Fail(fmt.Sprintf("v%d", i), "x")
	}
	run.Finish("done", "")
	progress := d.lastExec("UPDATE task_runs SET total")
	var failures []repository.TaskFailure
	_ = json.Unmarshal(progress[6].([]byte), &failures)
	if len(failures) != maxTaskFailures {
		t.Errorf("失敗の記録が %d 件（上限 %d）", len(failures), maxTaskFailures)
	}
	if failures[0].Target != "v50" || failures[len(failures)-1].Target != fmt.Sprintf("v%d", maxTaskFailures+49) {
		t.Errorf("直近の失敗ではない: first=%s last=%s", failures[0].Target, failures[len(failures)-1].Target)
	}
	if got := fmt.Sprint(progress[5]); got != fmt.Sprint(maxTaskFailures+50) {
		t.Errorf("失敗の件数 = %s, want %d", got, maxTaskFailures+50)
	}
}

// **取得できなかったものを成功に数えない**（BOT 判定は「0 曲埋まった」と件数では
// 区別できず、しかも backfill で最も見たい失敗）。replay 無しは見送り。
func TestRecordChatEndResult(t *testing.T) {
	cases := []struct {
		name string
		res  AnalyzeResult
		err  error
		want string
	}{
		{"エラー", AnalyzeResult{}, errors.New("db"), "failed"},
		{"一時的な失敗（BOT 判定など）", AnalyzeResult{Outcome: chatTransientError}, nil, "failed"},
		{"replay 無し", AnalyzeResult{Outcome: chatNoReplay}, nil, "skipped"},
		{"取得できた（0 曲でも）", AnalyzeResult{Outcome: chatOK}, nil, "succeeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := &TaskRun{svc: &TaskRunService{repo: repository.NewTaskRunRepository(nilDB(t))}}
			run.lastFlush = farFuture // 書き込みは見ない（数だけ）
			recordChatEndResult(run, "v", tc.res, tc.err)
			got := map[string]int{"succeeded": run.succeeded, "skipped": run.skipped, "failed": run.failed}
			for k, v := range got {
				if (k == tc.want) != (v == 1) {
					t.Errorf("%s = %d（want %s だけが 1）", k, v, tc.want)
				}
			}
		})
	}
}

// ---- 偽 driver（送った値まで記録する）----

type taskCall struct {
	query string
	args  []driver.Value
}

type taskDriver struct {
	mu         sync.Mutex
	calls      []taskCall
	failInsert bool
	beforeExec func(string, []driver.Value)
}

func (d *taskDriver) Open(string) (driver.Conn, error) { return &taskConn{d: d}, nil }
func (d *taskDriver) queries() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []string{}
	for _, c := range d.calls {
		out = append(out, c.query)
	}
	return out
}
func (d *taskDriver) lastExec(prefix string) []driver.Value {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := len(d.calls) - 1; i >= 0; i-- {
		if strings.HasPrefix(d.calls[i].query, prefix) {
			return d.calls[i].args
		}
	}
	return nil
}

type taskConn struct{ d *taskDriver }

func (c *taskConn) Prepare(q string) (driver.Stmt, error) { return &taskStmt{d: c.d, q: q}, nil }
func (c *taskConn) Close() error                          { return nil }
func (c *taskConn) Begin() (driver.Tx, error)             { return nil, io.ErrUnexpectedEOF }

type taskStmt struct {
	d *taskDriver
	q string
}

func (s *taskStmt) Close() error  { return nil }
func (s *taskStmt) NumInput() int { return -1 }
func (s *taskStmt) Exec(args []driver.Value) (driver.Result, error) {
	norm := strings.Join(strings.Fields(s.q), " ")
	if s.d.beforeExec != nil {
		s.d.beforeExec(norm, args)
	}
	s.d.mu.Lock()
	fail := s.d.failInsert && strings.HasPrefix(norm, "INSERT INTO task_runs")
	s.d.calls = append(s.d.calls, taskCall{query: norm, args: args})
	s.d.mu.Unlock()
	if fail {
		return nil, errors.New("一時的な失敗")
	}
	return driver.RowsAffected(1), nil
}
func (s *taskStmt) Query([]driver.Value) (driver.Rows, error) { return nil, io.ErrUnexpectedEOF }

// farFuture を lastFlush に入れると、途中の書き込みを飛ばして数だけを見られる。
var farFuture = time.Now().Add(24 * time.Hour)

var taskSeq int

func newTaskDB(t *testing.T) (*sql.DB, *taskDriver) {
	t.Helper()
	d := &taskDriver{}
	taskSeq++
	name := fmt.Sprintf("setori-task-%d", taskSeq)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, d
}

func nilDB(t *testing.T) *sql.DB {
	db, _ := newTaskDB(t)
	return db
}

// 遅い UPDATE が新しい snapshot を上書きしないこと。DB に着く順を意図的に逆転させる。
func TestTaskProgressDoesNotRegress(t *testing.T) {
	db, d := newTaskDB(t)
	svc := NewTaskRunService(repository.NewTaskRunRepository(db))
	run, err := svc.Start(TaskChapterBackfill, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	d.beforeExec = func(q string, args []driver.Value) {
		if strings.HasPrefix(q, "UPDATE task_runs SET total") && args[2] == int64(1) {
			close(entered)
			<-release
		}
	}
	run.mu.Lock()
	run.total, run.done = 2, 1
	run.mu.Unlock()
	first := make(chan struct{})
	go func() { run.flush(true); close(first) }()
	<-entered
	run.mu.Lock()
	run.done = 2
	run.mu.Unlock()
	second := make(chan struct{})
	go func() { run.flush(true); close(second) }()
	select {
	case <-second:
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-first
	<-second
	if got := d.lastExec("UPDATE task_runs SET total")[2]; got != int64(2) {
		t.Fatalf("古い UPDATE が進捗を巻き戻した: done=%v, want 2", got)
	}
}
