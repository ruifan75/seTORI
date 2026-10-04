package repository

import (
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// CreateRun/ListRuns を実行し、DB の channel_id と外部契約の singer_id を別々に固定する。
func TestBatchChannelScopeSQLAndJSON(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	scope := "owner,guest"
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	db := perfAssociationDB(t, []perfAssociationStep{
		{query: "INSERT INTO batch_fill_runs (mode, channel_id, started_by) VALUES ($1, $2, $3) RETURNING id", args: []driver.Value{"force", scope, nil}, rows: [][]driver.Value{{id.String()}}},
		{query: "SELECT b.id, b.mode, b.channel_id, b.status, b.streams_total, b.streams_done, b.songs_created, b.songs_review, b.songs_gap, b.skipped_stream_ids, b.ai_asked, b.message, b.started_at, b.finished_at, u.username FROM batch_fill_runs b LEFT JOIN users u ON u.id = b.started_by ORDER BY b.started_at DESC LIMIT $1", args: []driver.Value{int64(20)}, rows: [][]driver.Value{{id.String(), "force", scope, "done", int64(2), int64(2), int64(1), int64(0), int64(0), "{}", int64(0), "", now, nil, nil}}},
	})
	repo := NewBatchFillRepository(db)
	gotID, err := repo.CreateRun("force", &scope, nil)
	if err != nil || gotID != id {
		t.Fatalf("CreateRun=%s,%v", gotID, err)
	}
	runs, err := repo.ListRuns(20)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(runs)
	if err != nil {
		t.Fatal(err)
	}
	const want = `[{"id":"00000000-0000-0000-0000-000000000002","mode":"force","singer_id":"owner,guest","status":"done","streams_total":2,"streams_done":2,"songs_created":1,"songs_review":0,"songs_gap":0,"skipped_stream_ids":[],"ai_asked":0,"message":"","started_at":"2026-10-04T00:00:00Z"}]`
	if string(data) != want {
		t.Fatalf("scanned run JSON=%s want=%s", data, want)
	}
}
