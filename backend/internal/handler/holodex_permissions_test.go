package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruifan75/setori/internal/models"
)

// 本物のルート登録で端点を確認し、生成された認可の結果を完全一致で検査する。
// 期待値は実装の権限定数から作らず、公開する権限キーの契約を固定する。
func TestHolodexSyncEndpointPermissions(t *testing.T) {
	r := &Router{mux: http.NewServeMux()}
	r.setupRoutes()
	cases := []struct {
		path, pattern, permission string
	}{
		{"/api/sync/holodex", "POST /api/sync/holodex", "sync:run"},
		{"/api/sync/holodex/video/hVfDBfreYNI", "POST /api/sync/holodex/video/{id}", "sync:run"},
		{"/api/sync/holodex/to-holodex/hVfDBfreYNI", "POST /api/sync/holodex/to-holodex/{id}", "holodex:upload"},
		{"/api/sync/holodex/%74o-holodex/hVfDBfreYNI", "POST /api/sync/holodex/to-holodex/{id}", "holodex:upload"},
		{"/api/sync/%68olodex/to-holodex/.%2FhVfDBfreYNI", "POST /api/sync/holodex/to-holodex/{id}", "holodex:upload"},
	}
	users := []struct {
		name        string
		user        *models.User
		read, write bool
	}{
		{"anonymous", nil, false, false},
		{"viewer", &models.User{}, false, false},
		{"content only", &models.User{Permissions: []string{"content:edit"}}, false, false},
		{"sync only", &models.User{Permissions: []string{"sync:run"}}, true, false},
		{"default editor", &models.User{Permissions: []string{"content:edit", "sync:run", "logs:view"}}, true, false},
		{"upload only", &models.User{Permissions: []string{"holodex:upload"}}, false, true},
		{"both", &models.User{Permissions: []string{"sync:run", "holodex:upload"}}, true, true},
		{"admin wildcard", &models.User{Permissions: []string{"*"}}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			if _, pattern := r.mux.Handler(req); pattern != tc.pattern {
				t.Fatalf("ServeMux pattern = %q, want %q", pattern, tc.pattern)
			}
			perm, login := requiredPermission(req.Method, authzPath(req.URL.EscapedPath()))
			if perm != tc.permission || !login {
				t.Fatalf("requiredPermission = (%q, %v), want (%q, true)", perm, login, tc.permission)
			}
			for _, u := range users {
				t.Run(u.name, func(t *testing.T) {
					want := u.read
					if tc.permission == "holodex:upload" {
						want = u.write
					}
					if got := authorizeRequest(req, u.user); got != want {
						t.Fatalf("authorizeRequest = %v, want %v", got, want)
					}
				})
			}
		})
	}
}

func TestHolodexUploadSubpathsStayProtected(t *testing.T) {
	// 再送・削除等の別端点は現時点ではない。配下に将来足した場合の認可を固定する。
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodGet, http.MethodHead} {
		for _, path := range []string{
			"/api/sync/holodex/to-holodex",
			"/api/sync/holodex/to-holodex/",
			"/api/sync/holodex/to-holodex/hVfDBfreYNI/retry",
			"/api/sync/holodex/to-holodex/hVfDBfreYNI/status",
		} {
			perm, login := requiredPermission(method, path)
			if perm != "holodex:upload" || !login {
				t.Errorf("%s %s = (%q, %v), want (holodex:upload, true)", method, path, perm, login)
			}
		}
	}
	// 類似名の別ルートは読み取り同期の権限のまま。OPTIONS は共通の CORS 処理。
	for _, path := range []string{"/api/sync/holodex/to-holodex-report", "/api/sync/holodex/to-holodex-report/id"} {
		if perm, login := requiredPermission(http.MethodPost, path); perm != "sync:run" || !login {
			t.Errorf("%s = (%q, %v), want (sync:run, true)", path, perm, login)
		}
	}
	if perm, login := requiredPermission(http.MethodOptions, "/api/sync/holodex/to-holodex/id"); perm != "" || login {
		t.Errorf("OPTIONS = (%q, %v), want (empty, false)", perm, login)
	}
}
