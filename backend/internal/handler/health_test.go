package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/config"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
)

// 実際の ServeHTTP と database/sql を通す。SQL・期限・公開 JSON を独立した値で固定する。
// fixture の機密をエラー・設定に置き、公開応答の全キーを検査する。
func TestAPIHealth(t *testing.T) {
	previousStart, previousCommit := processStartedAt, buildCommit
	t.Cleanup(func() { processStartedAt, buildCommit = previousStart, previousCommit })
	buildCommit = "fixture-health-commit"
	for _, tc := range []struct {
		name, mode, method, path, bearer string
		status, queries                  int
		cancelled                        bool
	}{
		{"healthy-anonymous", "ok", "GET", "/api/health", "", 200, 1, false},
		{"head", "ok", "HEAD", "/api/health", "", 200, 1, false},
		{"stale-bearer-is-irrelevant", "ok", "GET", "/api/health", "stale-fixture-token", 200, 1, false},
		{"encoded-path-and-bearer", "ok", "GET", "/api/%68ealth", "stale-fixture-token", 200, 1, false},
		{"database-error", "query-error", "GET", "/api/health", "", 503, 1, false},
		{"database-error-with-bearer", "query-error", "GET", "/api/health", "stale-fixture-token", 503, 1, false},
		{"row-error", "row-error", "GET", "/api/health", "", 503, 1, false},
		{"no-row", "no-row", "GET", "/api/health", "", 503, 1, false},
		{"unexpected-value", "wrong-value", "GET", "/api/health", "", 503, 1, false},
		{"timeout", "timeout", "GET", "/api/health", "", 503, 1, false},
		{"busy-pool", "pool", "GET", "/api/health", "", 503, 0, false},
		{"request-cancelled", "ok", "GET", "/api/health", "", 503, 0, true},
		{"closed-database", "closed", "GET", "/api/health", "", 503, 0, false},
		{"missing-database", "nil", "GET", "/api/health", "", 503, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			processStartedAt = time.Now().Add(-120 * time.Second)
			c := &healthConnector{t: t, mode: tc.mode}
			db := sql.OpenDB(c)
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { db.Close() })
			if tc.mode == "pool" {
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { conn.Close() })
			}
			if tc.mode == "closed" {
				db.Close()
			}
			routerDB := db
			if tc.mode == "nil" {
				routerDB = nil
			}
			r := &Router{db: routerDB, mux: http.NewServeMux(),
				cfg:         &config.Config{DatabaseURL: "postgres://private-user:private-password@private-host/private-db", SettingsEncryptionKey: "private-key"},
				authService: service.NewAuthService(repository.NewAuthRepository(db)),
			}
			r.setupRoutes()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			if tc.cancelled {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			started := time.Now()
			r.ServeHTTP(w, req)
			if elapsed := time.Since(started); elapsed > 1400*time.Millisecond {
				t.Errorf("DB 確認に時間がかかりすぎる: %s", elapsed)
			}
			if w.Code != tc.status || c.queries != tc.queries {
				t.Fatalf("status=%d queries=%d body=%s", w.Code, c.queries, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("headers=%v", w.Header())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			var keys []string
			for key := range body {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, []string{"commit", "status", "uptime_seconds"}) {
				t.Fatalf("公開するキーが変わった: %v", keys)
			}
			status := "ok"
			if tc.status == 503 {
				status = "unavailable"
			}
			uptime, ok := body["uptime_seconds"].(float64)
			if body["status"] != status || body["commit"] != "fixture-health-commit" || !ok || uptime < 120 || uptime > 122 || uptime != float64(int64(uptime)) {
				t.Fatalf("body=%v", body)
			}
		})
	}
}

func TestHealthAuthenticationBoundary(t *testing.T) {
	for _, path := range []string{"/api/health-report", "/api/health/extra", "/api/health%2Fextra", "/API/health", "/api/HEALTH", "/api/auth/me"} {
		if isPublicHealthRequest(httptest.NewRequest("GET", path, nil)) {
			t.Errorf("別の端点で認証を省いた: %s", path)
		}
	}
	if isPublicHealthRequest(httptest.NewRequest("POST", "/api/health", nil)) {
		t.Fatal("GET/HEAD 以外で認証を省いた")
	}
}

type healthConnector struct {
	t       *testing.T
	mode    string
	queries int
}

func (c *healthConnector) Connect(context.Context) (driver.Conn, error) { return &healthConn{c}, nil }
func (*healthConnector) Driver() driver.Driver                          { return healthDriver{} }

type healthDriver struct{}

func (healthDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use Connector") }

type healthConn struct{ c *healthConnector }

func (*healthConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected Prepare") }
func (*healthConn) Begin() (driver.Tx, error)           { return nil, errors.New("unexpected Begin") }
func (*healthConn) Close() error                        { return nil }
func (c *healthConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.c.queries++
	if query != "SELECT 1" || len(args) != 0 {
		c.c.t.Errorf("SQL=%q args=%v; want SELECT 1 with no args", query, args)
		return nil, errors.New("unexpected query")
	}
	deadline, ok := ctx.Deadline()
	if remaining := time.Until(deadline); !ok || remaining <= 0 || remaining > 1100*time.Millisecond {
		c.c.t.Errorf("DB に渡った期限が1秒以内ではない: deadline=%s present=%t", deadline, ok)
	}
	if c.c.mode == "query-error" {
		return nil, errors.New("postgres://private-user:private-password@private-host/private-db: sensitive driver details")
	}
	if c.c.mode == "timeout" {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
			return nil, errors.New("driver received no short timeout")
		}
	}
	return &healthRows{mode: c.c.mode}, nil
}

type healthRows struct {
	mode string
	done bool
}

func (*healthRows) Columns() []string { return []string{"value"} }
func (*healthRows) Close() error      { return nil }
func (r *healthRows) Next(dest []driver.Value) error {
	if r.mode == "row-error" {
		return errors.New("private-password: sensitive row details")
	}
	if r.done || r.mode == "no-row" {
		return io.EOF
	}
	r.done = true
	dest[0] = int64(1)
	if r.mode == "wrong-value" {
		dest[0] = int64(0)
	}
	return nil
}
