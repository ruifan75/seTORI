package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

// 実 handler → repository → Tx を通し、変更先 false を true に読み替えないこと、
// 操作者・選択ID・前後値の退避を、発行 SQL と引数の完全一致で確かめる。
func TestProcessedPreviewForwardsTargetAndActor(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			fixture := &processedPreviewConnector{t: t, after: after, actor: uuid.MustParse("11111111-1111-1111-1111-111111111111")}
			db := sql.OpenDB(fixture)
			defer db.Close()
			r := &Router{processedReview: repository.NewProcessedReviewRepository(db)}
			req := withUser(httptest.NewRequest("POST", "/api/processed-review/preview", strings.NewReader(fmt.Sprintf(`{"stream_ids":["one","two"],"is_processed":%t}`, after))), &models.User{ID: fixture.actor})
			w := httptest.NewRecorder()
			r.handleProcessedPreview(w, req)
			var response struct {
				RunID string `json:"run_id"`
				Count int    `json:"count"`
				After bool   `json:"is_processed"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || response.RunID != fixture.run || response.Count != 2 || response.After != after || fixture.step != 4 {
				t.Fatal(w.Code, w.Body.String(), fixture.step)
			}
		})
	}
}

type processedPreviewConnector struct {
	t     *testing.T
	after bool
	actor uuid.UUID
	run   string
	step  int
}

func (d *processedPreviewConnector) Connect(context.Context) (driver.Conn, error) {
	return &processedPreviewConn{d}, nil
}
func (*processedPreviewConnector) Driver() driver.Driver { return processedPreviewDriver{} }

type processedPreviewDriver struct{}

func (processedPreviewDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type processedPreviewConn struct{ d *processedPreviewConnector }

func (*processedPreviewConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*processedPreviewConn) Close() error { return nil }
func (c *processedPreviewConn) Begin() (driver.Tx, error) {
	if c.d.step != 0 {
		return nil, errors.New("unexpected begin")
	}
	c.d.step++
	return &processedPreviewTx{c.d}, nil
}
func (c *processedPreviewConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	d := c.d
	values := []driver.Value{}
	for _, a := range args {
		values = append(values, a.Value)
	}
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	var want string
	var expected []driver.Value
	switch d.step {
	case 1:
		want = `INSERT INTO processed_review_runs (id, status, item_count, after_processed, started_by) VALUES ($1, 'preview', $2, $3, $4)`
		if len(values) != 4 {
			return nil, errors.New("run argument count")
		}
		var ok bool
		d.run, ok = values[0].(string)
		if !ok {
			return nil, errors.New("run ID is not UUID")
		}
		if _, err := uuid.Parse(d.run); err != nil {
			return nil, err
		}
		expected = []driver.Value{d.run, int64(2), d.after, d.actor.String()}
	case 2:
		want = `INSERT INTO processed_review_items (run_id, stream_id, stream_title, before_processed, after_processed, before_updated_at)
 SELECT $1, s.id, s.title, s.is_processed, $2, s.updated_at FROM streams s WHERE s.is_processed <> $2 AND s.id = ANY($3)`
		expected = []driver.Value{d.run, d.after, `{"one","two"}`}
	default:
		return nil, errors.New("unexpected exec")
	}
	if norm(q) != norm(want) || !reflect.DeepEqual(values, expected) {
		return nil, fmt.Errorf("SQL/args mismatch: %s %v; want %s %v", q, values, want, expected)
	}
	d.step++
	return driver.RowsAffected(2), nil
}

type processedPreviewTx struct{ d *processedPreviewConnector }

func (x *processedPreviewTx) Commit() error {
	if x.d.step != 3 {
		return errors.New("commit before audit")
	}
	x.d.step++
	return nil
}
func (x *processedPreviewTx) Rollback() error {
	x.d.t.Error("successful preview unexpectedly rolled back")
	return nil
}
