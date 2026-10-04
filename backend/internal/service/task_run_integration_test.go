package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/repository"
)

func TestTaskRunPersistence(t *testing.T) {
	db := reviewTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	id, other, user := uuid.New(), uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, role_id) VALUES ($1, 'operator', 'test', (SELECT id FROM roles WHERE name = 'admin'))`, user); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(id, TaskChapterBackfill, map[string]int{"concurrency": 3}, &user); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(other, TaskChatEndBackfill, map[string]int{"concurrency": 1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.Progress(id, 10, 9, 2, 3, 4, []repository.TaskFailure{{Target: "test", Reason: "timeout"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(id, "done", "finished"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != TaskChapterBackfill || got.Status != "done" || got.Total != 10 || got.Done != 9 || got.Succeeded != 2 || got.Skipped != 3 || got.Failed != 4 || got.Message != "finished" || got.StartedBy == nil || *got.StartedBy != "operator" || got.FinishedAt == nil || got.StartedAt.IsZero() || !json.Valid(got.Params) || string(got.Params) != `{"concurrency": 3}` || len(got.Failures) != 1 || got.Failures[0].Reason != "timeout" {
		t.Fatalf("保存値が一致しない: %+v", got)
	}
	running, err := repo.FindByID(other)
	if err != nil || running == nil || running.StartedBy != nil || running.FinishedAt != nil {
		t.Fatalf("NULL の読み取り: %+v %v", running, err)
	}
	missing, err := repo.FindByID(uuid.New())
	if err != nil || missing != nil {
		t.Fatalf("存在しない ID: %+v %v", missing, err)
	}
	// 終了時刻も維持し、running だけを中断する。
	finished := *got.FinishedAt
	if n, err := repo.MarkInterrupted(); err != nil || n != 1 {
		t.Fatalf("中断対象: %d %v", n, err)
	}
	got, err = repo.FindByID(id)
	if err != nil || got.Status != "done" || got.FinishedAt == nil || !got.FinishedAt.Equal(finished) {
		t.Fatalf("完了済みが変わった: %+v %v", got, err)
	}
	running, err = repo.FindByID(other)
	if err != nil || running.Status != "interrupted" || running.FinishedAt == nil || running.Message == "" {
		t.Fatalf("中断の記録: %+v %v", running, err)
	}
	rows, err := repo.List(100)
	if err != nil || len(rows) != 2 {
		t.Fatalf("一覧: %+v %v", rows, err)
	}
	if rows[0].ID != other || rows[1].ID != id {
		t.Fatalf("新しい順ではない: %+v", rows)
	}
}

// AnalyzeStream から Backfill、永続化まで通し、具体的な取得エラーを残す。
func TestChatBackfillPersistsFetchReason(t *testing.T) {
	db := reviewTestDB(t)
	_, err := db.Exec(`INSERT INTO streams (id, title, stream_date, comment_songs) VALUES ('test1234567', 'test', CURRENT_DATE, '[{"start":10,"name":"test"}]')`)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewChatEndService(repository.NewStreamRepository(db), "/missing/review-yt-dlp", t.TempDir())
	tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
	run, err := tasks.Start(TaskChatEndBackfill, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.Backfill(1, run)
	got, err := tasks.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Done != 1 || got.Failed != 1 || got.Succeeded != 0 || len(got.Failures) != 1 || !strings.Contains(got.Failures[0].Reason, "未インストール") {
		t.Fatalf("具体的な取得失敗が残っていない: %+v", got)
	}
	if got.FinishedAt == nil || time.Since(*got.FinishedAt) > time.Minute {
		t.Fatalf("完了時刻: %+v", got)
	}
}

// --ignore-no-formats-error では終了コード 0 の取得失敗もある。空の章節として保存しない。
func TestChapterBackfillDistinguishesTransientFailure(t *testing.T) {
	for _, output := range []string{"", "NA", "null", "[]"} {
		for _, transient := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/transient=%v", output, transient), func(t *testing.T) {
				db := reviewTestDB(t)
				if _, err := db.Exec(`INSERT INTO streams(id,title,stream_date) VALUES('chapter1234','test',NOW())`); err != nil {
					t.Fatal(err)
				}
				stderr := ""
				if transient {
					stderr = "WARNING: Video unavailable. This content isn't available, try again later. Your account has been rate-limited"
				}
				repo := repository.NewStreamRepository(db)
				chat := NewChatEndService(repo, chapterYtdlp(t, output, stderr), t.TempDir())
				chapters := NewChapterService(repo, nil, nil, chat)
				tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
				run, err := tasks.Start(TaskChapterBackfill, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				chapters.Backfill(1, run)
				got, err := tasks.Get(run.ID)
				if err != nil {
					t.Fatal(err)
				}
				var raw []byte
				if err := db.QueryRow(`SELECT chapter_raw FROM streams WHERE id='chapter1234'`).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				if transient {
					if got.Failed != 1 || got.Succeeded != 0 || len(raw) != 0 || len(got.Failures) != 1 || !strings.Contains(got.Failures[0].Reason, "一時的な失敗") {
						t.Fatalf("取得失敗が空章節の成功に化けた: %+v raw=%s", got, raw)
					}
				} else if got.Succeeded != 1 || got.Failed != 0 || string(raw) != "[]" {
					t.Fatalf("正常な空章節: %+v raw=%s", got, raw)
				}
			})
		}
	}
}

// 読める章節が得られていれば、補助取得の警告だけでは失敗にしない。
func TestChapterFetcherKeepsUsablePayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yt-dlp")
	script := `#!/bin/sh
cat <<'CHAPTERS'
[{"start_time":10,"end_time":20,"title":"test"}]
CHAPTERS
cat >&2 <<'WARNING'
WARNING: Auxiliary request failed, try again later
WARNING
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	s := NewChapterService(nil, nil, nil, NewChatEndService(nil, path, t.TempDir()))
	got, err := s.fetchChapters("chapter1234")
	if err != nil || len(got) != 1 || got[0].Start != 10 || got[0].End != 20 || got[0].Title != "test" {
		t.Fatalf("有効な章節を警告で捨てた: %+v %v", got, err)
	}
}

// 本物と同じく --no-warnings は警告を消す。引数を無視する偽物では、
// 警告を読む実装と警告を抑える引数の食い違いを検出できない。
func chapterYtdlp(t *testing.T, stdout, stderr string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	script := "#!/bin/sh\nwarnings=true\nfor arg in \"$@\"; do\n  if [ \"$arg\" = --no-warnings ]; then warnings=false; fi\ndone\n" +
		"cat <<'OUTPUT'\n" + stdout + "\nOUTPUT\n" +
		"if [ \"$warnings\" = true ]; then\ncat >&2 <<'WARNING'\n" + stderr + "\nWARNING\nfi\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

// 最後の snapshot が保存できなければ、件数が欠けた履歴を正常終了にしない。
func TestTaskFinalProgressFailureIsNotDone(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			db := reviewTestDB(t)
			tasks := NewTaskRunService(repository.NewTaskRunRepository(db))
			run, err := tasks.Start(TaskChapterBackfill, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			run.SetTotal(1)
			if reject {
				if _, err := db.Exec(`CREATE FUNCTION reject_task_progress() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.done IS DISTINCT FROM OLD.done THEN RAISE EXCEPTION 'task progress rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_task_progress BEFORE UPDATE ON task_runs FOR EACH ROW EXECUTE FUNCTION reject_task_progress()`); err != nil {
					t.Fatal(err)
				}
			}
			run.Succeed()
			run.Finish("done", "")
			got, err := tasks.Get(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.FinishedAt == nil {
				t.Fatal("終了時刻が無い")
			}
			if reject {
				if got.Status != "failed" || !strings.Contains(got.Message, "進捗の保存に失敗") || !strings.Contains(got.Message, "task progress rejected") {
					t.Fatalf("保存失敗が正常終了に化けた: %+v", got)
				}
			} else if got.Status != "done" || got.Done != 1 || got.Succeeded != 1 || got.Total != 1 {
				t.Fatalf("正常な完了: %+v", got)
			}
			again, err := tasks.Start(TaskChapterBackfill, nil, nil)
			if err != nil {
				t.Fatalf("実行枠が解放されない: %v", err)
			}
			again.Finish("done", "")
		})
	}
}
