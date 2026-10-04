package service

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/repository"
)

// **飛ばした配信を履歴へ書く**（issue #7）。メモリ上の状態にしか無かったので、
// 再起動か次の実行で消えていた。実際に保存を呼び、送った値を見る。
func TestSaveProgressWritesSkippedStreams(t *testing.T) {
	for _, tc := range []struct {
		name    string
		skipped []string
		want    string
	}{
		{"飛ばした配信あり", []string{"aaa", "bbb"}, `{"aaa","bbb"}`},
		// NOT NULL の列へ NULL を書かない（書くと保存自体が落ちる）
		{"飛ばした配信なし", nil, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, rec := newSkipDB(t)
			s := &BatchFillService{runRepo: repository.NewBatchFillRepository(db)}
			s.status = dto.BatchFillStatus{Done: 1, SkippedIDs: tc.skipped}
			s.saveProgress(uuid.New(), 3, 2, 1, 0, 0)

			calls := rec.execs("UPDATE batch_fill_runs")
			if len(calls) != 1 {
				t.Fatalf("進捗の保存が %d 回（1 回のはず）", len(calls))
			}
			args := calls[0]
			if len(args) != 8 {
				t.Fatalf("引数が %d 個: %v", len(args), args)
			}
			if got := fmt.Sprint(args[7]); got != tc.want {
				t.Errorf("skipped_stream_ids = %s, want %s", got, tc.want)
			}
			// 処理済み件数も状態から取る（呼び出し側が数え直さない）
			if got := fmt.Sprint(args[2]); got != "1" {
				t.Errorf("streams_done = %s, want 1", got)
			}
		})
	}
}

// **対象が 1 件も第 3 段へ進まない実行でも、最後に必ず保存する。**
// 以前は第 3 段のループの中でしか保存していなかったので、全部飛ばされた実行が
// `streams_total = 0, status = done` になり、何もしなかった実行と区別できなかった。
func TestRunSavesProgressEvenWithoutWrites(t *testing.T) {
	db, rec := newSkipDB(t)
	s := &BatchFillService{
		streamRepo: repository.NewStreamRepository(db),
		runRepo:    repository.NewBatchFillRepository(db),
	}
	// 第 1 段で飛ばした配信がある状態を作る（run は状態を初期化しない。初期化は start）。
	s.status.SkippedIDs = []string{"xyz"}
	s.run(uuid.New(), BatchFillModeUnprocessed, nil, false)

	// 完了の文言にも件数を出す（履歴の一覧は文言を title に出している）。
	finish := rec.execs("UPDATE batch_fill_runs SET status")
	if len(finish) == 1 && !strings.Contains(fmt.Sprint(finish[0][2]), "飛ばした配信 1") {
		t.Errorf("完了の文言に飛ばした配信の件数が無い: %v", finish[0][2])
	}

	if n := len(rec.execs("UPDATE batch_fill_runs SET streams_total")); n == 0 {
		// 進捗の UPDATE は「SET streams_total = …」で始まる（空白は詰めて比べる）
		t.Errorf("進捗を一度も保存していない: %q", rec.all())
	}
	if n := len(rec.execs("UPDATE batch_fill_runs SET status")); n != 1 {
		t.Errorf("完了の記録が %d 回: %q", n, rec.all())
	}
}

// ---- 送った値まで記録する偽 driver ----

type skipCall struct {
	query string
	args  []driver.Value
}

type skipDriver struct {
	mu    sync.Mutex
	calls []skipCall
}

func (d *skipDriver) Open(string) (driver.Conn, error) { return &skipConn{d: d}, nil }
func (d *skipDriver) record(q string, args []driver.Value) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, skipCall{query: strings.Join(strings.Fields(q), " "), args: args})
}
func (d *skipDriver) all() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.calls))
	for i, c := range d.calls {
		out[i] = c.query
	}
	return out
}

// execs は prefix で始まる文の引数を返す（空白は詰めて比べる）。
func (d *skipDriver) execs(prefix string) [][]driver.Value {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out [][]driver.Value
	for _, c := range d.calls {
		if strings.HasPrefix(c.query, prefix) {
			out = append(out, c.args)
		}
	}
	return out
}

type skipConn struct{ d *skipDriver }

func (c *skipConn) Prepare(q string) (driver.Stmt, error) { return &skipStmt{d: c.d, q: q}, nil }
func (c *skipConn) Close() error                          { return nil }
func (c *skipConn) Begin() (driver.Tx, error)             { return nil, io.ErrUnexpectedEOF }

type skipStmt struct {
	d *skipDriver
	q string
}

func (s *skipStmt) Close() error  { return nil }
func (s *skipStmt) NumInput() int { return -1 }
func (s *skipStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.d.record(s.q, args)
	return driver.RowsAffected(1), nil
}
func (s *skipStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.d.record(s.q, args)
	return skipRows{}, nil
}

type skipRows struct{}

func (skipRows) Columns() []string         { return []string{"id", "title"} }
func (skipRows) Close() error              { return nil }
func (skipRows) Next([]driver.Value) error { return io.EOF }

var skipSeq int

func newSkipDB(t *testing.T) (*sql.DB, *skipDriver) {
	t.Helper()
	d := &skipDriver{}
	skipSeq++
	name := fmt.Sprintf("setori-skip-%d", skipSeq)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, d
}
