package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

// 同じ行を何度読んでも DB の現在の状態を返す。事前取得された pending の
// コピーを二度書く退行を、クエリ拒否や nil チェックでなく実際の件数で検出する。
func TestSuggestionMergeRejectsDuplicateIDsBeforeWriting(t *testing.T) {
	query, err := os.ReadFile("testdata/suggestion_merge_view.sql")
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse("a27b14c9-f292-4e20-836c-9c41c0ae944d")
	target := uuid.MustParse("212d7d97-7a99-4d87-bcf2-6c210bd9e400")
	for _, tc := range []struct {
		name      string
		ids       []string
		duplicate bool
	}{
		{"単件の陽性対照", []string{id.String()}, false},
		{"同じ ID を二度指定", []string{id.String(), id.String()}, true},
		{"大文字表記でも同じ ID", []string{id.String(), strings.ToUpper(id.String())}, true},
		{"URN 表記でも同じ ID", []string{id.String(), "urn:uuid:" + id.String()}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &duplicateMergeConnector{query: string(query), id: id, target: target, status: "pending"}
			db := sql.OpenDB(c)
			t.Cleanup(func() { db.Close() })
			e := &auditReviewEditor{}
			s := &SuggestionService{repo: repository.NewSuggestionRepository(db), editors: map[string]TargetEditor{"performance": e}}
			response, err := s.Merge(&dto.MergeSuggestionsRequest{TargetType: "performance", TargetID: target.String(),
				IDs: tc.ids, Fields: map[string]string{"end_seconds": "210"}}, &models.User{Permissions: []string{"content:edit"}})
			if tc.duplicate {
				var invalidInput *ValidationError
				if !errors.As(err, &invalidInput) || response != nil || e.applied != nil || c.writes != 0 {
					t.Fatalf("duplicate applied: response=%+v err=%v target=%v queries/writes=%d/%d", response, err, e.applied, c.queries, c.writes)
				}
			} else if err != nil || response == nil || response.Approved != 1 || response.Rejected != 0 ||
				!reflect.DeepEqual(e.applied, map[string]string{"end_seconds": "210"}) || c.queries != 1 || c.writes != 1 || c.status != "approved" {
				t.Fatalf("positive: response=%+v err=%v target=%v queries/writes=%d/%d status=%s", response, err, e.applied, c.queries, c.writes, c.status)
			}
		})
	}
}

type duplicateMergeConnector struct {
	query           string
	id, target      uuid.UUID
	status          string
	queries, writes int
}

func (c *duplicateMergeConnector) Connect(context.Context) (driver.Conn, error) {
	return &duplicateMergeConn{c}, nil
}
func (*duplicateMergeConnector) Driver() driver.Driver { return duplicateMergeDriver{} }

type duplicateMergeDriver struct{}

func (duplicateMergeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use Connector")
}

type duplicateMergeConn struct{ c *duplicateMergeConnector }

func (*duplicateMergeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (*duplicateMergeConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected Begin") }
func (*duplicateMergeConn) Close() error              { return nil }
func (c *duplicateMergeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.c.queries++
	if strings.Join(strings.Fields(query), " ") != strings.Join(strings.Fields(c.c.query), " ") || len(args) != 1 || args[0].Value != c.c.id.String() {
		return nil, fmt.Errorf("unexpected query: %s args=%v", query, args)
	}
	return &mergeAuditRows{rows: [][]driver.Value{{c.c.id.String(), "performance", c.c.target.String(), "", "target", "field",
		[]byte(`{"end_seconds":"200"}`), []byte(`{"end_seconds":"210"}`), []byte("{}"), "", c.c.status,
		nil, "", "", nil, "", time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), nil}}}, nil
}
func (c *duplicateMergeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	const expected = `UPDATE edit_suggestions SET status = $2, reviewed_by = $3, review_note = $4, reviewed_at = NOW() WHERE id = $1`
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	if strings.Join(strings.Fields(query), " ") != expected || !reflect.DeepEqual(values, []driver.Value{c.c.id.String(), "approved", nil, "統合して反映"}) {
		return nil, fmt.Errorf("unexpected write: %s args=%v", query, args)
	}
	c.c.writes++
	c.c.status = "approved"
	return driver.RowsAffected(1), nil
}
