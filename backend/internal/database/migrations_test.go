package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

var errMigrationProbe = errors.New("migration probe failure")

var migrationProbeFiles = fstest.MapFS{
	"migrations/067_last.sql":   {Data: []byte("CREATE TABLE last_probe (id INTEGER)")},
	"migrations/062_first.sql":  {Data: []byte("CREATE TABLE first_probe (id INTEGER)")},
	"migrations/066_second.sql": {Data: []byte("CREATE TABLE second_probe (id INTEGER)")},
}
var migrationProbeVersions = []string{"062_first.sql", "066_second.sql", "067_last.sql"}

type migrationProbeState struct {
	tables, versions map[string]bool
	files            fstest.MapFS
	events           []string
	fail             string
	began            int
}
type migrationProbeConnector struct{ state *migrationProbeState }

func (c migrationProbeConnector) Connect(context.Context) (driver.Conn, error) {
	return &migrationProbeConn{state: c.state}, nil
}
func (c migrationProbeConnector) Driver() driver.Driver { return migrationProbeDriver{} }

type migrationProbeDriver struct{}

func (migrationProbeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type migrationProbeConn struct {
	state *migrationProbeState
	tx    *migrationProbeTx
}

func (c *migrationProbeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (c *migrationProbeConn) Close() error { return nil }
func (c *migrationProbeConn) Begin() (driver.Tx, error) {
	c.state.began++
	c.state.events = append(c.state.events, "begin")
	if c.state.began == 2 && c.state.fail == "begin" {
		c.state.fail = ""
		return nil, errMigrationProbe
	}
	c.tx = &migrationProbeTx{conn: c, tables: make(map[string]bool), versions: make(map[string]bool), number: c.state.began}
	return c.tx, nil
}
func (c *migrationProbeConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(q, "CREATE TABLE IF NOT EXISTS schema_migrations") {
		return driver.RowsAffected(0), nil
	}
	where := "auto"
	if c.tx != nil {
		where = "tx"
	}
	for path, file := range c.state.files {
		version := strings.TrimPrefix(path, "migrations/")
		if q != string(file.Data) {
			continue
		}
		c.state.events = append(c.state.events, "ddl:"+version+":"+where)
		if c.state.tables[version] {
			return nil, fmt.Errorf("duplicate table for %s", version)
		}
		target := c.state.tables
		if c.tx != nil {
			target = c.tx.tables
		}
		target[version] = true
		if version == migrationProbeVersions[1] && c.state.fail == "ddl" {
			c.state.fail = ""
			return nil, errMigrationProbe
		}
		return driver.RowsAffected(0), nil
	}
	if q == "INSERT INTO schema_migrations (version) VALUES ($1)" && len(args) == 1 {
		version, ok := args[0].Value.(string)
		if !ok {
			return nil, errors.New("invalid version binding")
		}
		c.state.events = append(c.state.events, "record:"+version+":"+where)
		if version == migrationProbeVersions[1] && c.state.fail == "record" {
			c.state.fail = ""
			return nil, errMigrationProbe
		}
		if c.state.versions[version] {
			return nil, errors.New("duplicate version")
		}
		target := c.state.versions
		if c.tx != nil {
			target = c.tx.versions
		}
		target[version] = true
		return driver.RowsAffected(1), nil
	}
	return nil, fmt.Errorf("unexpected SQL: %s; args=%v", q, args)
}
func (c *migrationProbeConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if q != "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)" || len(args) != 1 {
		return nil, fmt.Errorf("unexpected query: %s; args=%v", q, args)
	}
	version, ok := args[0].Value.(string)
	if !ok {
		return nil, errors.New("invalid version binding")
	}
	return &migrationProbeRows{exists: c.state.versions[version]}, nil
}

type migrationProbeRows struct{ exists, read bool }

func (*migrationProbeRows) Columns() []string { return []string{"exists"} }
func (*migrationProbeRows) Close() error      { return nil }
func (r *migrationProbeRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	dest[0] = r.exists
	return nil
}

type migrationProbeTx struct {
	conn             *migrationProbeConn
	tables, versions map[string]bool
	number           int
}

func (tx *migrationProbeTx) Commit() error {
	s := tx.conn.state
	s.events = append(s.events, "commit")
	tx.conn.tx = nil
	fail := tx.number == 2 && (s.fail == "commit" || s.fail == "commit-response")
	if !fail || s.fail == "commit-response" {
		for k := range tx.tables {
			s.tables[k] = true
		}
		for k := range tx.versions {
			s.versions[k] = true
		}
	}
	if fail {
		s.fail = ""
		return errMigrationProbe
	}
	return nil
}
func (tx *migrationProbeTx) Rollback() error {
	tx.conn.state.events = append(tx.conn.state.events, "rollback")
	tx.conn.tx = nil
	return nil
}
func newMigrationProbe(t *testing.T, fail string) (*sql.DB, *migrationProbeState) {
	t.Helper()
	state := &migrationProbeState{tables: make(map[string]bool), versions: make(map[string]bool), files: migrationProbeFiles, fail: fail}
	db := sql.OpenDB(migrationProbeConnector{state})
	// An accidental db.Exec inside the transaction must execute, rather than deadlock.
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { db.Close() })
	return db, state
}
func assertMigrationProbeState(t *testing.T, s *migrationProbeState, done int) {
	t.Helper()
	want := make(map[string]bool)
	for _, v := range migrationProbeVersions[:done] {
		want[v] = true
	}
	if !reflect.DeepEqual(s.tables, want) || !reflect.DeepEqual(s.versions, want) {
		t.Fatalf("tables=%v, records=%v; want both %v", s.tables, s.versions, want)
	}
}
func TestRunMigrationsCommitsSQLAndRecordTogether(t *testing.T) {
	db, state := newMigrationProbe(t, "")
	if err := runMigrations(db, migrationProbeFiles); err != nil {
		t.Fatal(err)
	}
	assertMigrationProbeState(t, state, 3)
	want := []string{}
	for _, v := range migrationProbeVersions {
		want = append(want, "begin", "ddl:"+v+":tx", "record:"+v+":tx", "commit")
	}
	if !reflect.DeepEqual(state.events, want) {
		t.Fatalf("events=%v; want %v", state.events, want)
	}
	before := len(state.events)
	if err := runMigrations(db, migrationProbeFiles); err != nil {
		t.Fatal(err)
	}
	if len(state.events) != before {
		t.Fatalf("applied migrations ran again: %v", state.events[before:])
	}
}
func TestRunMigrationsRetriesOnlyUncommittedFiles(t *testing.T) {
	for _, failure := range []string{"begin", "ddl", "record", "commit", "commit-response"} {
		t.Run(failure, func(t *testing.T) {
			db, state := newMigrationProbe(t, failure)
			err := runMigrations(db, migrationProbeFiles)
			if !errors.Is(err, errMigrationProbe) || !strings.Contains(err.Error(), "066_second.sql") {
				t.Fatalf("got %v; want second migration probe failure", err)
			}
			done := 1
			if failure == "commit-response" {
				done = 2
			}
			assertMigrationProbeState(t, state, done)
			if failure == "ddl" || failure == "record" {
				if got := state.events[len(state.events)-1]; got != "rollback" {
					t.Fatalf("last event=%s; want rollback", got)
				}
			}
			retryFrom := len(state.events)
			if err := runMigrations(db, migrationProbeFiles); err != nil {
				t.Fatal(err)
			}
			assertMigrationProbeState(t, state, 3)
			for _, event := range state.events[retryFrom:] {
				for _, v := range migrationProbeVersions[:done] {
					if strings.Contains(event, v) {
						t.Fatalf("committed file reapplied: %s", event)
					}
				}
			}
		})
	}
}
