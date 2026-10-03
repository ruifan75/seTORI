package service

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/repository"
)

type batchCommentScript struct {
	responses []*dto.AnalyzeCommentsResponse
	errors    []error
	forces    []bool
}

func (s *batchCommentScript) AnalyzeCommentsForBatch(id string, force bool) (*dto.AnalyzeCommentsResponse, error) {
	i := len(s.forces)
	s.forces = append(s.forces, force)
	if id != "batch123456" || i >= len(s.errors) {
		return nil, fmt.Errorf("unexpected analysis call: %s %v", id, s.forces)
	}
	return s.responses[i], s.errors[i]
}
func batchFixture(t *testing.T, db *sql.DB, script *batchCommentScript) (*BatchFillService, uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO streams(id,title,stream_date,comment_raw) VALUES('batch123456','test',NOW(),'["test"]')`); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewStreamRepository(db)
	runs := repository.NewBatchFillRepository(db)
	id, err := runs.CreateRun(BatchFillModeUnprocessed, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &BatchFillService{streamRepo: repo, runRepo: runs, commentService: script, holodexService: &HolodexService{streamRepo: repo}, running: true, status: dto.BatchFillStatus{Running: true}}
	return s, id
}

// 実際の入力読み込みから最後の UPDATE、ListRuns の Scan まで通す。
func TestBatchFillPersistsActualSkippedStreams(t *testing.T) {
	for _, tc := range []struct {
		name     string
		second   error
		response *dto.AnalyzeCommentsResponse
		skipped  bool
	}{
		{"競合が続く", ErrCommentRawChanged, nil, true},
		{"読み直しが別のエラー", errors.New("db unavailable"), nil, true},
		{"コメントが空に差し替わる", ErrNoStoredComments, nil, false},
		{"読み直しに成功", nil, &dto.AnalyzeCommentsResponse{}, false},
		{"読み直しで live chat 待ち", nil, &dto.AnalyzeCommentsResponse{Deferred: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := reviewTestDB(t)
			script := &batchCommentScript{errors: []error{ErrCommentRawChanged, tc.second}, responses: []*dto.AnalyzeCommentsResponse{nil, tc.response}}
			s, id := batchFixture(t, db, script)
			s.run(id, BatchFillModeUnprocessed, nil, false)
			if !reflect.DeepEqual(script.forces, []bool{false, true}) {
				t.Fatalf("読み直しの呼び出し: %v", script.forces)
			}
			runs, err := s.ListRuns(20)
			if err != nil || len(runs) != 1 {
				t.Fatalf("履歴: %+v %v", runs, err)
			}
			got := runs[0]
			wantDone := 1
			wantIDs := []string{}
			if tc.skipped {
				wantDone = 0
				wantIDs = []string{"batch123456"}
			}
			if got.ID != id || got.Status != "done" || got.StreamsTotal != 1 || got.StreamsDone != wantDone || !reflect.DeepEqual(got.SkippedStreamIDs, wantIDs) || got.FinishedAt == nil || got.StartedByName != nil || got.SingerID != nil {
				t.Fatalf("処理した配信の履歴: %+v", got)
			}
			if tc.skipped && !strings.Contains(got.Message, "飛ばした配信 1") {
				t.Fatalf("見送りの文言: %s", got.Message)
			}
		})
	}
}

func TestBatchFillFinalProgressFailureIsNotDone(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			db := reviewTestDB(t)
			script := &batchCommentScript{errors: []error{nil}, responses: []*dto.AnalyzeCommentsResponse{{Deferred: true}}}
			s, id := batchFixture(t, db, script)
			s.cancelled = cancelled
			// 進捗 UPDATE だけを落とし、status の更新は可能にしておく。
			if _, err := db.Exec(`CREATE FUNCTION reject_progress() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.streams_total IS DISTINCT FROM OLD.streams_total THEN RAISE EXCEPTION 'review progress failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_progress BEFORE UPDATE ON batch_fill_runs FOR EACH ROW EXECUTE FUNCTION reject_progress()`); err != nil {
				t.Fatal(err)
			}
			s.run(id, BatchFillModeUnprocessed, nil, false)
			runs, err := s.ListRuns(20)
			if err != nil || len(runs) != 1 {
				t.Fatalf("履歴: %+v %v", runs, err)
			}
			if runs[0].Status != "failed" || !strings.Contains(runs[0].Message, "進捗の保存に失敗") || runs[0].FinishedAt == nil {
				t.Fatalf("保存失敗が正常終了になった: %+v", runs[0])
			}
		})
	}
}

func TestBatchFillProgressColumnsAndNulls(t *testing.T) {
	db := reviewTestDB(t)
	repo := repository.NewBatchFillRepository(db)
	id, err := repo.CreateRun("force", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateProgress(id, 10, 9, 2, 3, 4, 5, []string{"first", "second"}); err != nil {
		t.Fatal(err)
	}
	runs, err := repo.ListRuns(20)
	if err != nil || len(runs) != 1 {
		t.Fatalf("Scan: %+v %v", runs, err)
	}
	r := runs[0]
	if r.StreamsTotal != 10 || r.StreamsDone != 9 || r.SongsCreated != 2 || r.SongsReview != 3 || r.SongsGap != 4 || r.AIAsked != 5 || !reflect.DeepEqual(r.SkippedStreamIDs, []string{"first", "second"}) || r.FinishedAt != nil || r.StartedByName != nil || r.SingerID != nil {
		t.Fatalf("保存・Scan の列: %+v", r)
	}
	if err := repo.UpdateProgress(id, 10, 9, 2, 3, 4, 5, nil); err != nil {
		t.Fatal(err)
	}
	runs, err = repo.ListRuns(20)
	if err != nil || len(runs) != 1 || !reflect.DeepEqual(runs[0].SkippedStreamIDs, []string{}) {
		t.Fatalf("空配列: %+v %v", runs, err)
	}
}

func TestBatchFillInputErrorsAreSkipped(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		response *dto.AnalyzeCommentsResponse
		skipped  bool
	}{
		{"初回の DB エラー", errors.New("db unavailable"), nil, true},
		{"AI 劣化警告", nil, &dto.AnalyzeCommentsResponse{Warning: "AI unavailable"}, true},
		{"コメントなしは正常", ErrNoStoredComments, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := reviewTestDB(t)
			script := &batchCommentScript{errors: []error{tc.err}, responses: []*dto.AnalyzeCommentsResponse{tc.response}}
			s, id := batchFixture(t, db, script)
			s.run(id, BatchFillModeUnprocessed, nil, false)
			runs, err := s.ListRuns(20)
			if err != nil || len(runs) != 1 {
				t.Fatalf("履歴: %+v %v", runs, err)
			}
			wantDone := 1
			wantIDs := []string{}
			if tc.skipped {
				wantDone = 0
				wantIDs = []string{"batch123456"}
			}
			if runs[0].StreamsDone != wantDone || !reflect.DeepEqual(runs[0].SkippedStreamIDs, wantIDs) {
				t.Fatalf("入力の取得結果と履歴が不一致: %+v", runs[0])
			}
		})
	}
}
