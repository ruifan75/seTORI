package repository

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 実際の repository を呼び、発行SQL全文・引数・commit/rollbackを順に検査する。
// DB の候補判定の評価はしない。SQLの変更を検出する範囲のテスト。
type visibilityStep struct {
	kind, query string
	args        []driver.Value
	rows        [][]driver.Value
	affected    int64
}
type visibilityDriver struct {
	t     *testing.T
	steps []visibilityStep
	index int
	calls [][]driver.Value
}

func normVisibility(q string) string { return strings.Join(strings.Fields(q), " ") }
func (d *visibilityDriver) take(kind, q string, args []driver.Value) visibilityStep {
	d.t.Helper()
	if d.index >= len(d.steps) {
		d.t.Errorf("予期しない%s: %s", kind, q)
		return visibilityStep{}
	}
	d.calls = append(d.calls, append([]driver.Value(nil), args...))
	step := d.steps[d.index]
	d.index++
	if step.kind != kind || normVisibility(step.query) != normVisibility(q) {
		d.t.Errorf("step%d %s\n got %s\nwant %s (%s)", d.index, kind, normVisibility(q), normVisibility(step.query), step.kind)
	}
	if step.args != nil && !reflect.DeepEqual(args, step.args) {
		d.t.Errorf("step%d args=%v want %v", d.index, args, step.args)
	}
	return step
}
func (d *visibilityDriver) Open(string) (driver.Conn, error) { return &visibilityConn{d}, nil }

type visibilityConn struct{ d *visibilityDriver }

func (c *visibilityConn) Prepare(q string) (driver.Stmt, error) { return &visibilityStmt{c.d, q}, nil }
func (c *visibilityConn) Close() error                          { return nil }
func (c *visibilityConn) Begin() (driver.Tx, error) {
	c.d.take("begin", "", nil)
	return &visibilityTx{c.d}, nil
}

type visibilityTx struct{ d *visibilityDriver }

func (x *visibilityTx) Commit() error   { x.d.take("commit", "", nil); return nil }
func (x *visibilityTx) Rollback() error { x.d.take("rollback", "", nil); return nil }

type visibilityStmt struct {
	d *visibilityDriver
	q string
}

func (s *visibilityStmt) Close() error  { return nil }
func (s *visibilityStmt) NumInput() int { return -1 }
func (s *visibilityStmt) Exec(args []driver.Value) (driver.Result, error) {
	step := s.d.take("exec", s.q, args)
	return driver.RowsAffected(step.affected), nil
}
func (s *visibilityStmt) Query(args []driver.Value) (driver.Rows, error) {
	step := s.d.take("query", s.q, args)
	return &visibilityRows{rows: step.rows}, nil
}

type visibilityRows struct {
	rows  [][]driver.Value
	index int
}

func (r *visibilityRows) Columns() []string {
	if len(r.rows) == 0 {
		return []string{"id"}
	}
	c := make([]string, len(r.rows[0]))
	for i := range c {
		c[i] = fmt.Sprint(i)
	}
	return c
}
func (r *visibilityRows) Close() error { return nil }
func (r *visibilityRows) Next(dst []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dst, r.rows[r.index])
	r.index++
	return nil
}

var visibilitySeq int

func visibilityDB(t *testing.T, steps []visibilityStep) (*VisibilityReviewRepository, *visibilityDriver) {
	t.Helper()
	d := &visibilityDriver{t: t, steps: steps}
	visibilitySeq++
	name := fmt.Sprintf("visibility-%d", visibilitySeq)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if d.index != len(d.steps) {
			t.Errorf("SQL/transaction が未実行: %d/%d", d.index, len(d.steps))
		}
	})
	return NewVisibilityReviewRepository(db), d
}

const visibilityWantWhere = `s.is_hidden = TRUE AND s.duration_seconds > $2
 AND EXISTS (SELECT 1 FROM stream_stream_tags t WHERE t.stream_id = s.id AND t.tag_id = ANY($1))
 AND NOT EXISTS (SELECT 1 FROM non_singing_checks c WHERE c.stream_id = s.id)`
const visibilityMusicArg = `{"concert","karaoke","music_cover","mv","original_song","singing"}`

var visibilityID = uuid.MustParse("01234567-89ab-cdef-0123-456789abcdef")
var visibilityIDArgs = []driver.Value{visibilityID.String()}

func TestVisibilityCandidateQuery(t *testing.T) {
	for _, dismissed := range []bool{false, true} {
		t.Run(fmt.Sprint(dismissed), func(t *testing.T) {
			where := visibilityWantWhere
			args := []driver.Value{visibilityMusicArg, int64(180)}
			limit, offset := "$3", "$4"
			if dismissed {
				where = `EXISTS (SELECT 1 FROM non_singing_checks c WHERE c.stream_id = s.id)`
				args = []driver.Value{}
				limit, offset = "$1", "$2"
			}
			r, _ := visibilityDB(t, []visibilityStep{
				{kind: "query", query: `SELECT COUNT(*) FROM streams s WHERE ` + where, args: args, rows: [][]driver.Value{{int64(1)}}},
				{kind: "query", query: `SELECT s.id, s.title, s.stream_date, COALESCE(s.duration_seconds, 0),
 COALESCE((SELECT array_agg(t.tag_id ORDER BY t.tag_id) FROM stream_stream_tags t WHERE t.stream_id = s.id), '{}')
 FROM streams s WHERE ` + where + ` ORDER BY s.stream_date DESC, s.id ASC LIMIT ` + limit + ` OFFSET ` + offset, args: append(append([]driver.Value{}, args...), int64(100), int64(200)), rows: [][]driver.Value{{"v", "歌枠", time.Unix(1, 0), int64(181), `{singing,members_only}`}}},
			})
			rows, count, err := r.Candidates(dismissed, 100, 200)
			if err != nil || count != 1 || len(rows) != 1 || rows[0].Duration != 181 || !reflect.DeepEqual(rows[0].Tags, []string{"singing", "members_only"}) {
				t.Fatalf("list=%v count=%d err=%v", rows, count, err)
			}
		})
	}
}
func TestVisibilityPreviewWritesOnlyAudit(t *testing.T) {
	for _, n := range []int64{2, 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			end := "commit"
			if n != 2 {
				end = "rollback"
			}
			r, d := visibilityDB(t, []visibilityStep{
				{kind: "begin"},
				{kind: "exec", query: `INSERT INTO visibility_review_runs (id, status, item_count, started_by) VALUES ($1, 'preview', $2, $3)`, affected: 1},
				{kind: "exec", query: `INSERT INTO visibility_review_items (run_id, stream_id, stream_title, before_hidden, after_hidden, before_updated_at)
 SELECT $3, s.id, s.title, s.is_hidden, FALSE, s.updated_at FROM streams s WHERE ` + visibilityWantWhere + ` AND s.id = ANY($4)`, affected: n},
				{kind: end},
			})
			id, err := r.Preview([]string{"one", "two"}, &visibilityID)
			if n == 2 {
				if err != nil || id == uuid.Nil {
					t.Fatal(id, err)
				}
			} else if !errors.Is(err, ErrVisibilityConflict) {
				t.Fatal("一部だけの候補を受け付けた", err)
			}
			// 可変のrun ID以外の引数も固定する。
			generated := d.calls[1][0]
			if _, err := uuid.Parse(generated.(string)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.calls[1][1:], []driver.Value{int64(2), visibilityID.String()}) {
				t.Fatal("runの対象数/操作者", d.calls[1])
			}
			if !reflect.DeepEqual(d.calls[2], []driver.Value{visibilityMusicArg, int64(180), generated, `{"one","two"}`}) {
				t.Fatal("退避対象の引数", d.calls[2])
			}
		})
	}
}

const visibilityWantLock = `SELECT status, item_count FROM visibility_review_runs WHERE id = $1 FOR UPDATE`
const visibilityWantApplyLock = `SELECT s.id FROM streams s JOIN visibility_review_items i ON i.stream_id = s.id
 WHERE ` + visibilityWantWhere + ` AND i.run_id = $3
 AND s.is_hidden = i.before_hidden AND s.updated_at = i.before_updated_at
 ORDER BY s.id ASC FOR UPDATE OF s`
const visibilityWantApply = `WITH changed AS (
 UPDATE streams s SET is_hidden = i.after_hidden, updated_at = NOW()
 FROM visibility_review_items i WHERE i.run_id = $1 AND i.stream_id = s.id
 AND s.is_hidden = i.before_hidden AND s.updated_at = i.before_updated_at
 AND s.is_hidden = TRUE AND s.duration_seconds > $3
 AND EXISTS (SELECT 1 FROM stream_stream_tags t WHERE t.stream_id = s.id AND t.tag_id = ANY($2))
 AND NOT EXISTS (SELECT 1 FROM non_singing_checks c WHERE c.stream_id = s.id)
 RETURNING s.id, s.updated_at)
 UPDATE visibility_review_items i SET after_updated_at = c.updated_at
 FROM changed c WHERE i.run_id = $1 AND i.stream_id = c.id`
const visibilityWantRevert = `WITH restored AS (
 UPDATE streams s SET is_hidden = i.before_hidden, updated_at = NOW()
 FROM visibility_review_items i WHERE i.run_id = $1 AND i.stream_id = s.id
 AND s.is_hidden = i.after_hidden AND s.updated_at = i.after_updated_at
 AND i.reverted_at IS NULL
 RETURNING s.id)
 UPDATE visibility_review_items i SET reverted_at = NOW()
 FROM restored s WHERE i.run_id = $1 AND i.stream_id = s.id`

func TestVisibilityApplySQLAndAtomicConflict(t *testing.T) {
	for _, scenario := range []string{"success", "missing", "write-count", "replay"} {
		t.Run(scenario, func(t *testing.T) {
			status := "preview"
			if scenario == "replay" {
				status = "applied"
			}
			steps := []visibilityStep{{kind: "begin"}, {kind: "query", query: visibilityWantLock, args: visibilityIDArgs, rows: [][]driver.Value{{status, int64(2)}}}}
			if scenario != "replay" {
				rows := [][]driver.Value{{"one"}, {"two"}}
				if scenario == "missing" {
					rows = rows[:1]
				}
				steps = append(steps, visibilityStep{kind: "query", query: visibilityWantApplyLock, args: []driver.Value{visibilityMusicArg, int64(180), visibilityID.String()}, rows: rows})
				if scenario != "missing" {
					n := int64(2)
					if scenario == "write-count" {
						n = 1
					}
					steps = append(steps, visibilityStep{kind: "exec", query: visibilityWantApply, args: []driver.Value{visibilityID.String(), visibilityMusicArg, int64(180)}, affected: n})
				}
			}
			if scenario == "success" {
				steps = append(steps, visibilityStep{kind: "exec", query: `UPDATE visibility_review_runs SET status = 'applied', applied_at = NOW() WHERE id = $1`, args: visibilityIDArgs, affected: 1}, visibilityStep{kind: "commit"})
			} else {
				steps = append(steps, visibilityStep{kind: "rollback"})
			}
			r, _ := visibilityDB(t, steps)
			n, err := r.Apply(visibilityID)
			if scenario == "success" {
				if err != nil || n != 2 {
					t.Fatal(n, err)
				}
			} else if !errors.Is(err, ErrVisibilityConflict) {
				t.Fatal("競合時に一部の変更を確定した", err)
			}
		})
	}
}
func TestVisibilityRevertSQLPreservesLaterChanges(t *testing.T) {
	for _, status := range []string{"applied", "preview", "reverted"} {
		t.Run(status, func(t *testing.T) {
			steps := []visibilityStep{{kind: "begin"}, {kind: "query", query: visibilityWantLock, args: visibilityIDArgs, rows: [][]driver.Value{{status, int64(2)}}}}
			if status == "applied" {
				steps = append(steps,
					visibilityStep{kind: "exec", query: visibilityWantRevert, args: visibilityIDArgs, affected: 1},
					visibilityStep{kind: "exec", query: `UPDATE visibility_review_runs SET status = 'reverted', reverted_count = $2, reverted_at = NOW() WHERE id = $1`, args: []driver.Value{visibilityID.String(), int64(1)}, affected: 1},
					visibilityStep{kind: "commit"})
			} else {
				steps = append(steps, visibilityStep{kind: "rollback"})
			}
			r, _ := visibilityDB(t, steps)
			n, skipped, err := r.Revert(visibilityID)
			if status == "applied" {
				if err != nil || n != 1 || skipped != 1 {
					t.Fatal(n, skipped, err)
				}
			} else if !errors.Is(err, ErrVisibilityConflict) {
				t.Fatal("再撤回/未実行を戻した", err)
			}
		})
	}
}
func TestVisibilityRejectsBadSelectionBeforeDB(t *testing.T) {
	for _, ids := range [][]string{nil, {""}, {"same", "same"}, make([]string, 501)} {
		r, _ := visibilityDB(t, nil)
		if _, err := r.Preview(ids, nil); !errors.Is(err, ErrVisibilitySelection) {
			t.Fatal("invalid selection accepted", err)
		}
	}
}
