package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/auth"
)

// 本物の ServeHTTP、セッション解決、ロール更新を通す。
// 外部書き込みのハンドラだけは 204 を返す probe に替え、許可時の到達も数える。
// 本物の送信ルートの登録は TestHolodexSyncEndpointPermissions で別途検査する。
func TestHolodexUploadRoleChangesAndMiddleware(t *testing.T) {
	state := &holodexRoleDB{permissions: []string{"users:manage", "content:edit", "sync:run"}}
	db := sql.OpenDB(state)
	defer db.Close()
	r := &Router{mux: http.NewServeMux(), authService: service.NewAuthService(repository.NewAuthRepository(db))}
	r.setupRoutes()
	realMux := r.mux
	r.mux = http.NewServeMux()
	var reached int
	r.mux.HandleFunc("POST /api/sync/holodex/to-holodex/{id}", func(w http.ResponseWriter, req *http.Request) {
		reached++
		w.WriteHeader(http.StatusNoContent)
	})
	r.mux.Handle("PUT /api/roles/{id}", realMux)
	r.mux.Handle("GET /api/permissions", realMux)
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	paths := []string{
		"/api/sync/holodex/to-holodex/hVfDBfreYNI",
		"/api/sync/holodex/%74o-holodex/hVfDBfreYNI",
		"/api/sync/%68olodex/to-holodex/.%2FhVfDBfreYNI",
	}
	assertUploads := func(token string, status int) {
		t.Helper()
		before := reached
		for _, path := range paths {
			w := request(http.MethodPost, path, "", token)
			if w.Code != status {
				t.Fatalf("POST %s: %d %s, want %d", path, w.Code, w.Body.String(), status)
			}
		}
		want := before
		if status == http.StatusNoContent {
			want += len(paths)
		}
		if reached != want {
			t.Fatalf("upload handler calls = %d, want %d", reached, want)
		}
	}
	assertUploads("", http.StatusUnauthorized)
	assertUploads("same-session", http.StatusForbidden)
	catalog := request(http.MethodGet, "/api/permissions", "", "same-session")
	var permissions []auth.PermissionInfo
	if catalog.Code != http.StatusOK || json.Unmarshal(catalog.Body.Bytes(), &permissions) != nil {
		t.Fatalf("permission catalog: %d %s", catalog.Code, catalog.Body.String())
	}
	var uploadCount int
	for _, permission := range permissions {
		if permission.Key == "holodex:upload" {
			uploadCount++
		}
	}
	if uploadCount != 1 {
		t.Fatalf("upload catalog entries = %d, want 1", uploadCount)
	}
	base := append([]string{}, state.permissions...)
	for _, grant := range []bool{true, false} {
		wanted := append([]string{}, base...)
		if grant {
			wanted = append(wanted, "holodex:upload")
		}
		state.wantWrite = wanted
		body, err := json.Marshal(map[string]any{"description": "delegated", "permissions": wanted})
		if err != nil {
			t.Fatal(err)
		}
		w := request(http.MethodPut, "/api/roles/"+holodexRoleID, string(body), "same-session")
		var role models.Role
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &role) != nil || !reflect.DeepEqual([]string(role.Permissions), wanted) {
			t.Fatalf("role update: %d %s, want %v", w.Code, w.Body.String(), wanted)
		}
		status := http.StatusForbidden
		if grant {
			status = http.StatusNoContent
		}
		assertUploads("same-session", status)
	}
	if state.writes != 2 {
		t.Fatalf("role writes = %d, want 2", state.writes)
	}
}

const holodexRoleID = "00000000-0000-0000-0000-000000000084"

// 専用 connector。権限を読み書きする SQL 全体と引数を検査する。
type holodexRoleDB struct {
	permissions, wantWrite []string
	writes                 int
}

func (s *holodexRoleDB) Connect(context.Context) (driver.Conn, error) { return s, nil }
func (s *holodexRoleDB) Driver() driver.Driver                        { return s }
func (s *holodexRoleDB) Open(string) (driver.Conn, error)             { return s, nil }
func (s *holodexRoleDB) Close() error                                 { return nil }
func (s *holodexRoleDB) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("unexpected Prepare")
}
func (s *holodexRoleDB) Begin() (driver.Tx, error) { return nil, fmt.Errorf("unexpected Begin") }
func (s *holodexRoleDB) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("unexpected query args: %v", args)
	}
	perms, err := pq.Array(s.permissions).Value()
	if err != nil {
		return nil, err
	}
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	var values []driver.Value
	switch strings.Join(strings.Fields(query), " ") {
	case `SELECT u.id, u.username, u.display_name, u.email, u.email_verified, COALESCE(u.password_hash, ''), u.role_id, u.is_active, u.last_login, u.created_at, u.updated_at, r.name, r.permissions FROM users u JOIN roles r ON r.id = u.role_id JOIN sessions s ON s.user_id = u.id WHERE s.token_hash = $1 AND s.expires_at > NOW() AND u.is_active = TRUE`:
		if args[0].Value != auth.HashToken("same-session") {
			return nil, fmt.Errorf("unexpected session token")
		}
		values = []driver.Value{"00000000-0000-0000-0000-000000000001", "fixture", "Fixture", nil, false, "", holodexRoleID, true, nil, now, now, "custom", perms}
	case `SELECT id, name, description, permissions, is_system, created_at, updated_at FROM roles WHERE id=$1`:
		if args[0].Value != holodexRoleID {
			return nil, fmt.Errorf("unexpected role ID: %v", args)
		}
		values = []driver.Value{holodexRoleID, "custom", "delegated", perms, false, now, now}
	default:
		return nil, fmt.Errorf("unexpected auth SQL: %s", query)
	}
	return &holodexRoleRows{values: values}, nil
}
func (s *holodexRoleDB) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	wantPerms, err := pq.Array(s.wantWrite).Value()
	if err != nil {
		return nil, err
	}
	if strings.Join(strings.Fields(query), " ") != `UPDATE roles SET description=$2, permissions=$3, updated_at=NOW() WHERE id=$1` || len(args) != 3 || args[0].Value != holodexRoleID || args[1].Value != "delegated" || args[2].Value != wantPerms {
		return nil, fmt.Errorf("unexpected role write: %s, %v", query, args)
	}
	s.permissions = append([]string{}, s.wantWrite...)
	s.writes++
	return driver.RowsAffected(1), nil
}

type holodexRoleRows struct {
	values []driver.Value
	read   bool
}

func (r *holodexRoleRows) Columns() []string {
	columns := make([]string, len(r.values))
	for i := range columns {
		columns[i] = fmt.Sprintf("column%d", i)
	}
	return columns
}
func (r *holodexRoleRows) Close() error { return nil }
func (r *holodexRoleRows) Next(values []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	copy(values, r.values)
	return nil
}
