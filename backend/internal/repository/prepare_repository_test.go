package repository

import (
	"database/sql/driver"
	"reflect"
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
 AND EXISTS (SELECT 1 FROM stream_channels ss WHERE ss.stream_id = s.id AND ss.channel_id = $1 AND ss.is_owner = TRUE)
 AND NOT EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = s.id AND mt.tag_id = 'members_only')
 AND s.restriction_override IS DISTINCT FROM TRUE
 ORDER BY s.stream_date ASC, s.id ASC`
	queries := d.all()
	if len(queries) != 1 || strings.Join(strings.Fields(queries[0]), " ") != strings.Join(strings.Fields(want), " ") {
		t.Fatalf("scope SQL=%v", queries)
	}
	if !reflect.DeepEqual(d.bindings, [][]driver.Value{{"owner"}}) {
		t.Fatal(d.bindings)
	}
}

// 実際に発行した SQL・引数と bool の Scan を検査する。条件の SQL 評価は専用 DB で行う。
type preparationScopeDriver struct {
	recordingDriver
	value bool
}

func (d *preparationScopeDriver) Open(string) (driver.Conn, error) {
	return &preparationScopeConn{recordingConn{d: &d.recordingDriver}, d}, nil
}

type preparationScopeConn struct {
	recordingConn
	scope *preparationScopeDriver
}

func (c *preparationScopeConn) Prepare(q string) (driver.Stmt, error) {
	c.d.record(q)
	return &preparationScopeStmt{recordingStmt{query: q, d: c.d}, c.scope.value}, nil
}

type preparationScopeStmt struct {
	recordingStmt
	value bool
}

func (s *preparationScopeStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.d.bindings = append(s.d.bindings, append([]driver.Value(nil), args...))
	return &streamRestrictionValueRows{values: []driver.Value{s.value}}, nil
}
func TestPreparationRecheckSQLAndArguments(t *testing.T) {
	const want = `SELECT EXISTS (SELECT 1 FROM streams s WHERE s.is_hidden = FALSE AND s.is_processed = FALSE
 AND EXISTS (SELECT 1 FROM stream_channels ss WHERE ss.stream_id = s.id AND ss.channel_id = $1 AND ss.is_owner = TRUE)
 AND NOT EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = s.id AND mt.tag_id = 'members_only')
 AND s.restriction_override IS DISTINCT FROM TRUE AND s.id = $2)`
	for _, value := range []bool{false, true} {
		d := &preparationScopeDriver{value: value}
		db := openRestrictionTestDB(t, d, "")
		got, err := NewStreamRepository(db).PreparationStreamEligible("owner", "video")
		if err != nil || got != value {
			t.Fatal(got, err)
		}
		queries := d.all()
		if len(queries) != 1 || strings.Join(strings.Fields(queries[0]), " ") != strings.Join(strings.Fields(want), " ") {
			t.Fatalf("recheck SQL=%v", queries)
		}
		if !reflect.DeepEqual(d.bindings, [][]driver.Value{{"owner", "video"}}) {
			t.Fatal(d.bindings)
		}
	}
}
