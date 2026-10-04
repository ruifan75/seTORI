package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/auth"
)

// 実際の Router -> 認証 -> 認可 -> ハンドラ -> service/repository を通す。
// SQL の実行計画・秘匿条件の正しさは既存の repository 統合テストが担当する。
// ここでは新旧で同じ応答・同じ SQL/引数が出ることと、成功/拒否の契約を固定する。
func TestChannelAPIAliases(t *testing.T) {
	cases := []struct {
		method, suffix, body string
		protected            bool
		status               int
		contains             string
	}{
		{"GET", "?include_hidden=true", "", false, 200, `"singers":[`},
		{"GET", "?group=organization&include_hidden=true", "", false, 200, `"groups":[`},
		{"GET", "/search?q=channel", "", false, 200, `"name":"channel"`},
		{"GET", "/channel", "", false, 200, `"performance_count":0`},
		{"GET", "/channel/streams?hidden=all&processed=true", "", false, 200, `"streams":[]`},
		{"GET", "/channel/performances", "", false, 200, `"singer":{`},
		// 登録は外部サービスを呼ぶ。同じハンドラへの登録は固定の棚卸し表でも検査し、
		// このケースでは外部通信前の入力検証と認可を実行する。
		{"POST", "", `{}`, true, 400, `"error":`},
		{"PUT", "/channel", `{"name":"updated"}`, true, 200, `"name":"updated"`},
		{"PUT", "/channel/visibility", `{"is_hidden":true}`, true, 200, `"is_hidden":true`},
		{"PUT", "/channel/members-policy", `{"members_only_policy":"allow"}`, true, 200, `"members_only_policy":"allow"`},
		{"PUT", "/channel/auto-fill", `{"auto_fill_enabled":true}`, true, 200, `"auto_fill_enabled":true`},
		{"GET", "/auto-fill", "", true, 200, `"singers":[`},
		{"PUT", "/channel/organization", `{"organization":""}`, true, 200, `"organization":""`},
	}
	users := []struct {
		token, permissions string
		canEdit            bool
	}{
		{"", "{}", false}, {"viewer", "{}", false},
		{"editor", "{content:edit}", true}, {"restricted", "{restricted:view}", false},
		{"both", "{content:edit,restricted:view}", true}, {"admin", "{*}", true},
	}
	for _, tc := range cases {
		for _, user := range users {
			t.Run(tc.method+tc.suffix+"/"+user.token, func(t *testing.T) {
				var wantBody string
				var wantSQL []channelAliasCall
				for i, prefix := range []string{"/api/channels", "/api/singers", "/api/%63hannels", "/api/%73ingers"} {
					fixture := &channelAliasDB{permissions: user.permissions}
					db := sql.OpenDB(fixture)
					defer db.Close()
					r := &Router{mux: http.NewServeMux(),
						authService:   service.NewAuthService(repository.NewAuthRepository(db)),
						singerService: service.NewSingerService(repository.NewSingerRepository(db), repository.NewStreamRepository(db), repository.NewPerformanceRepository(db)),
					}
					r.setupRoutes()
					req := httptest.NewRequest(tc.method, prefix+tc.suffix, strings.NewReader(tc.body))
					if user.token != "" {
						req.Header.Set("Authorization", "Bearer "+user.token)
					}
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)
					status := tc.status
					if tc.protected && !user.canEdit {
						status = 403
						if user.token == "" {
							status = 401
						}
					}
					if w.Code != status {
						t.Fatalf("%s: status=%d want=%d body=%s", prefix, w.Code, status, w.Body.String())
					}
					if fixture.unexpected != "" {
						t.Fatal(fixture.unexpected)
					}
					if status == tc.status && !strings.Contains(w.Body.String(), tc.contains) {
						t.Fatalf("%s: success/input validation response missing %s: %s", prefix, tc.contains, w.Body.String())
					}
					if i == 0 {
						wantBody = w.Body.String()
						wantSQL = fixture.calls
					} else {
						if w.Body.String() != wantBody {
							t.Errorf("%s response differs from /api/channels:\n%s\nwant %s", prefix, w.Body.String(), wantBody)
						}
						if !reflect.DeepEqual(fixture.calls, wantSQL) {
							t.Errorf("%s SQL/arguments differ:\n%#v\nwant %#v", prefix, fixture.calls, wantSQL)
						}
					}
					if tc.protected && !user.canEdit {
						// 拒否した要求は認証以外の SELECT/UPDATE や外部サービスへ届かない。
						count := 0
						if user.token != "" {
							count = 1
						}
						if len(fixture.calls) != count {
							t.Fatalf("rejected request reached repository: %#v", fixture.calls)
						}
					}
				}
			})
		}
	}
}

// Connector を局所的に使い、他の偽 driver の登録や列数に影響しない。
type channelAliasCall struct {
	query string
	args  []driver.NamedValue
}
type channelAliasDB struct {
	permissions, unexpected string
	calls                   []channelAliasCall
}

func (d *channelAliasDB) Connect(context.Context) (driver.Conn, error) {
	return &channelAliasConn{d}, nil
}
func (d *channelAliasDB) Driver() driver.Driver { return channelAliasDriver{} }

type channelAliasDriver struct{}

func (channelAliasDriver) Open(string) (driver.Conn, error) { return nil, fmt.Errorf("use connector") }

type channelAliasConn struct{ db *channelAliasDB }

func (*channelAliasConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("unexpected prepare")
}
func (*channelAliasConn) Close() error              { return nil }
func (*channelAliasConn) Begin() (driver.Tx, error) { return nil, fmt.Errorf("unexpected transaction") }
func (c *channelAliasConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.db.calls = append(c.db.calls, channelAliasCall{query, append([]driver.NamedValue(nil), args...)})
	q := strings.Join(strings.Fields(query), " ")
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	switch {
	case strings.Contains(q, "FROM users u JOIN roles r"):
		if !strings.Contains(q, "JOIN sessions s") || len(args) != 1 {
			break
		}
		// セッション検索にも本物のハッシュを渡していることを確認する。
		valid := false
		for _, token := range []string{"viewer", "editor", "restricted", "both", "admin"} {
			if args[0].Value == auth.HashToken(token) {
				valid = true
			}
		}
		if !valid {
			break
		}
		return channelAliasRowsOf(13, []driver.Value{"00000000-0000-0000-0000-000000000001", "viewer", "Viewer", nil, false, "", "00000000-0000-0000-0000-000000000002", true, nil, now, now, "fixture", c.db.permissions}), nil
	case strings.HasPrefix(q, "UPDATE singers SET name =") && strings.HasSuffix(q, "RETURNING created_at, updated_at"):
		return channelAliasRowsOf(2, []driver.Value{now, now}), nil
	case strings.HasPrefix(q, "SELECT COUNT(*), COUNT(*) FILTER"):
		return channelAliasRowsOf(2, []driver.Value{int64(1), int64(0)}), nil
	case strings.HasPrefix(q, "SELECT ss.singer_id, COUNT(*)"):
		return channelAliasRowsOf(2), nil
	case strings.HasPrefix(q, "SELECT COUNT("):
		return channelAliasRowsOf(1, []driver.Value{int64(0)}), nil
	case strings.HasPrefix(q, "SELECT s.id, s.name, s.english_name") && strings.Contains(q, "FROM singers s LEFT JOIN organizations o"):
		return channelAliasRowsOf(14, []driver.Value{"channel", "channel", nil, nil, nil, nil, nil, false, "manual", false, "allow", true, now, now}), nil
	case strings.Contains(q, "FROM streams s") || strings.Contains(q, "FROM performances p"):
		return channelAliasRowsOf(1), nil
	}
	c.db.unexpected = "unexpected SQL: " + q
	return nil, fmt.Errorf("%s", c.db.unexpected)
}
func (c *channelAliasConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.db.calls = append(c.db.calls, channelAliasCall{query, append([]driver.NamedValue(nil), args...)})
	if strings.HasPrefix(strings.TrimSpace(query), "UPDATE singers SET ") {
		return driver.RowsAffected(1), nil
	}
	c.db.unexpected = "unexpected SQL: " + query
	return nil, fmt.Errorf("%s", c.db.unexpected)
}

type channelAliasRows struct {
	columns []string
	values  [][]driver.Value
}

func channelAliasRowsOf(columns int, values ...[]driver.Value) *channelAliasRows {
	return &channelAliasRows{make([]string, columns), values}
}
func (r *channelAliasRows) Columns() []string { return r.columns }
func (*channelAliasRows) Close() error        { return nil }
func (r *channelAliasRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}
