package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"reflect"
	"testing"
	"time"
)

type prepareFixture struct {
	events                      []string
	streams                     []models.Stream
	chapterError                bool
	stopAfterChapter            bool
	run                         *TaskRun
	batchReserved, fillReserved bool
	batchRelease, fillRelease   int
	block, released             chan struct{}
}

func (f *prepareFixture) FindPreparationStreams(id string) ([]models.Stream, error) {
	f.events = append(f.events, "list:"+id)
	if f.block != nil {
		<-f.block
	}
	return f.streams, nil
}
func (f *prepareFixture) RefreshChapters(id string) ([]Chapter, error) {
	f.events = append(f.events, "chapter:"+id)
	if f.stopAfterChapter {
		f.run.cancelled.Store(true)
	}
	if f.chapterError {
		return nil, errors.New("BOT")
	}
	return []Chapter{}, nil
}

type prepareBatch struct{ f *prepareFixture }

func (s prepareBatch) Reserve() bool {
	if !s.f.batchReserved {
		return false
	}
	s.f.batchReserved = false
	return true
}
func (s prepareBatch) Release() {
	s.f.batchRelease++
	if s.f.released != nil {
		close(s.f.released)
	}
}
func (s prepareBatch) Cancelled() bool { return false }
func (s prepareBatch) RunPrepared(streams []models.Stream, run *TaskRun, stop func() bool) error {
	s.f.events = append(s.f.events, "analysis")
	if run != s.f.run && s.f.run != nil {
		return errors.New("different task")
	}
	for range streams {
		run.Succeed()
	}
	return nil
}

type prepareFill struct{ f *prepareFixture }

func (s prepareFill) Reserve() bool {
	if !s.f.fillReserved {
		return false
	}
	s.f.fillReserved = false
	return true
}
func (s prepareFill) Release()        { s.f.fillRelease++ }
func (s prepareFill) Cancelled() bool { return false }

func TestPrepareSequenceAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			db, d := newTaskDB(t)
			tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
			run, err := tasks.Start(TaskStreamPrepare, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			f := &prepareFixture{run: run, chapterError: fail, streams: []models.Stream{{ID: "fresh"}, {ID: "cached", ChapterRaw: json.RawMessage(`[]`)}}}
			svc := NewPrepareService(f, f, prepareBatch{f}, prepareFill{f}, tasks)
			svc.execute("owner", run)
			if !reflect.DeepEqual(f.events, []string{"list:owner", "chapter:fresh", "analysis"}) {
				t.Fatalf("順序/取得済みの再取得: %v", f.events)
			}
			p := d.lastExec("UPDATE task_runs SET total")
			want := "[4 4 3 1 0]"
			status := "done"
			if fail {
				want = "[4 4 2 1 1]"
				status = "failed"
			}
			if fmt.Sprint(p[1:6]) != want {
				t.Fatalf("進捗=%v want %s", p, want)
			}
			finish := d.lastExec("UPDATE task_runs SET status")
			if finish[1] != status {
				t.Fatalf("終了=%v", finish)
			}
			if fail {
				var fs []repository.TaskFailure
				json.Unmarshal(p[6].([]byte), &fs)
				if len(fs) != 1 || fs[0].Target != "fresh" || fs[0].Reason != "章節取得: BOT" {
					t.Fatalf("失敗理由=%v", fs)
				}
			}
			phases := []string{}
			for _, c := range d.calls {
				if c.query == "UPDATE task_runs SET phase = $2 WHERE id = $1" {
					if c.args[0] != run.ID.String() {
						t.Fatal("別の実行へ段階を書いた")
					}
					phases = append(phases, c.args[1].(string))
				}
			}
			if !reflect.DeepEqual(phases, []string{"chapters", "analysis"}) {
				t.Fatalf("段階=%v", phases)
			}
			if f.fillRelease != 1 || f.batchRelease != 1 {
				t.Fatalf("枠の解放=%d,%d", f.fillRelease, f.batchRelease)
			}
		})
	}
}
func TestPrepareCancellationStopsBeforeAnalysis(t *testing.T) {
	db, d := newTaskDB(t)
	tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
	run, _ := tasks.Start(TaskStreamPrepare, nil, nil)
	f := &prepareFixture{run: run, stopAfterChapter: true, streams: []models.Stream{{ID: "first"}, {ID: "next"}}}
	svc := NewPrepareService(f, f, prepareBatch{f}, prepareFill{f}, tasks)
	svc.execute("owner", run)
	if !reflect.DeepEqual(f.events, []string{"list:owner", "chapter:first"}) {
		t.Fatalf("停止後も動いた: %v", f.events)
	}
	if finish := d.lastExec("UPDATE task_runs SET status"); finish[1] != "cancelled" {
		t.Fatalf("終了=%v", finish)
	}
}
func TestPrepareReservesAndReleasesOnConflict(t *testing.T) {
	for _, scenario := range []string{"fill", "batch", "chapter", "chat", "insert"} {
		t.Run(scenario, func(t *testing.T) {
			db, d := newTaskDB(t)
			tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
			f := &prepareFixture{batchReserved: true, fillReserved: true}
			if scenario == "fill" {
				f.fillReserved = false
			}
			if scenario == "batch" {
				f.batchReserved = false
			}
			var active *TaskRun
			if scenario == "chapter" || scenario == "chat" {
				kind := TaskChapterBackfill
				if scenario == "chat" {
					kind = TaskChatEndBackfill
				}
				active, _ = tasks.Start(kind, nil, nil)
				defer active.Finish("done", "")
			}
			if scenario == "insert" {
				d.failInsert = true
			}
			svc := NewPrepareService(f, f, prepareBatch{f}, prepareFill{f}, tasks)
			if _, err := svc.Start("owner", nil); err == nil {
				t.Fatal("競合/DB失敗なのに開始した")
			}
			if len(f.events) > 0 {
				t.Fatalf("予約失敗後に入力を触った: %v", f.events)
			}
			fill, batch := 1, 1
			if scenario == "fill" {
				fill, batch = 0, 0
			}
			if scenario == "batch" {
				batch = 0
			}
			if f.fillRelease != fill || f.batchRelease != batch {
				t.Fatalf("解放=%d,%d want %d,%d", f.fillRelease, f.batchRelease, fill, batch)
			}
		})
	}
}
func TestPreparationExcludesBackfillsInBothDirections(t *testing.T) {
	db, _ := newTaskDB(t)
	s := NewTaskRunService(repository.NewTaskRunRepository(db))
	r, _ := s.Start(TaskStreamPrepare, nil, nil)
	for _, kind := range []string{TaskStreamPrepare, TaskChapterBackfill, TaskChatEndBackfill} {
		if _, err := s.Start(kind, nil, nil); !errors.Is(err, ErrTaskRunning) {
			t.Fatalf("準備中に%sが起動: %v", kind, err)
		}
	}
	if err := s.CancelPreparation(r.ID); err != nil || !r.Cancelled() {
		t.Fatal("停止要求が届かない", err)
	}
	r.Finish("cancelled", "")
	for _, kind := range []string{TaskChapterBackfill, TaskChatEndBackfill} {
		other, err := s.Start(kind, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Start(TaskStreamPrepare, nil, nil); !errors.Is(err, ErrTaskRunning) {
			t.Fatal("backfill中の準備が起動", err)
		}
		other.Finish("done", "")
	}
}
func TestPrepareStartPersistsSingerAndRejectsSecondSinger(t *testing.T) {
	db, d := newTaskDB(t)
	tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
	f := &prepareFixture{batchReserved: true, fillReserved: true, streams: []models.Stream{}, block: make(chan struct{}), released: make(chan struct{})}
	svc := NewPrepareService(f, f, prepareBatch{f}, prepareFill{f}, tasks)
	run, err := svc.Start("owner", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start("another", nil); !errors.Is(err, ErrBatchFillAlreadyRunning) {
		t.Fatal("別singerの二重起動", err)
	}
	if err := tasks.CancelPreparation(run.ID); err != nil {
		t.Fatal(err)
	}
	close(f.block)
	select {
	case <-f.released:
	case <-time.After(3 * time.Second):
		t.Fatal("解放されない")
	}
	insert := d.lastExec("INSERT INTO task_runs")
	if insert[1] != TaskStreamPrepare || string(insert[2].([]byte)) != `{"singer_id":"owner"}` {
		t.Fatal("対象を記録しない", insert)
	}
}

type preparedComments struct {
	empty bool
	err   error
}

func (s preparedComments) RefreshCommentRaw(string) (int, error) {
	if s.empty {
		return 0, nil
	}
	return 1, nil
}
func (s preparedComments) AnalyzeCommentsForBatch(string, bool) (*dto.AnalyzeCommentsResponse, error) {
	return nil, s.err
}
func TestPreparedAnalysisRecordsMissingInputAndEmptySeparately(t *testing.T) {
	for _, empty := range []bool{true, false} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			db, d := newTaskDB(t)
			tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
			run, _ := tasks.Start(TaskStreamPrepare, nil, nil)
			b := &BatchAnalyzeService{commentService: preparedComments{empty: empty, err: ErrNoStoredComments}}
			if !b.Reserve() {
				t.Fatal("reserve")
			}
			defer b.Release()
			if err := b.RunPrepared([]models.Stream{{ID: "video"}}, run, nil); err != nil {
				t.Fatal(err)
			}
			run.Finish("done", "")
			p := d.lastExec("UPDATE task_runs SET total")
			want := "[0 1 0 1 0]"
			if !empty {
				want = "[0 1 0 0 1]"
			}
			if fmt.Sprint(p[1:6]) != want {
				t.Fatalf("outcomes=%v want %s", p, want)
			}
			if !empty {
				var fs []repository.TaskFailure
				json.Unmarshal(p[6].([]byte), &fs)
				if len(fs) != 1 || fs[0].Target != "video" || fs[0].Reason != "プレ分析: "+ErrNoStoredComments.Error() {
					t.Fatal("具体的理由が無い", fs)
				}
			}
		})
	}
}

type analyzedComments struct{ response *dto.AnalyzeCommentsResponse }

func (s analyzedComments) RefreshCommentRaw(string) (int, error) { return 1, nil }
func (s analyzedComments) AnalyzeCommentsForBatch(string, bool) (*dto.AnalyzeCommentsResponse, error) {
	return s.response, nil
}
func TestPreparedAnalysisSeparatesCacheFromUnsavedResults(t *testing.T) {
	for _, x := range []struct {
		name, path      string
		saved, deferred bool
		want            batchOutcome
		songs           int
	}{
		{"cache", "cache", false, false, batchOutcomeDone, 1},
		{"saved", "grouped", true, false, batchOutcomeDone, 1},
		{"unsaved nonempty", "grouped", false, false, batchOutcomeDone, -1},
		{"chat pending", "grouped", false, true, batchOutcomeDeferred, 0},
	} {
		t.Run(x.name, func(t *testing.T) {
			b := &BatchAnalyzeService{commentService: analyzedComments{&dto.AnalyzeCommentsResponse{Songs: []dto.CommentSong{{}}, Stats: &dto.AnalyzeStats{Path: x.path, Saved: x.saved}, Deferred: x.deferred}}}
			outcome, songs, reason := b.processOneDetailed("video", false)
			if outcome != x.want || songs != x.songs {
				t.Fatal(outcome, songs, reason)
			}
			if x.songs < 0 && reason != "抽出結果を保存できませんでした" {
				t.Fatal("保存失敗を伝えない", reason)
			}
		})
	}
}
