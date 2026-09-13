package repository

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// 発行された SQL を記録するだけの driver。
//
// **値を固定するだけでは足りない。** `streamListFilter` の返り値を完全一致で
// 固定しても、呼び出し側がそれを WHERE に使っているかは別の話で、
// `WHERE streamListFilter(...) OR TRUE` や、呼び出しを `TRUE` に置き換える
// 改変は素通りしていた（実測）。かといってソースを読む検査に戻ると、
// 前後の語だけ合わせた改変を取りこぼす（3 回そうなった）。
//
// **実際に呼んで、出てきた SQL を見る**のが両方を避ける唯一の形。
type recordingDriver struct {
	mu      sync.Mutex
	queries []string
}

func (d *recordingDriver) Open(string) (driver.Conn, error) { return &recordingConn{d: d}, nil }

func (d *recordingDriver) record(q string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queries = append(d.queries, q)
}

func (d *recordingDriver) all() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.queries...)
}

type recordingConn struct{ d *recordingDriver }

func (c *recordingConn) Prepare(q string) (driver.Stmt, error) {
	c.d.record(q)
	return &recordingStmt{query: q}, nil
}
func (c *recordingConn) Close() error              { return nil }
func (c *recordingConn) Begin() (driver.Tx, error) { return nil, io.ErrUnexpectedEOF }

type recordingStmt struct{ query string }

func (s *recordingStmt) Close() error  { return nil }
func (s *recordingStmt) NumInput() int { return -1 }
func (s *recordingStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, io.ErrUnexpectedEOF
}

// 件数は 1 行返さないと呼び出し側が早退して一覧の SQL が出てこない。
func (s *recordingStmt) Query([]driver.Value) (driver.Rows, error) {
	if strings.Contains(s.query, "COUNT(*)") {
		return &oneIntRow{}, nil
	}
	return &noRows{}, nil
}

type oneIntRow struct{ done bool }

func (r *oneIntRow) Columns() []string { return []string{"count"} }
func (r *oneIntRow) Close() error      { return nil }
func (r *oneIntRow) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = int64(0)
	return nil
}

type noRows struct{}

func (noRows) Columns() []string         { return nil }
func (noRows) Close() error              { return nil }
func (noRows) Next([]driver.Value) error { return io.EOF }

var recordingSeq int

func newRecordingDB(t *testing.T) (*sql.DB, *recordingDriver) {
	t.Helper()
	d := &recordingDriver{}
	recordingSeq++
	name := fmt.Sprintf("setori-recording-%d", recordingSeq)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, d
}

// whereClause は SQL の**一番外側**の WHERE 句を取り出して空白を詰める。
//
// 単純に最初の "WHERE" を探すと、SELECT の列にある副問い合わせ
// （`EffectiveRestrictedExpr` は入れ子の WHERE を持つ）に当たる。
// 括弧の深さ 0 のものだけを見ること。
func whereClause(t *testing.T, sqlText string) string {
	t.Helper()
	norm := strings.Join(strings.Fields(sqlText), " ")
	upper := strings.ToUpper(norm)

	depth, start := 0, -1
	for i := 0; i < len(norm); i++ {
		switch norm[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 || start >= 0 {
			continue
		}
		if strings.HasPrefix(upper[i:], "WHERE ") && (i == 0 || norm[i-1] == ' ') {
			start = i
		}
	}
	if start < 0 {
		return ""
	}

	rest, restUpper := norm[start:], upper[start:]
	depth = 0
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 {
			continue
		}
		for _, stop := range []string{" ORDER BY ", " LIMIT ", " GROUP BY "} {
			if strings.HasPrefix(restUpper[i:], stop) {
				return strings.TrimSpace(rest[:i])
			}
		}
	}
	return strings.TrimSpace(rest)
}

// 一覧を返す経路が、**件数と一覧の両方で** streamListFilter の値をそのまま
// WHERE に使っていることを、実際に発行された SQL で確かめる。
//
// **WHERE 句を丸ごと期待値と突き合わせる。** 「含むか」だけだと、
// 後ろに `OR TRUE` を足す / 呼び出しを `TRUE` に差し替える、といった
// 「呼び出しは在るが効いていない」改変が通る（実測）。
func TestStreamListsApplyFilterInBothQueries(t *testing.T) {
	cases := []struct {
		name                string
		call                func(*StreamRepository)
		wantCount, wantList string
	}{
		{
			name:      "FindAll",
			call:      func(r *StreamRepository) { r.FindAll(20, 0, false, "", "") },
			wantCount: "WHERE " + streamListFilter("streams", false),
			wantList:  "WHERE " + streamListFilter("streams", false),
		},
		{
			name:      "FindByTagID",
			call:      func(r *StreamRepository) { r.FindByTagID("singing", 20, 0) },
			wantCount: "WHERE sst.tag_id = $1 AND " + streamListFilter("s", false),
			wantList:  "WHERE sst.tag_id = $1 AND " + streamListFilter("s", false),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, rec := newRecordingDB(t)
			tc.call(NewStreamRepository(db))

			var count, list string
			for _, q := range rec.all() {
				if strings.Contains(q, "COUNT(*)") {
					count = q
				} else if strings.Contains(strings.ToUpper(q), "SELECT") {
					list = q
				}
			}
			if count == "" || list == "" {
				t.Fatalf("件数と一覧の両方が発行されていない: %q", rec.all())
			}

			for label, pair := range map[string][2]string{
				"件数": {whereClause(t, count), strings.Join(strings.Fields(tc.wantCount), " ")},
				"一覧": {whereClause(t, list), strings.Join(strings.Fields(tc.wantList), " ")},
			} {
				if pair[0] != pair[1] {
					t.Errorf("%s の WHERE が期待と違う\n got: %q\nwant: %q", label, pair[0], pair[1])
				}
			}
		})
	}
}
