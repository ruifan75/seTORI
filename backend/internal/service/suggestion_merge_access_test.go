package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
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

// 実際の Merge → repository を通し、SQL 全文・引数と書き込み順を検査する。
// driver は SQL の意味を評価しない。公開用の条件は独立した固定 fixture で照合する。
func TestSuggestionMergeValidatesSelectedBeforeApply(t *testing.T) {
	publicQuery, err := os.ReadFile("testdata/suggestion_merge_view.sql")
	if err != nil {
		t.Fatal(err)
	}
	const privilegedQuery = `SELECT id, target_type, target_id, target_key, target_label, kind,
 before_data, after_data, payload, note, status,
 created_by, created_by_name, client_hint, reviewed_by, review_note,
 created_at, reviewed_at FROM edit_suggestions WHERE id = $1`
	target := uuid.MustParse("212d7d97-7a99-4d87-bcf2-6c210bd9e400")
	other := uuid.MustParse("9699bfc2-c5f7-4084-a756-982c65838119")
	id := uuid.MustParse("527b14c9-f292-4e20-836c-9c41c0ae944d")
	for _, tc := range []struct {
		name, targetType, after, status string
		targetID                        uuid.UUID
		permissions                     []string
		missing, malformedID            bool
		lastMissing, lastMalformedID    bool
		wantError                       error
		wantValidation                  bool
		approved, rejected              int
	}{
		{name: "秘匿された提案を読めない", missing: true, wantError: ErrSuggestionNotFound},
		{name: "別の歌唱の提案", targetType: "performance", targetID: other, wantValidation: true},
		{name: "対象の種類が違う", targetType: "song", targetID: target, wantValidation: true},
		{name: "既処理でも対象違いは拒否", targetType: "performance", targetID: other, status: "approved", wantValidation: true},
		{name: "無効な ID で対象を書かない", malformedID: true, wantValidation: true},
		{name: "最後の提案が秘匿なら書かない", targetType: "performance", targetID: target, after: "210", lastMissing: true, wantError: ErrSuggestionNotFound},
		{name: "最後の ID が不正でも書かない", targetType: "performance", targetID: target, after: "210", lastMalformedID: true, wantValidation: true},
		{name: "公開の提案を承認", targetType: "performance", targetID: target, after: "210", approved: 1},
		{name: "公開の提案を別の値で統合", targetType: "performance", targetID: target, after: "220", rejected: 1},
		{name: "restricted:view の提案を承認", targetType: "performance", targetID: target, after: "210", permissions: []string{"content:edit", "restricted:view"}, approved: 1},
		{name: "管理者の提案を承認", targetType: "performance", targetID: target, after: "210", permissions: []string{"*"}, approved: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &auditReviewEditor{}
			query := string(publicQuery)
			perms := tc.permissions
			if perms == nil {
				perms = []string{"content:edit"}
			} else {
				query = privilegedQuery
			}
			c := &mergeAuditConnector{t: t, query: query, id: id.String(), editor: e}
			if !tc.missing && !tc.malformedID {
				status := tc.status
				if status == "" {
					status = "pending"
				}
				c.rows = [][]driver.Value{{id.String(), tc.targetType, tc.targetID.String(), "", "target", "field",
					[]byte(`{"end_seconds":"200"}`), []byte(fmt.Sprintf(`{"end_seconds":%q}`, tc.after)), []byte("{}"), "", status,
					nil, "", "", nil, "", time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), nil}}
			}
			wantQueries := 1
			if tc.malformedID {
				wantQueries = 0
			}
			if tc.lastMissing {
				c.followID = other.String()
				wantQueries = 2
			}
			wantWrites := tc.approved + tc.rejected
			c.status, c.note = "approved", "統合して反映"
			if tc.rejected > 0 {
				c.status, c.note = "rejected", "統合して反映（別の値を採用）"
			}
			db := sql.OpenDB(c)
			t.Cleanup(func() {
				db.Close()
				if c.queries != wantQueries || c.writes != wantWrites {
					t.Errorf("queries/writes=%d/%d want %d/%d", c.queries, c.writes, wantQueries, wantWrites)
				}
			})
			s := &SuggestionService{repo: repository.NewSuggestionRepository(db), editors: map[string]TargetEditor{"performance": e}}
			rawID := id.String()
			if tc.malformedID {
				rawID = "invalid-id"
			}
			ids := []string{rawID}
			if tc.lastMissing {
				ids = append(ids, other.String())
			} else if tc.lastMalformedID {
				ids = append(ids, "invalid-id")
			}
			response, err := s.Merge(&dto.MergeSuggestionsRequest{TargetType: "performance", TargetID: target.String(),
				IDs: ids, Fields: map[string]string{"end_seconds": "210"}}, &models.User{Permissions: perms})
			if tc.wantError != nil || tc.wantValidation {
				var validation *ValidationError
				if tc.wantError != nil && !errors.Is(err, tc.wantError) || tc.wantValidation && !errors.As(err, &validation) || response != nil || e.applied != nil {
					t.Fatalf("rejection response=%+v err=%v applied=%v", response, err, e.applied)
				}
			} else if err != nil || response == nil || response.Approved != tc.approved || response.Rejected != tc.rejected ||
				!reflect.DeepEqual(e.applied, map[string]string{"end_seconds": "210"}) || !reflect.DeepEqual(response.Applied, e.applied) {
				t.Fatalf("positive response=%+v err=%v applied=%v", response, err, e.applied)
			}
		})
	}
}

type mergeAuditConnector struct {
	t               *testing.T
	query, id       string
	followID        string // 2 件目は閲覧不能で、公開用の SQL が行を返さない。
	rows            [][]driver.Value
	editor          *auditReviewEditor
	queries, writes int
	status, note    string
	allowUnapplied  bool // 衝突の記録は対象を編集せずに行う。
}

func (c *mergeAuditConnector) Connect(context.Context) (driver.Conn, error) {
	return &mergeAuditConn{c}, nil
}
func (*mergeAuditConnector) Driver() driver.Driver { return mergeAuditDriver{} }

type mergeAuditDriver struct{}

func (mergeAuditDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use Connector") }

type mergeAuditConn struct{ c *mergeAuditConnector }

func (*mergeAuditConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (*mergeAuditConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected Begin") }
func (*mergeAuditConn) Close() error              { return nil }
func (c *mergeAuditConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.c.queries++
	if c.c.editor.applied != nil {
		c.c.t.Error("target was edited before validating selected suggestions")
	}
	if strings.Join(strings.Fields(query), " ") != strings.Join(strings.Fields(c.c.query), " ") {
		c.c.t.Errorf("SQL mismatch\ngot: %s\nwant: %s", query, c.c.query)
		return nil, errors.New("SQL mismatch")
	}
	id, rows := c.c.id, c.c.rows
	if c.c.queries > 1 {
		if c.c.queries != 2 || c.c.followID == "" {
			return nil, errors.New("unexpected extra query")
		}
		id, rows = c.c.followID, nil
	}
	if len(args) != 1 || args[0].Value != id {
		return nil, fmt.Errorf("query arguments=%v", args)
	}
	return &mergeAuditRows{rows: rows}, nil
}
func (c *mergeAuditConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.c.writes++
	const expected = `UPDATE edit_suggestions SET status = $2, reviewed_by = $3, review_note = $4, reviewed_at = NOW() WHERE id = $1`
	if strings.Join(strings.Fields(query), " ") != expected {
		return nil, fmt.Errorf("unexpected write: %s", query)
	}
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	if !reflect.DeepEqual(values, []driver.Value{c.c.id, c.c.status, nil, c.c.note}) {
		return nil, fmt.Errorf("write arguments=%v", values)
	}
	if c.c.editor.applied == nil && !c.c.allowUnapplied {
		return nil, errors.New("suggestion status written before applying target")
	}
	return driver.RowsAffected(1), nil
}

type mergeAuditRows struct {
	rows [][]driver.Value
	next int
}

func (*mergeAuditRows) Columns() []string {
	return strings.Fields("id target_type target_id target_key target_label kind before_data after_data payload note status created_by created_by_name client_hint reviewed_by review_note created_at reviewed_at")
}
func (*mergeAuditRows) Close() error { return nil }
func (r *mergeAuditRows) Next(dest []driver.Value) error {
	if r.next == len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}
