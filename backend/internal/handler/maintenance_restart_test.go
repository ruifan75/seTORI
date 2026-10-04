package handler

import (
	"database/sql"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
)

// 終了の DB 書き込みが失敗しても、次の起動を 409 に閉じ込めない。
func TestMaintenanceFinishStorageFailureReleasesBeforeRetry(t *testing.T) {
	for _, kind := range []string{"readings_backfill", "duplicate_scan"} {
		t.Run(kind, func(t *testing.T) {
			a := &maintenanceAI{replies: []string{maintenanceArtistReply, maintenanceSongReply, maintenanceArtistReply, maintenanceSongReply}, errors: make([]error, 4)}
			if kind == "duplicate_scan" {
				a.replies = []string{maintenanceScanReply, maintenanceScanReply}
			}
			r, d := newMaintenanceRouter(t, a)
			d.failFinish = true
			firstID := maintenanceTaskID(t, startMaintenance(t, r, kind))
			deadline := time.Now().Add(2 * time.Second)
			for {
				d.mu.Lock()
				attempted := d.finishAttempts != 0
				if attempted {
					d.failFinish = false
				}
				d.mu.Unlock()
				if attempted {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("finish was not attempted")
				}
				time.Sleep(time.Millisecond)
			}
			var next *httptest.ResponseRecorder
			for time.Now().Before(deadline) {
				next = startMaintenance(t, r, kind)
				if next.Code != 409 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if next == nil {
				t.Fatal("no retry response")
			}
			nextID := maintenanceTaskID(t, next)
			if nextID == firstID {
				t.Fatal("reused task ID")
			}
			second := waitMaintenance(t, r, nextID)
			if second.Status != "done" {
				t.Fatalf("retry=%+v", second)
			}
			d.mu.Lock()
			defer d.mu.Unlock()
			if len(d.bad) != 0 || d.tasks[firstID].Status != "running" {
				t.Fatalf("SQL=%v first=%+v", d.bad, d.tasks[firstID])
			}
		})
	}
}

// メモリの枠は引き継がず、既存の起動時の処理が新しい kind も中断にする。
// 同じ固定 SQL driver で running だけを更新する SQL と完了済みの保持を検査する。
func TestMaintenanceRestartInterruptsNewKindsAndAllowsRetry(t *testing.T) {
	for _, kind := range []string{"readings_backfill", "duplicate_scan"} {
		t.Run(kind, func(t *testing.T) {
			a := &maintenanceAI{replies: []string{maintenanceArtistReply, maintenanceSongReply}, errors: make([]error, 2)}
			if kind == "duplicate_scan" {
				a.replies = []string{maintenanceScanReply}
			}
			r, d := newMaintenanceRouter(t, a)
			actor := uuid.MustParse(maintenanceActor)
			params := map[string]int{}
			if kind == "readings_backfill" {
				params["batch_size"] = 30
			}
			finished, err := r.taskRunService.Start(kind, params, &actor)
			if err != nil {
				t.Fatal(err)
			}
			finished.Finish("done", "already finished")
			first, err := r.taskRunService.Start(kind, params, &actor)
			if err != nil {
				t.Fatal(err)
			}
			first.SetTotal(2)
			first.Succeed()
			first.SetTotal(2) // 最後に保存された進捗を明示的に残してから再起動を模す。
			// 新しいプロセスの TaskRunService に差し替え、起動時と同じ片付けを実行する。
			db := sql.OpenDB(d)
			t.Cleanup(func() { db.Close() })
			r.taskRunService = service.NewTaskRunService(repository.NewTaskRunRepository(db))
			r.taskRunService.MarkInterrupted()
			interrupted := waitMaintenance(t, r, first.ID.String())
			assertMaintenance(t, d, interrupted, "interrupted", [5]int{2, 1, 1, 0, 0}, []repository.TaskFailure{})
			if interrupted.Message != "サーバーの再起動で中断されました" {
				t.Fatalf("message=%q", interrupted.Message)
			}
			done := waitMaintenance(t, r, finished.ID.String())
			if done.Status != "done" || done.Message != "already finished" {
				t.Fatalf("finished changed: %+v", done)
			}
			next := waitMaintenance(t, r, maintenanceTaskID(t, startMaintenance(t, r, kind)))
			if next.Status != "done" {
				t.Fatalf("retry=%+v", next)
			}
		})
	}
}
