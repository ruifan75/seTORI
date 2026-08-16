package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruifan75/setori/pkg/auth"
)

func TestActivityEndpointPermissions(t *testing.T) {
	cases := []struct {
		method    string
		path      string
		wantPerm  string
		wantLogin bool
	}{
		{http.MethodPost, "/api/activity/visit", "", false},
		{http.MethodGet, "/api/activity/policy", "", false},
		{http.MethodGet, "/api/activity", auth.PermUsersManage, true},
		{http.MethodGet, "/api/activity/stats", auth.PermUsersManage, true},
		{http.MethodGet, "/api/activity/users", auth.PermUsersManage, true},
		{http.MethodPost, "/api/users/00000000-0000-0000-0000-000000000000/revoke-sessions", auth.PermUsersManage, true},
	}

	for _, tc := range cases {
		perm, login := requiredPermission(tc.method, tc.path)
		if perm != tc.wantPerm || login != tc.wantLogin {
			t.Errorf("%s %s = (%q,%v), want (%q,%v)", tc.method, tc.path, perm, login, tc.wantPerm, tc.wantLogin)
		}
	}
}

func TestActivityOriginAllowed(t *testing.T) {
	cases := []struct {
		name     string
		host     string
		origin   string
		frontend string
		want     bool
	}{
		{"same origin", "setori.app", "https://setori.app", "https://setori.app", true},
		{"configured dev frontend", "localhost:8080", "http://localhost:5173", "http://localhost:5173", true},
		{"cross site", "setori.app", "https://evil.example", "https://setori.app", false},
		{"invalid origin", "setori.app", "not a url", "https://setori.app", false},
		{"non browser request", "setori.app", "", "https://setori.app", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/activity/visit", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if got := activityOriginAllowed(req, tc.frontend); got != tc.want {
				t.Fatalf("activityOriginAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}
