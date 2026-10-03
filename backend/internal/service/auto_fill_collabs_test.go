package service

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/ruifan75/setori/internal/repository"
)

// 客串を含めるのは**オプトイン**（issue #60）。deploy しただけで対象が増えてはいけない。
func TestAutoFillCollabsDefaultsOff(t *testing.T) {
	if defaultAutoFillSettings().IncludeCollabs {
		t.Error("既定で客串を含めている。明示的に有効にしない限り対象を広げない")
	}
}

// **取り直しが設定の値をそのまま repository へ渡すこと。** 渡し忘れると、
// 一括作成だけ客串へ広がり、取り直していない入力を見続ける。
func TestRefreshCommentsPassesIncludeCollabs(t *testing.T) {
	for _, collabs := range []bool{false, true} {
		db, rec := newCollabDB(t)
		s := &AutoFillService{streamRepo: repository.NewStreamRepository(db)}
		s.refreshComments([]string{"UC1"}, 30, nil, collabs)

		issued := rec.all()
		if len(issued) != 1 {
			t.Fatalf("1 本のはずが %d 本: %q", len(issued), issued)
		}
		ownerOnly := strings.Contains(issued[0], "ss.is_owner")
		if ownerOnly == collabs {
			t.Errorf("collabs=%v なのに所有者だけ=%v で絞っている: %s", collabs, ownerOnly, issued[0])
		}
	}
}

// ---- SQL を記録するだけの偽 driver（行は返さない）----

type collabDriver struct {
	mu      sync.Mutex
	queries []string
}

func (d *collabDriver) Open(string) (driver.Conn, error) { return &collabConn{d: d}, nil }
func (d *collabDriver) all() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.queries...)
}

type collabConn struct{ d *collabDriver }

func (c *collabConn) Prepare(q string) (driver.Stmt, error) {
	c.d.mu.Lock()
	c.d.queries = append(c.d.queries, q)
	c.d.mu.Unlock()
	return collabStmt{}, nil
}
func (c *collabConn) Close() error              { return nil }
func (c *collabConn) Begin() (driver.Tx, error) { return nil, io.ErrUnexpectedEOF }

type collabStmt struct{}

func (collabStmt) Close() error                               { return nil }
func (collabStmt) NumInput() int                              { return -1 }
func (collabStmt) Exec([]driver.Value) (driver.Result, error) { return driver.RowsAffected(0), nil }
func (collabStmt) Query([]driver.Value) (driver.Rows, error)  { return collabRows{}, nil }

type collabRows struct{}

func (collabRows) Columns() []string         { return []string{"id"} }
func (collabRows) Close() error              { return nil }
func (collabRows) Next([]driver.Value) error { return io.EOF }

var collabSeq int

func newCollabDB(t *testing.T) (*sql.DB, *collabDriver) {
	t.Helper()
	d := &collabDriver{}
	collabSeq++
	name := fmt.Sprintf("setori-collab-%d", collabSeq)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, d
}
