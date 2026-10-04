package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
)

// 発行 SQL 全文・引数を独立した期待値と照合する。共有 driver は変更しない。
type auditSQLConnector struct {
	query    string
	args     []driver.Value
	rows     [][]driver.Value
	columns  int
	queryErr error
	calls    int
	steps    []auditSQLConnector
}

func (c *auditSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &auditSQLConn{c}, nil
}
func (*auditSQLConnector) Driver() driver.Driver { return auditSQLDriver{} }

type auditSQLDriver struct{}

func (auditSQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use Connector") }

type auditSQLConn struct{ c *auditSQLConnector }

func (*auditSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (*auditSQLConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected Begin") }
func (*auditSQLConn) Close() error              { return nil }
func (c *auditSQLConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.c.calls++
	expected := c.c
	if len(c.c.steps) > 0 {
		if c.c.calls > len(c.c.steps) {
			return nil, errors.New("unexpected extra query")
		}
		expected = &c.c.steps[c.c.calls-1]
	}
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	if normalize(query) != normalize(expected.query) {
		return nil, fmt.Errorf("SQL mismatch\ngot: %s\nwant: %s", query, expected.query)
	}
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	if !reflect.DeepEqual(values, expected.args) {
		return nil, fmt.Errorf("arguments: %#v want %#v", values, expected.args)
	}
	if expected.queryErr != nil {
		return nil, expected.queryErr
	}
	return &auditSQLRows{rows: expected.rows, columns: expected.columns}, nil
}

type auditSQLRows struct {
	rows          [][]driver.Value
	columns, next int
}

func (r *auditSQLRows) Columns() []string {
	cols := make([]string, r.columns)
	for i := range cols {
		cols[i] = fmt.Sprintf("column_%d", i)
	}
	return cols
}
func (*auditSQLRows) Close() error { return nil }
func (r *auditSQLRows) Next(dest []driver.Value) error {
	if r.next == len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}
func auditTestDB(t *testing.T, c *auditSQLConnector, calls int) *sql.DB {
	t.Helper()
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		db.Close()
		if c.calls != calls {
			t.Errorf("queries=%d want %d", c.calls, calls)
		}
	})
	return db
}

// 所有者全員の許可を独立したリテラルで固定する。is_hidden は秘匿に使わない。
func auditWantAutoRestricted(alias string) string {
	return "EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = " + alias + ".id AND mt.tag_id = 'members_only') AND NOT " +
		"COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow') FROM stream_singers eo JOIN singers eg ON eg.id = eo.singer_id WHERE eo.stream_id = " + alias + ".id AND eo.is_owner), FALSE)"
}
func auditWantStreamQuery() string {
	return `SELECT id, title, stream_date, duration_seconds, thumbnail_url, holodex_data, holodex_hash, comment_raw, comment_songs, comment_songs_analyzed_at, chapter_raw, chapter_songs, is_processed, is_hidden, restriction_override, holodex_uploaded_at, holodex_upload_unknown, availability, playable_in_embed, availability_checked_at, created_at, updated_at,
 COALESCE(streams.restriction_override, ` + auditWantAutoRestricted("streams") + `) AS is_restricted_effective,
 streams.restriction_override IS FALSE AND ` + auditWantAutoRestricted("streams") + ` AND streams.restriction_override_auto IS DISTINCT FROM TRUE AS restriction_needs_review
 FROM streams WHERE id = $1`
}
func auditStreamRow(id string, hidden, restricted bool) []driver.Value {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	return []driver.Value{id, "title", now, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, hidden, nil, nil, false, nil, nil, nil, now, now, restricted, false}
}

func TestAnalysisAccessUsesEffectiveRestriction(t *testing.T) {
	for _, tc := range []struct {
		name               string
		hidden, restricted bool
		permissions        []string
		queryErr           error
		status, calls      int
	}{
		{name: "表示でも秘匿", restricted: true, permissions: []string{"content:edit"}, status: 403, calls: 1},
		{name: "非表示でも公開", hidden: true, permissions: []string{"content:edit"}, status: 204, calls: 2},
		{name: "公開の陽性対照", permissions: []string{"content:edit"}, status: 204, calls: 2},
		{name: "秘匿を閲覧できる編集者", restricted: true, permissions: []string{"content:edit", "restricted:view"}, status: 204},
		{name: "system admin", restricted: true, permissions: []string{"*"}, status: 204},
		{name: "照会失敗で先へ進まない", permissions: []string{"content:edit"}, queryErr: errors.New("DB unavailable"), status: 500, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "hVfDBfreYNI"
			c := &auditSQLConnector{query: auditWantStreamQuery(), args: []driver.Value{id}, columns: 24, rows: [][]driver.Value{auditStreamRow(id, tc.hidden, tc.restricted)}, queryErr: tc.queryErr}
			r := &Router{db: auditTestDB(t, c, tc.calls)}
			req := withUser(httptest.NewRequest("POST", "/api/streams/"+id+"/comments/analyze", nil), &models.User{Permissions: tc.permissions})
			req.SetPathValue("id", id)
			reached := false
			w := httptest.NewRecorder()
			r.withAnalysisAccess(func(w http.ResponseWriter, _ *http.Request) { reached = true; w.WriteHeader(204) })(w, req)
			if w.Code != tc.status || reached != (tc.status == 204) {
				t.Fatalf("status=%d reached=%t body=%s", w.Code, reached, w.Body.String())
			}
		})
	}
}

// 処理中の公開可否の変更を偽 DB の二段階の応答で再現する。
// callback は取得・解析の代役。入口の拒否は callback より先、再確認は
// 本文・ヘッダー・ステータスが利用者へ届くより先でなければならない。
func TestAnalysisAccessRechecksBeforeResponse(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		initiallyRestricted   bool
		finallyRestricted     bool
		initiallyMissing      bool
		finallyMissing        bool
		finalQueryErr         error
		permissions           []string
		status, queries, work int
	}{
		{name: "取得前に拒否", initiallyRestricted: true, permissions: []string{"content:edit"}, status: 403, queries: 1},
		{name: "未登録の配信では取得しない", initiallyMissing: true, permissions: []string{"content:edit"}, status: 404, queries: 1},
		{name: "処理中に秘匿へ変更", finallyRestricted: true, permissions: []string{"content:edit"}, status: 403, queries: 2, work: 1},
		{name: "処理中に配信を削除", finallyMissing: true, permissions: []string{"content:edit"}, status: 404, queries: 2, work: 1},
		{name: "応答前の照会失敗", finalQueryErr: errors.New("final check unavailable"), permissions: []string{"content:edit"}, status: 500, queries: 2, work: 1},
		{name: "公開の結果を返す", permissions: []string{"content:edit"}, status: 201, queries: 2, work: 1},
		{name: "restricted:view の結果を返す", initiallyRestricted: true, finallyRestricted: true, permissions: []string{"content:edit", "restricted:view"}, status: 201, work: 1},
		{name: "管理者の結果を返す", initiallyRestricted: true, finallyRestricted: true, permissions: []string{"*"}, status: 201, work: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "hVfDBfreYNI"
			c := &auditSQLConnector{steps: []auditSQLConnector{
				{query: auditWantStreamQuery(), args: []driver.Value{id}, columns: 24, rows: [][]driver.Value{auditStreamRow(id, false, tc.initiallyRestricted)}},
				{query: auditWantStreamQuery(), args: []driver.Value{id}, columns: 24, rows: [][]driver.Value{auditStreamRow(id, false, tc.finallyRestricted)}, queryErr: tc.finalQueryErr},
			}}
			if tc.initiallyMissing {
				c.steps[0].rows = nil
			}
			if tc.finallyMissing {
				c.steps[1].rows = nil
			}
			r := &Router{db: auditTestDB(t, c, tc.queries)}
			req := withUser(httptest.NewRequest("POST", "/api/streams/"+id+"/comments/analyze", nil), &models.User{Permissions: tc.permissions})
			req.SetPathValue("id", id)
			w := httptest.NewRecorder()
			work := 0
			r.withAnalysisAccess(func(out http.ResponseWriter, _ *http.Request) {
				work++
				out.Header().Set("X-Analysis-Result", "private-metadata")
				respondJSON(out, 201, map[string]string{"song": "private-result"})
				if tc.queries > 0 && (w.Body.Len() != 0 || w.Header().Get("X-Analysis-Result") != "") {
					t.Error("analysis response reached the client before final check")
				}
			})(w, req)
			if w.Code != tc.status || work != tc.work {
				t.Fatalf("status=%d work=%d body=%s", w.Code, work, w.Body.String())
			}
			if tc.status == 201 {
				if w.Body.String() != "{\"song\":\"private-result\"}\n" || w.Header().Get("X-Analysis-Result") != "private-metadata" || w.Header().Get("Content-Type") != "application/json" {
					t.Fatalf("positive response headers=%v body=%s", w.Header(), w.Body.String())
				}
			} else {
				var body map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if len(body) != 1 || body["error"] == "" || w.Header().Get("X-Analysis-Result") != "" {
					t.Fatalf("denial includes result: headers=%v body=%v", w.Header(), body)
				}
			}
		})
	}
}

// 実登録の全解析ルートを通す。拒否時は解析サービスや外部取得へ到達しない。
func TestRegisteredAnalysisRoutesDenyRestrictedEditor(t *testing.T) {
	for _, pattern := range []string{
		"GET /api/streams/{id}/comments", "GET /api/streams/{id}/chapters", "GET /api/streams/{id}/holodex-songs",
		"GET /api/streams/{id}/import/live-chat", "POST /api/streams/{id}/comments/analyze",
		"POST /api/streams/{id}/chapters/analyze", "POST /api/streams/{id}/holodex-songs/analyze",
		"POST /api/streams/{id}/chapters/sync", "POST /api/streams/{id}/chat-end-estimate",
		"POST /api/streams/{id}/analyze-chat-ends", "POST /api/streams/{id}/comments/sync-youtube",
	} {
		t.Run(pattern, func(t *testing.T) {
			const id = "hVfDBfreYNI"
			parts := strings.SplitN(pattern, " ", 2)
			c := &auditSQLConnector{query: auditWantStreamQuery(), args: []driver.Value{id}, columns: 24, rows: [][]driver.Value{auditStreamRow(id, false, true)}}
			r := &Router{db: auditTestDB(t, c, 1), mux: http.NewServeMux()}
			r.setupRoutes()
			req := withUser(httptest.NewRequest(parts[0], strings.ReplaceAll(parts[1], "{id}", id), nil), &models.User{Permissions: []string{"content:edit"}})
			w := httptest.NewRecorder()
			r.mux.ServeHTTP(w, req)
			if w.Code != 403 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func auditWantGapsQuery(privileged bool) string {
	clause := ""
	if !privileged {
		clause = " AND NOT COALESCE(st.restriction_override, " + auditWantAutoRestricted("st") + ")"
	}
	return `SELECT p.stream_id, st.title, g.performance_id, s.name, p.start_seconds
 FROM batch_fill_gaps g JOIN performances p ON p.id = g.performance_id
 JOIN songs s ON s.id = p.song_id JOIN streams st ON st.id = p.stream_id
 WHERE g.run_id = $1` + clause + ` ORDER BY st.stream_date DESC, p.start_seconds`
}

// handler → service → repository を通し、発行 SQL 全体と応答を検査する。
// 偽 driver は WHERE を評価しない。SQL の意味は Postgres 統合テストが担当する。
func TestBatchFillGapsPassViewerAccess(t *testing.T) {
	for _, privileged := range []bool{false, true} {
		t.Run(fmt.Sprint(privileged), func(t *testing.T) {
			runID := uuid.MustParse("527b14c9-f292-4e20-836c-9c41c0ae944d")
			rows := [][]driver.Value{{"public00001", "public stream", "d6bd6e63-bbc8-4e8f-a579-bc0a6b29d213", "public song", int64(30)}}
			perms := []string{"content:edit"}
			if privileged {
				perms = append(perms, "restricted:view")
				rows = append(rows, []driver.Value{"private0001", "private stream", "499a61c3-9ab3-49ca-a6a5-706fd755c264", "private song", int64(60)})
			}
			c := &auditSQLConnector{query: auditWantGapsQuery(privileged), args: []driver.Value{runID.String()}, columns: 5, rows: rows}
			svc := service.NewBatchFillService(nil, nil, repository.NewBatchFillRepository(auditTestDB(t, c, 1)), nil, nil, nil, nil, nil, nil)
			r := &Router{batchFillService: svc, mux: http.NewServeMux()}
			r.setupRoutes()
			req := withUser(httptest.NewRequest("GET", "/api/streams/batch-fill/runs/"+runID.String()+"/gaps", nil), &models.User{Permissions: perms})
			w := httptest.NewRecorder()
			r.mux.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var response struct {
				Gaps []repository.BatchFillGap `json:"gaps"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			got := response.Gaps
			if len(got) != len(rows) || got[0].StreamID != "public00001" || got[0].SongName != "public song" || got[0].StartSeconds != 30 {
				t.Fatalf("gaps=%+v", got)
			}
			if privileged && (got[1].SongName != "private song" || got[1].StartSeconds != 60) {
				t.Fatalf("privileged gaps=%+v", got)
			}
		})
	}
}
