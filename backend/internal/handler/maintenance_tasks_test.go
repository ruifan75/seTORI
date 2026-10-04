package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
)

var maintenanceIDs = []string{
	"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002",
	"00000000-0000-0000-0000-000000000003", "00000000-0000-0000-0000-000000000004",
	"00000000-0000-0000-0000-000000000005",
}

const maintenanceActor = "10000000-0000-0000-0000-000000000000"

type maintenanceAI struct {
	mu         sync.Mutex
	replies    []string
	errors     []error
	calls      int
	panicValue any
}

func (a *maintenanceAI) SimpleChat(_, _ string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panicValue != nil {
		panic(a.panicValue)
	}
	i := a.calls
	a.calls++
	if i >= len(a.replies) {
		return "", fmt.Errorf("unexpected AI call %d", i)
	}
	return a.replies[i], a.errors[i]
}
func (a *maintenanceAI) count() int { a.mu.Lock(); defer a.mu.Unlock(); return a.calls }

type maintenanceDB struct {
	mu                     sync.Mutex
	tasks                  map[string]repository.TaskRun
	bad                    []string
	gate                   chan struct{}
	entered                chan struct{}
	gateOnce               sync.Once
	failCreate, failFinish bool
	finishAttempts         int
	failQuery, failPhase   string
	failSave               map[string]bool
	existing               map[string]bool
	saved                  []string
	queries                int
	empty                  bool
	shortScan              bool
}

func (d *maintenanceDB) Connect(context.Context) (driver.Conn, error) {
	return &maintenanceConn{d}, nil
}
func (*maintenanceDB) Driver() driver.Driver { return maintenanceDriver{} }

type maintenanceDriver struct{}

func (maintenanceDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type maintenanceConn struct{ d *maintenanceDB }

func (*maintenanceConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (*maintenanceConn) Close() error              { return nil }
func (*maintenanceConn) Begin() (driver.Tx, error) { return maintenanceTx{}, nil }

type maintenanceTx struct{}

func (maintenanceTx) Commit() error   { return nil }
func (maintenanceTx) Rollback() error { return nil }
func maintenanceValues(args []driver.NamedValue) []driver.Value {
	v := make([]driver.Value, len(args))
	for i, a := range args {
		v[i] = a.Value
	}
	return v
}
func (d *maintenanceDB) unexpected(q string, args []driver.NamedValue) error {
	msg := fmt.Sprintf("unexpected SQL/args: %s %v", q, args)
	d.bad = append(d.bad, msg)
	return errors.New(msg)
}
func (c *maintenanceConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	q = strings.Join(strings.Fields(q), " ")
	d := c.d
	d.mu.Lock()
	defer d.mu.Unlock()
	v := maintenanceValues(args)
	switch q {
	case "INSERT INTO task_runs (id, kind, status, params, started_by) VALUES ($1, $2, 'running', $3, $4)":
		if d.failCreate {
			return nil, errors.New("task insert unavailable")
		}
		if len(v) != 4 || v[3] != maintenanceActor {
			return nil, d.unexpected(q, args)
		}
		kind := fmt.Sprint(v[1])
		if kind != "readings_backfill" && kind != "duplicate_scan" {
			return nil, d.unexpected(q, args)
		}
		var params any
		if err := json.Unmarshal(v[2].([]byte), &params); err != nil {
			return nil, err
		}
		if kind == "readings_backfill" && !reflect.DeepEqual(params, map[string]any{"batch_size": float64(30)}) {
			return nil, d.unexpected(q, args)
		}
		if kind == "duplicate_scan" && !reflect.DeepEqual(params, map[string]any{}) {
			return nil, d.unexpected(q, args)
		}
		d.tasks[fmt.Sprint(v[0])] = repository.TaskRun{ID: uuid.MustParse(fmt.Sprint(v[0])), Kind: kind, Status: "running", Params: v[2].([]byte), Failures: []repository.TaskFailure{}, StartedAt: time.Now()}
	case "UPDATE task_runs SET total = $2, done = $3, succeeded = $4, skipped = $5, failed = $6, failures = $7 WHERE id = $1":
		if len(v) != 7 {
			return nil, d.unexpected(q, args)
		}
		r := d.tasks[fmt.Sprint(v[0])]
		r.Total = int(v[1].(int64))
		r.Done = int(v[2].(int64))
		r.Succeeded = int(v[3].(int64))
		r.Skipped = int(v[4].(int64))
		r.Failed = int(v[5].(int64))
		if err := json.Unmarshal(v[6].([]byte), &r.Failures); err != nil {
			return nil, err
		}
		d.tasks[r.ID.String()] = r
	case "UPDATE task_runs SET status = $2, message = $3, finished_at = NOW() WHERE id = $1":
		if len(v) != 3 {
			return nil, d.unexpected(q, args)
		}
		d.finishAttempts++
		if d.failFinish {
			return nil, errors.New("finish storage unavailable")
		}
		r := d.tasks[fmt.Sprint(v[0])]
		r.Status = fmt.Sprint(v[1])
		r.Message = fmt.Sprint(v[2])
		now := time.Now()
		r.FinishedAt = &now
		d.tasks[r.ID.String()] = r
	case "UPDATE task_runs SET status = 'interrupted', finished_at = NOW(), message = CASE WHEN message = '' THEN 'サーバーの再起動で中断されました' ELSE message END WHERE status = 'running'":
		if len(v) != 0 {
			return nil, d.unexpected(q, args)
		}
		var n int64
		for id, r := range d.tasks {
			if r.Status != "running" {
				continue
			}
			r.Status = "interrupted"
			now := time.Now()
			r.FinishedAt = &now
			if r.Message == "" {
				r.Message = "サーバーの再起動で中断されました"
			}
			d.tasks[id] = r
			n++
		}
		return driver.RowsAffected(n), nil
	case "UPDATE task_runs SET phase = $2 WHERE id = $1":
		if len(v) != 2 {
			return nil, d.unexpected(q, args)
		}
		if fmt.Sprint(v[1]) == d.failPhase {
			return nil, errors.New("phase storage unavailable")
		}
		r := d.tasks[fmt.Sprint(v[0])]
		r.Phase = fmt.Sprint(v[1])
		d.tasks[r.ID.String()] = r
	case "UPDATE artists SET name_reading = NULLIF($2, ''), updated_at = NOW() WHERE id = $1", "UPDATE songs SET name_reading = NULLIF($2, ''), updated_at = NOW() WHERE id = $1":
		if len(v) != 2 {
			return nil, d.unexpected(q, args)
		}
		id := fmt.Sprint(v[0])
		// ID が正しくても保存先の表を取り違えたら失敗させる（callback のコピー間違い）。
		artistUpdate := q == "UPDATE artists SET name_reading = NULLIF($2, ''), updated_at = NOW() WHERE id = $1"
		if artistUpdate && id != maintenanceIDs[0] && id != maintenanceIDs[2] {
			return nil, d.unexpected(q, args)
		}
		if !artistUpdate && id != maintenanceIDs[3] && id != maintenanceIDs[4] {
			return nil, d.unexpected(q, args)
		}
		readings := map[string]string{maintenanceIDs[0]: "こう", maintenanceIDs[2]: "へい", maintenanceIDs[3]: "きょくこう", maintenanceIDs[4]: "きょくおつ"}
		if v[1] != readings[id] {
			return nil, d.unexpected(q, args)
		}
		if d.failSave[id] {
			return nil, errors.New("reading storage unavailable")
		}
		d.saved = append(d.saved, id)
	case "UPDATE songs SET original_artist_reading = NULLIF($2, ''), updated_at = NOW() WHERE id IN (SELECT song_id FROM song_artists WHERE artist_id = $1)":
		if len(v) != 2 || (v[0] != maintenanceIDs[0] && v[0] != maintenanceIDs[2]) {
			return nil, d.unexpected(q, args)
		}
	case "INSERT INTO song_merge_candidates (new_song_id, existing_song_id, score, reason, origin) VALUES ($1, $2, $3, $4, $5) ON CONFLICT ON CONSTRAINT song_merge_candidates_pair_unique DO NOTHING":
		if len(v) != 5 || v[4] != "scan" || (v[3] != "same_title" && v[3] != "ai_scan") {
			return nil, d.unexpected(q, args)
		}
		if v[3] == "same_title" && (v[0] != maintenanceIDs[1] || v[1] != maintenanceIDs[0] || v[2] != float64(.5)) {
			return nil, d.unexpected(q, args)
		}
		if v[3] == "ai_scan" && v[2] != float64(0) {
			return nil, d.unexpected(q, args)
		}
		pair := fmt.Sprint(v[0]) + "/" + fmt.Sprint(v[1])
		if d.failSave[pair] {
			return nil, errors.New("candidate storage unavailable")
		}
		d.existing[pair] = true
		d.saved = append(d.saved, pair)
	default:
		return nil, d.unexpected(q, args)
	}
	return driver.RowsAffected(1), nil
}

const maintenanceTaskGet = "SELECT t.id, t.kind, t.status, t.total, t.done, t.succeeded, t.skipped, t.failed, t.params, t.failures, t.message, u.username, t.started_at, t.finished_at, t.phase FROM task_runs t LEFT JOIN users u ON u.id = t.started_by WHERE t.id = $1"
const maintenanceArtistList = "SELECT id, name, name_reading, created_at, updated_at, 0 FROM artists ORDER BY name"
const maintenanceSongList = "SELECT id, name, name_reading FROM songs ORDER BY name"
const maintenanceTitleScan = "SELECT k.name_key, k.song_id FROM song_match_keys k WHERE k.name_key IN ( SELECT name_key FROM song_match_keys GROUP BY name_key HAVING COUNT(*) > 1 ) ORDER BY k.name_key, k.song_id"
const maintenanceScanSongs = "SELECT id, name, original_artist FROM songs ORDER BY created_at"
const maintenanceCandidateExists = "SELECT EXISTS(SELECT 1 FROM song_merge_candidates WHERE (new_song_id = $1 AND existing_song_id = $2) OR (new_song_id = $2 AND existing_song_id = $1))"

func (c *maintenanceConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	q = strings.Join(strings.Fields(q), " ")
	d := c.d
	if q == maintenanceArtistList || q == maintenanceTitleScan {
		d.gateOnce.Do(func() {
			if d.gate != nil {
				close(d.entered)
				<-d.gate
			}
		})
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if q == maintenanceTaskGet {
		if len(args) != 1 {
			return nil, d.unexpected(q, args)
		}
		r, ok := d.tasks[fmt.Sprint(args[0].Value)]
		if !ok {
			return &maintenanceRows{n: 15}, nil
		}
		failures, _ := json.Marshal(r.Failures)
		var finished driver.Value
		if r.FinishedAt != nil {
			finished = *r.FinishedAt
		}
		return &maintenanceRows{n: 15, rows: [][]driver.Value{{r.ID.String(), r.Kind, r.Status, int64(r.Total), int64(r.Done), int64(r.Succeeded), int64(r.Skipped), int64(r.Failed), []byte(r.Params), failures, r.Message, "reviewer", r.StartedAt, finished, r.Phase}}}, nil
	}
	d.queries++
	if q == d.failQuery {
		return nil, errors.New("target query unavailable")
	}
	if q == maintenanceCandidateExists {
		if len(args) != 2 {
			return nil, d.unexpected(q, args)
		}
		a, b := fmt.Sprint(args[0].Value), fmt.Sprint(args[1].Value)
		return &maintenanceRows{n: 1, rows: [][]driver.Value{{d.existing[a+"/"+b] || d.existing[b+"/"+a]}}}, nil
	}
	if len(args) != 0 {
		return nil, d.unexpected(q, args)
	}
	now := time.Now()
	switch q {
	case maintenanceArtistList:
		rows := [][]driver.Value{{maintenanceIDs[0], "歌手甲", nil, now, now, int64(0)}, {maintenanceIDs[1], "歌手乙", nil, now, now, int64(0)}, {maintenanceIDs[2], "歌手丙", nil, now, now, int64(0)}}
		if d.empty {
			rows = nil
		}
		return &maintenanceRows{n: 6, rows: rows}, nil
	case maintenanceSongList:
		rows := [][]driver.Value{{maintenanceIDs[3], "曲甲", nil}, {maintenanceIDs[4], "曲乙", nil}}
		if d.empty {
			rows = nil
		}
		return &maintenanceRows{n: 3, rows: rows}, nil
	case maintenanceTitleScan:
		rows := [][]driver.Value{{"same", maintenanceIDs[0]}, {"same", maintenanceIDs[1]}}
		if d.empty || d.shortScan {
			rows = nil
		}
		return &maintenanceRows{n: 2, rows: rows}, nil
	case maintenanceScanSongs:
		rows := [][]driver.Value{{maintenanceIDs[0], "甲", "原曲甲"}, {maintenanceIDs[1], "乙", "原曲乙"}, {maintenanceIDs[2], "丙", "原曲丙"}}
		if d.empty {
			rows = nil
		}
		if d.shortScan {
			rows = rows[:1]
		}
		return &maintenanceRows{n: 3, rows: rows}, nil
	default:
		return nil, d.unexpected(q, args)
	}
}

type maintenanceRows struct {
	n    int
	rows [][]driver.Value
}

func (r *maintenanceRows) Columns() []string { return make([]string, r.n) }
func (*maintenanceRows) Close() error        { return nil }
func (r *maintenanceRows) Next(dst []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dst, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func newMaintenanceRouter(t *testing.T, a *maintenanceAI) (*Router, *maintenanceDB) {
	t.Helper()
	d := &maintenanceDB{tasks: map[string]repository.TaskRun{}, failSave: map[string]bool{}, existing: map[string]bool{}}
	db := sql.OpenDB(d)
	t.Cleanup(func() { db.Close() })
	r := &Router{taskRunService: service.NewTaskRunService(repository.NewTaskRunRepository(db)), artistService: service.NewArtistService(repository.NewArtistRepository(db), repository.NewSongRepository(db), a), songMatchService: service.NewSongMatchService(repository.NewSongMatchRepository(db), nil, nil, nil), aiService: a}
	return r, d
}
func startMaintenance(t *testing.T, r *Router, kind string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/ai/backfill-readings"
	h := r.handleStartReadingsBackfill
	if kind == "duplicate_scan" {
		path = "/api/songs/merge-candidates/scan"
		h = r.handleStartDuplicateScan
	}
	req := withUser(httptest.NewRequest(http.MethodPost, path, nil), &models.User{ID: uuid.MustParse(maintenanceActor), Permissions: []string{"content:edit"}})
	w := httptest.NewRecorder()
	returned := make(chan struct{})
	go func() { h(w, req); close(returned) }()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("start waited for worker instead of returning 202")
	}
	return w
}
func waitMaintenance(t *testing.T, r *Router, id string) repository.TaskRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+id, nil)
		req.SetPathValue("id", id)
		r.handleGetTask(w, req)
		var task repository.TaskRun
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &task) != nil {
			t.Fatalf("get: %d %s", w.Code, w.Body.String())
		}
		if task.Status != "running" {
			return task
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task did not finish")
	return repository.TaskRun{}
}
func maintenanceTaskID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		ID      string `json:"task_id"`
		Message string `json:"message"`
	}
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Message == "" {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	if _, err := uuid.Parse(body.ID); err != nil {
		t.Fatalf("task_id=%q", body.ID)
	}
	return body.ID
}
func assertMaintenance(t *testing.T, d *maintenanceDB, task repository.TaskRun, status string, counts [5]int, failures []repository.TaskFailure) {
	t.Helper()
	got := [5]int{task.Total, task.Done, task.Succeeded, task.Skipped, task.Failed}
	if task.Status != status || got != counts || !reflect.DeepEqual(task.Failures, failures) || task.FinishedAt == nil {
		t.Fatalf("task=%+v; want %s %v failures=%+v", task, status, counts, failures)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.bad) > 0 {
		t.Fatalf("unexpected SQL: %v", d.bad)
	}
}

const maintenanceArtistReply = `[{"index":0,"reading":"こう","confidence":0.95},{"index":1,"reading":"おつ","confidence":0.1},{"index":2,"reading":"へい","confidence":0.9}]`
const maintenanceSongReply = `[{"index":0,"reading":"きょくこう","confidence":0.95},{"index":1,"reading":"きょくおつ","confidence":0.9}]`
const maintenanceScanReply = `[{"a":0,"b":2},{"a":1,"b":2},{"a":0,"b":1}]`

func TestMaintenanceStartsBeforeWorkAndRejectsDuplicates(t *testing.T) {
	for _, kind := range []string{"readings_backfill", "duplicate_scan"} {
		t.Run(kind, func(t *testing.T) {
			a := &maintenanceAI{replies: []string{maintenanceArtistReply, maintenanceSongReply, maintenanceArtistReply, maintenanceSongReply}, errors: make([]error, 4)}
			if kind == "duplicate_scan" {
				a.replies = []string{maintenanceScanReply, maintenanceScanReply}
			}
			r, d := newMaintenanceRouter(t, a)
			d.gate = make(chan struct{})
			d.entered = make(chan struct{})
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(d.gate) }) })
			id := maintenanceTaskID(t, startMaintenance(t, r, kind))
			select {
			case <-d.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("worker did not start")
			}
			if w := startMaintenance(t, r, kind); w.Code != 409 {
				t.Fatalf("second start=%d %s", w.Code, w.Body.String())
			}
			d.mu.Lock()
			n := len(d.tasks)
			d.mu.Unlock()
			if n != 1 || a.count() != 0 {
				t.Fatalf("reserved too late: runs=%d AI=%d", n, a.count())
			}
			release.Do(func() { close(d.gate) })
			task := waitMaintenance(t, r, id)
			counts := [5]int{5, 5, 4, 1, 0}
			if kind == "duplicate_scan" {
				counts = [5]int{4, 4, 3, 1, 0}
			}
			assertMaintenance(t, d, task, "done", counts, []repository.TaskFailure{})
			// Finish の永続化とメモリ上の枠解放は連続する別操作。解放後の再開始を待つ。
			deadline := time.Now().Add(2 * time.Second)
			var next *httptest.ResponseRecorder
			for time.Now().Before(deadline) {
				next = startMaintenance(t, r, kind)
				if next.Code != 409 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			nextID := maintenanceTaskID(t, next)
			if nextID == id {
				t.Fatal("reused task ID")
			}
			waitMaintenance(t, r, nextID)
		})
	}
}

func TestReadingTaskPartialFailuresAreNotSuccess(t *testing.T) {
	for _, scenario := range []string{"success", "artist-ai-error", "song-ai-error", "parse-error", "save-error", "empty", "query-error", "phase-error", "panic"} {
		t.Run(scenario, func(t *testing.T) {
			a := &maintenanceAI{replies: []string{maintenanceArtistReply, maintenanceSongReply}, errors: make([]error, 2)}
			r, d := newMaintenanceRouter(t, a)
			status, counts, failures := "done", [5]int{5, 5, 4, 1, 0}, []repository.TaskFailure{}
			switch scenario {
			case "artist-ai-error":
				a.errors[0] = errors.New("quota")
				status = "failed"
				counts = [5]int{5, 5, 2, 0, 3}
				for _, id := range maintenanceIDs[:3] {
					failures = append(failures, repository.TaskFailure{Target: "artist:" + id, Reason: "アーティスト名の AI 補完に失敗: ai chat: quota"})
				}
			case "song-ai-error":
				a.errors[1] = errors.New("quota")
				status = "failed"
				counts = [5]int{5, 5, 2, 1, 2}
				for _, id := range maintenanceIDs[3:] {
					failures = append(failures, repository.TaskFailure{Target: "song:" + id, Reason: "曲名の AI 補完に失敗: ai chat: quota"})
				}
			case "parse-error":
				a.replies[0] = "{"
				status = "failed"
				counts = [5]int{5, 5, 2, 0, 3}
				for _, id := range maintenanceIDs[:3] {
					failures = append(failures, repository.TaskFailure{Target: "artist:" + id, Reason: "アーティスト名の AI 補完に失敗: parse ai readings: invalid character ']' looking for beginning of object key string"})
				}
			case "save-error":
				d.failSave[maintenanceIDs[2]] = true
				d.failSave[maintenanceIDs[4]] = true
				status = "failed"
				counts = [5]int{5, 5, 2, 1, 2}
				failures = []repository.TaskFailure{{Target: "artist:" + maintenanceIDs[2], Reason: "読みの保存に失敗: update artist reading: reading storage unavailable"}, {Target: "song:" + maintenanceIDs[4], Reason: "読みの保存に失敗: update song name reading: reading storage unavailable"}}
			case "empty":
				d.empty = true
				counts = [5]int{}
			case "query-error":
				d.failQuery = maintenanceArtistList
				status = "failed"
				counts = [5]int{}
			case "phase-error":
				d.failPhase = "artists"
				status = "failed"
				counts = [5]int{5, 0, 0, 0, 0}
			case "panic":
				a.panicValue = "worker panic"
				status = "failed"
				counts = [5]int{5, 0, 0, 0, 0}
			}
			task := waitMaintenance(t, r, maintenanceTaskID(t, startMaintenance(t, r, "readings_backfill")))
			assertMaintenance(t, d, task, status, counts, failures)
			wantMessage := fmt.Sprintf("読み補完: アーティスト %d 件・曲名 %d 件（失敗 %d 件）", 2, 2, 0)
			switch scenario {
			case "artist-ai-error", "parse-error":
				wantMessage = "読み補完: アーティスト 0 件・曲名 2 件（失敗 3 件）"
			case "song-ai-error":
				wantMessage = "読み補完: アーティスト 2 件・曲名 0 件（失敗 2 件）"
			case "save-error":
				wantMessage = "読み補完: アーティスト 1 件・曲名 1 件（失敗 2 件）"
			case "empty":
				wantMessage = "読み補完: アーティスト 0 件・曲名 0 件（失敗 0 件）"
			case "query-error":
				wantMessage = "失敗: list artists for readings: target query unavailable"
			case "phase-error":
				wantMessage = "失敗: phase storage unavailable"
			case "panic":
				wantMessage = "処理中に panic が発生しました: worker panic"
			}
			if task.Message != wantMessage {
				t.Fatalf("message=%q want=%q", task.Message, wantMessage)
			}
			wantCalls := 2
			if scenario == "empty" || scenario == "query-error" || scenario == "phase-error" || scenario == "panic" {
				wantCalls = 0
			}
			if a.count() != wantCalls {
				t.Fatalf("AI calls=%d want=%d", a.count(), wantCalls)
			}
		})
	}
}

func TestDuplicateScanTaskRecordsFailuresAndCounts(t *testing.T) {
	for _, scenario := range []string{"success", "ai-error", "parse-error", "save-error", "no-pairs", "invalid-pair", "short", "query-error", "target-error"} {
		t.Run(scenario, func(t *testing.T) {
			a := &maintenanceAI{replies: []string{maintenanceScanReply}, errors: make([]error, 1)}
			r, d := newMaintenanceRouter(t, a)
			status, counts, failures, added := "done", [5]int{4, 4, 3, 1, 0}, []repository.TaskFailure{}, 3
			switch scenario {
			case "ai-error":
				a.errors[0] = errors.New("quota")
				status = "failed"
				counts = [5]int{2, 2, 1, 0, 1}
				added = 1
				failures = []repository.TaskFailure{{Target: "AI 走査", Reason: "quota"}}
			case "parse-error":
				a.replies[0] = "{"
				status = "failed"
				counts = [5]int{2, 2, 1, 0, 1}
				added = 1
				failures = []repository.TaskFailure{{Target: "AI 走査", Reason: "AI の応答を解析できませんでした: invalid character ']' looking for beginning of object key string"}}
			case "save-error":
				d.failSave[maintenanceIDs[0]+"/"+maintenanceIDs[2]] = true
				status = "failed"
				counts = [5]int{4, 4, 2, 1, 1}
				added = 2
				failures = []repository.TaskFailure{{Target: maintenanceIDs[0] + " / " + maintenanceIDs[2], Reason: "候補の保存に失敗: record merge candidate: candidate storage unavailable"}}
			case "no-pairs":
				a.replies[0] = "[]"
				counts = [5]int{2, 2, 2, 0, 0}
				added = 1
			case "invalid-pair":
				a.replies[0] = `[{"a":0,"b":2},{"a":0,"b":99}]`
				status = "failed"
				counts = [5]int{3, 3, 2, 0, 1}
				added = 2
				failures = []repository.TaskFailure{{Target: "AI 走査", Reason: "無効な候補の曲番号: 0 / 99"}}
			case "short":
				d.shortScan = true
				counts = [5]int{2, 2, 1, 1, 0}
				added = 0
			case "query-error":
				d.failQuery = maintenanceTitleScan
				status = "failed"
				counts = [5]int{2, 1, 0, 0, 1}
				added = 0
				failures = []repository.TaskFailure{{Target: "曲名キー走査", Reason: "scan duplicate titles: target query unavailable"}}
			case "target-error":
				d.failQuery = maintenanceScanSongs
				status = "failed"
				counts = [5]int{2, 2, 1, 0, 1}
				added = 1
				failures = []repository.TaskFailure{{Target: "AI 走査の対象取得", Reason: "list songs for scan: target query unavailable"}}
			}
			task := waitMaintenance(t, r, maintenanceTaskID(t, startMaintenance(t, r, "duplicate_scan")))
			assertMaintenance(t, d, task, status, counts, failures)
			d.mu.Lock()
			saved := len(d.saved)
			d.mu.Unlock()
			if saved != added {
				t.Fatalf("saved=%d want=%d", saved, added)
			}
			byKey := 1
			if scenario == "short" || scenario == "query-error" {
				byKey = 0
			}
			want := fmt.Sprintf("%d 件の重複候補を追加しました（曲名キー %d / AI %d、失敗 %d 件）", added, byKey, added-byKey, counts[4])
			switch scenario {
			case "ai-error":
				want += " 失敗: AI 呼び出しに失敗しました: quota"
			case "parse-error":
				want += " 失敗: AI の応答を解析できませんでした: invalid character ']' looking for beginning of object key string"
			case "query-error":
				want = "失敗: scan duplicate titles: target query unavailable"
			case "target-error":
				want += " 失敗: list songs for scan: target query unavailable"
			}
			if task.Message != want {
				t.Fatalf("message=%q want=%q", task.Message, want)
			}
			wantCalls := 1
			if scenario == "short" || scenario == "query-error" || scenario == "target-error" {
				wantCalls = 0
			}
			if a.count() != wantCalls {
				t.Fatalf("AI calls=%d want=%d", a.count(), wantCalls)
			}
		})
	}
}

func TestMaintenanceCreateFailureReleasesBeforeRetry(t *testing.T) {
	for _, kind := range []string{"readings_backfill", "duplicate_scan"} {
		t.Run(kind, func(t *testing.T) {
			a := &maintenanceAI{replies: []string{maintenanceArtistReply, maintenanceSongReply}, errors: make([]error, 2)}
			if kind == "duplicate_scan" {
				a.replies[0] = maintenanceScanReply
			}
			r, d := newMaintenanceRouter(t, a)
			d.failCreate = true
			w := startMaintenance(t, r, kind)
			if w.Code != 500 {
				t.Fatalf("start=%d %s", w.Code, w.Body.String())
			}
			d.mu.Lock()
			n, q := len(d.tasks), d.queries
			d.failCreate = false
			d.mu.Unlock()
			if n != 0 || q != 0 || a.count() != 0 {
				t.Fatalf("worker ran before create: runs=%d queries=%d AI=%d", n, q, a.count())
			}
			task := waitMaintenance(t, r, maintenanceTaskID(t, startMaintenance(t, r, kind)))
			if task.Status != "done" {
				t.Fatalf("retry=%+v", task)
			}
		})
	}
}

// 通常完了だけでなく、AI エラー・panic でも予約が戻ることを handler から検査する。
func TestMaintenanceFailureAndPanicReleaseBeforeRetry(t *testing.T) {
	for _, kind := range []string{"readings_backfill", "duplicate_scan"} {
		for _, failure := range []string{"AI error", "panic"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				a := &maintenanceAI{replies: []string{maintenanceArtistReply, maintenanceSongReply, maintenanceArtistReply, maintenanceSongReply}, errors: make([]error, 4)}
				if kind == "duplicate_scan" {
					a.replies = []string{maintenanceScanReply, maintenanceScanReply}
				}
				if failure == "AI error" {
					a.errors[0] = errors.New("quota")
				} else {
					a.panicValue = "worker panic"
				}
				r, d := newMaintenanceRouter(t, a)
				id := maintenanceTaskID(t, startMaintenance(t, r, kind))
				first := waitMaintenance(t, r, id)
				if first.Status != "failed" {
					t.Fatalf("first=%+v", first)
				}
				a.mu.Lock()
				a.panicValue = nil
				a.mu.Unlock()
				deadline := time.Now().Add(2 * time.Second)
				var next *httptest.ResponseRecorder
				for time.Now().Before(deadline) {
					next = startMaintenance(t, r, kind)
					if next.Code != 409 {
						break
					}
					time.Sleep(time.Millisecond)
				}
				nextID := maintenanceTaskID(t, next)
				if nextID == id {
					t.Fatal("retry reused task")
				}
				second := waitMaintenance(t, r, nextID)
				if second.Status != "done" {
					t.Fatalf("retry=%+v", second)
				}
				d.mu.Lock()
				defer d.mu.Unlock()
				if len(d.bad) != 0 {
					t.Fatalf("unexpected SQL: %v", d.bad)
				}
			})
		}
	}
}
