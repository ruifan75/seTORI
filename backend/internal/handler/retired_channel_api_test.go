package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
)

// 廃止した API は認証状態によらず 404。新 API の権限・応答は固定表と
// TestChannelAPI / TestChannelWireContract が引き続き検査する。
func TestRetiredSingerAPI(t *testing.T) {
	paths := []string{
		"/api/singers", "/api/singers?include_hidden=true", "/api/singers/search?q=channel",
		"/api/singers/channel", "/api/singers/channel/streams", "/api/singers/channel/performances",
		"/api/singers/channel/visibility", "/api/singers/channel/members-policy",
		"/api/singers/channel/organization", "/api/singers/channel/auto-fill", "/api/singers/auto-fill",
		"/api/singers/", "/api/singers/auto-fill/child", "/api/singers/.%2Fchannel/performances",
		"/api/%73ingers", "/%61pi/%73ingers/%61uto-fill", "/api/singers/channel/%76isibility",
		"/api/%73ingers/a%2Fb/performances",
	}
	users := []struct{ token, permissions string }{
		{"", "{}"}, {"viewer", "{}"}, {"editor", "{content:edit}"},
		{"restricted", "{restricted:view}"}, {"both", "{content:edit,restricted:view}"}, {"admin", "{*}"},
	}
	for _, path := range paths {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"} {
			for _, user := range users {
				t.Run(method+path+"/"+user.token, func(t *testing.T) {
					fixture := &channelAliasDB{permissions: user.permissions}
					db := sql.OpenDB(fixture)
					defer db.Close()
					r := &Router{mux: http.NewServeMux(), authService: service.NewAuthService(repository.NewAuthRepository(db))}
					r.setupRoutes()
					req := httptest.NewRequest(method, path, strings.NewReader(`{"is_hidden":true}`))
					if user.token != "" {
						req.Header.Set("Authorization", "Bearer "+user.token)
					}
					if _, pattern := r.mux.Handler(req); pattern != "" {
						t.Fatalf("retired route is still registered: %s", pattern)
					}
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)
					if w.Code != 404 {
						t.Fatalf("status=%d want=404 body=%s", w.Code, w.Body.String())
					}
					if fixture.unexpected != "" {
						t.Fatal(fixture.unexpected)
					}
				})
			}
		}
	}
}
