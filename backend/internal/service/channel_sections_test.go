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

// **権限が無ければ非表示の行を引きもしない**（issue #65）。画面で薄字にする・畳む
// だけでは、中身は viewer に届いている。陽性対照として、権限があるときは引くことも見る
// ── 「引かない」だけでは、引く処理そのものが壊れているのと区別できない。
func TestGetGroupedFetchesHiddenOnlyWithPermission(t *testing.T) {
	const hiddenQuery = "WHERE s.is_hidden ORDER BY"
	for _, tc := range []struct {
		includeHidden bool
		wantQuery     bool
	}{
		{false, false},
		{true, true},
	} {
		db, rec := newSectionDB(t)
		svc := &ChannelService{channelRepo: repository.NewChannelRepository(db)}
		resp, err := svc.GetGrouped(tc.includeHidden, false)
		if err != nil {
			t.Fatalf("includeHidden=%v: %v", tc.includeHidden, err)
		}
		queried := false
		for _, q := range rec.all() {
			if strings.Contains(strings.Join(strings.Fields(q), " "), hiddenQuery) {
				queried = true
			}
		}
		if queried != tc.wantQuery {
			t.Errorf("includeHidden=%v: 非表示を引いた=%v, want %v", tc.includeHidden, queried, tc.wantQuery)
		}
		if !tc.includeHidden && resp.Hidden != nil {
			t.Errorf("権限が無いのに Hidden を返している: %v", resp.Hidden)
		}
	}
}

// ---- SQL を記録するだけの偽 driver ----

type sectionDriver struct {
	mu      sync.Mutex
	queries []string
}

func (d *sectionDriver) Open(string) (driver.Conn, error) { return &sectionConn{d: d}, nil }
func (d *sectionDriver) all() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.queries...)
}

type sectionConn struct{ d *sectionDriver }

func (c *sectionConn) Prepare(q string) (driver.Stmt, error) {
	c.d.mu.Lock()
	c.d.queries = append(c.d.queries, q)
	c.d.mu.Unlock()
	return sectionStmt{}, nil
}
func (c *sectionConn) Close() error              { return nil }
func (c *sectionConn) Begin() (driver.Tx, error) { return nil, io.ErrUnexpectedEOF }

type sectionStmt struct{}

func (sectionStmt) Close() error                               { return nil }
func (sectionStmt) NumInput() int                              { return -1 }
func (sectionStmt) Exec([]driver.Value) (driver.Result, error) { return driver.RowsAffected(0), nil }
func (sectionStmt) Query([]driver.Value) (driver.Rows, error)  { return sectionRows{}, nil }

type sectionRows struct{}

func (sectionRows) Columns() []string         { return []string{"c"} }
func (sectionRows) Close() error              { return nil }
func (sectionRows) Next([]driver.Value) error { return io.EOF }

var sectionSeq int

func newSectionDB(t *testing.T) (*sql.DB, *sectionDriver) {
	t.Helper()
	d := &sectionDriver{}
	sectionSeq++
	name := fmt.Sprintf("setori-section-%d", sectionSeq)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, d
}
