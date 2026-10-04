package handler

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ruifan75/setori/internal/models"
)

type auditedRoute struct {
	Pattern    string `json:"pattern"`
	Handler    string `json:"handler"`
	Permission string `json:"permission"`
	Login      bool   `json:"login"`
	Analysis   bool   `json:"analysis"`
}

// 期待値は人が監査した固定の表。登録から権限を推論したり、実装から作らない。
// AST は登録の棚卸しにだけ使う。認可はリクエストから実際の関数を呼んで検査する。
func TestAllRegisteredRoutePermissions(t *testing.T) {
	data, err := os.ReadFile("testdata/route_permissions.json")
	if err != nil {
		t.Fatal(err)
	}
	var routes []auditedRoute
	if err := json.Unmarshal(data, &routes); err != nil {
		t.Fatal(err)
	}
	expected := make(map[string]auditedRoute)
	for _, route := range routes {
		if _, found := expected[route.Pattern]; found {
			t.Fatalf("duplicate audit row: %s", route.Pattern)
		}
		expected[route.Pattern] = route
	}
	// 全 handler ファイルを見るので、setupRoutes の外へ登録を移しても棚卸しから落ちない。
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(info os.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (selector.Sel.Name != "HandleFunc" && selector.Sel.Name != "Handle") {
					return true
				}
				if len(call.Args) != 2 {
					t.Error("route registration must have 2 arguments")
					return true
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Error("dynamic route registration requires an explicit audit")
					return true
				}
				pattern, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Error(err)
					return true
				}
				want, found := expected[pattern]
				if !found {
					t.Errorf("unclassified registered route: %s; add an audited permission row", pattern)
					return true
				}
				if seen[pattern] {
					t.Errorf("duplicate registered route: %s", pattern)
				}
				seen[pattern] = true
				arg := call.Args[1]
				analysis := false
				if wrapper, ok := arg.(*ast.CallExpr); ok {
					fn, ok := wrapper.Fun.(*ast.SelectorExpr)
					if !ok || fn.Sel.Name != "withAnalysisAccess" || len(wrapper.Args) != 1 {
						t.Errorf("unclassified route wrapper: %s", pattern)
						return true
					}
					analysis = true
					arg = wrapper.Args[0]
				}
				fn, ok := arg.(*ast.SelectorExpr)
				if !ok || fn.Sel.Name != want.Handler || analysis != want.Analysis {
					t.Errorf("%s handler/analysis guard differs from audit", pattern)
				}
				return true
			})
		}
	}
	for pattern := range expected {
		if !seen[pattern] {
			t.Errorf("audit row without registration: %s", pattern)
		}
	}
	if t.Failed() {
		return
	}
	r := &Router{mux: http.NewServeMux()}
	r.setupRoutes()
	probe := &Router{mux: http.NewServeMux()}
	for _, route := range routes {
		probe.mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	}
	for _, route := range routes {
		t.Run(route.Pattern, func(t *testing.T) {
			parts := strings.SplitN(route.Pattern, " ", 2)
			method := parts[0]
			segments := strings.Split(parts[1], "/")
			for i, seg := range segments {
				if strings.HasPrefix(seg, "{") {
					segments[i] = "audit-id"
				}
			}
			// 通常、全セグメントの文字を符号化、wildcard 内に符号化された '/' を含む形。
			paths := []string{strings.Join(segments, "/")}
			encoded := append([]string{}, segments...)
			for i, seg := range encoded {
				if seg != "" {
					encoded[i] = "%" + strconv.FormatInt(int64(seg[0]), 16) + seg[1:]
				}
			}
			paths = append(paths, strings.Join(encoded, "/"))
			for i, seg := range strings.Split(parts[1], "/") {
				if strings.HasPrefix(seg, "{") {
					slashed := append([]string{}, segments...)
					slashed[i] = ".%2Faudit-id"
					paths = append(paths, strings.Join(slashed, "/"))
				}
			}
			methods := []string{method}
			if method == http.MethodGet {
				methods = append(methods, http.MethodHead)
			}
			for _, method := range methods {
				for _, path := range paths {
					req := httptest.NewRequest(method, path, nil)
					if _, pattern := r.mux.Handler(req); pattern != route.Pattern {
						t.Fatalf("%s %s routes to %q", method, path, pattern)
					}
					perm, login := requiredPermission(method, authzPath(req.URL.EscapedPath()))
					if perm != route.Permission || login != route.Login {
						t.Errorf("%s %s permission=(%q,%t), want (%q,%t)", method, path, perm, login, route.Permission, route.Login)
					}
					for _, user := range []*models.User{nil, {Permissions: []string{}}, {Permissions: []string{"unrelated:permission"}}, {Permissions: []string{route.Permission}}, {Permissions: []string{"*"}}} {
						want := !route.Login || user != nil && route.Permission == ""
						if user != nil {
							for _, permission := range user.Permissions {
								want = want || permission == "*" || permission == route.Permission
							}
						}
						if authorizeRequest(req, user) != want {
							t.Errorf("%s %s authorization differs for %+v", method, path, user)
						}
					}
					w := httptest.NewRecorder()
					probe.ServeHTTP(w, req)
					status := http.StatusNoContent
					if route.Login {
						status = http.StatusUnauthorized
					}
					if w.Code != status {
						t.Errorf("middleware status=%d, want %d", w.Code, status)
					}
				}
			}
		})
	}
}

// 登録前の文字列判定も境界を固定する。新しい実ルートの分類は上の固定表で必須にする。
func TestPermissionFamilyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		path, permission string
		login            bool
	}{
		{"/api/readings", "content:edit", true}, {"/api/filter-keywords", "content:edit", true},
		{"/api/tag-keyword-rules", "content:edit", true}, {"/api/songs/identity-checks", "content:edit", true},
		{"/api/suggestions", "content:edit", true}, {"/api/aliases", "content:edit", true},
		{"/api/tag-gaps", "content:edit", true}, {"/api/users", "users:manage", true},
		{"/api/roles", "users:manage", true}, {"/api/activity", "users:manage", true},
		{"/api/settings", "users:manage", true}, {"/api/ai-providers", "ai:manage", true},
		{"/api/logs", "logs:view", true}, {"/api/sync", "sync:run", true},
		{"/api/backups", "backup:manage", true}, {"/api/tasks", "content:edit", true},
		{"/api/restriction-review", "content:edit", true}, {"/api/auto-fill", "content:edit", true},
		{"/api/non-singing-candidates", "content:edit", true}, {"/api/streams/batch-fill", "content:edit", true},
		{"/api/streams/batch-analyze", "content:edit", true}, {"/api/singers/auto-fill", "content:edit", true}, {"/api/channels/auto-fill", "content:edit", true},
	} {
		for _, method := range []string{"GET", "HEAD"} {
			for _, path := range []string{tc.path, tc.path + "/audit-subresource"} {
				perm, login := requiredPermission(method, path)
				if perm != tc.permission || login != tc.login {
					t.Errorf("%s %s=(%q,%t)", method, path, perm, login)
				}
			}
			perm, login := requiredPermission(method, tc.path+"-report")
			if perm != "" || login {
				t.Errorf("sibling %s %s=(%q,%t)", method, tc.path, perm, login)
			}
		}
	}
	for _, tc := range []struct {
		method, path, permission string
		login                    bool
	}{
		{"GET", "/api/shared/playlists/slug", "", false}, {"HEAD", "/api/shared/playlists/slug/items", "", false},
		{"POST", "/api/shared/playlists/slug", "content:edit", true}, {"DELETE", "/api/shared/playlists/slug/items", "content:edit", true},
		{"POST", "/api/playlists/public", "", true}, {"DELETE", "/api/playlists/public/items", "", true},
		{"POST", "/api/playlists-report", "content:edit", true}, {"POST", "/api/presets-report", "content:edit", true},
		{"POST", "/api/auth/oauth-report", "content:edit", true},
	} {
		perm, login := requiredPermission(tc.method, tc.path)
		if perm != tc.permission || login != tc.login {
			t.Errorf("%s %s=(%q,%t) want (%q,%t)", tc.method, tc.path, perm, login, tc.permission, tc.login)
		}
	}
}
