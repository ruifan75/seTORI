package repository

import (
	"strings"
	"testing"
)

func TestPreparationQueryScope(t *testing.T) {
	db, d := newRecordingDB(t)
	_, err := NewStreamRepository(db).FindPreparationStreams("owner")
	if err != nil {
		t.Fatal(err)
	}
	const want = `SELECT s.id, s.title, s.is_hidden, s.is_processed, s.chapter_raw
 FROM streams s WHERE s.is_hidden = FALSE AND s.is_processed = FALSE
 AND EXISTS (SELECT 1 FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = $1 AND ss.is_owner = TRUE)
 ORDER BY s.stream_date ASC, s.id ASC`
	queries := d.all()
	if len(queries) != 1 || strings.Join(strings.Fields(queries[0]), " ") != strings.Join(strings.Fields(want), " ") {
		t.Fatalf("scope SQL=%v", queries)
	}
}
