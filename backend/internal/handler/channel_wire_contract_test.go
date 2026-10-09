package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
)

type channelWireResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// 改名前の Router を実行して採った固定の応答。新しい実装から期待値は作らない。
// 権限別の省略と、新 API の一覧/詳細/更新のキー・値を丸ごと比較する。
func TestChannelWireContract(t *testing.T) {
	fixtures := map[string]channelWireResponse{}
	data, err := os.ReadFile("testdata/channel_wire_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ method, suffix, body string }{
		{"GET", "?include_hidden=true", ""}, {"GET", "?group=organization&include_hidden=true", ""},
		{"GET", "/search?q=channel", ""}, {"GET", "/channel", ""},
		{"GET", "/channel/streams?hidden=all&processed=true", ""}, {"GET", "/channel/performances", ""},
		{"POST", "", `{}`}, {"PUT", "/channel", `{"name":"updated"}`},
		{"PUT", "/channel/visibility", `{"is_hidden":true}`}, {"PUT", "/channel/members-policy", `{"members_only_policy":"allow"}`},
		{"PUT", "/channel/auto-fill", `{"auto_fill_enabled":true}`}, {"GET", "/auto-fill", ""},
		{"PUT", "/channel/organization", `{"organization":""}`},
	}
	users := []struct{ token, permissions string }{
		{"", "{}"}, {"viewer", "{}"}, {"editor", "{content:edit}"}, {"restricted", "{restricted:view}"},
		{"both", "{content:edit,restricted:view}"}, {"admin", "{*}"},
	}
	for _, tc := range cases {
		for _, u := range users {
			key := tc.method + " " + tc.suffix + " " + u.token
			t.Run(key, func(t *testing.T) {
				for _, prefix := range []string{"/api/channels"} {
					fixture := &channelAliasDB{permissions: u.permissions}
					db := sql.OpenDB(fixture)
					defer db.Close()
					r := &Router{mux: http.NewServeMux(), authService: service.NewAuthService(repository.NewAuthRepository(db)),
						channelService: service.NewChannelService(repository.NewChannelRepository(db), repository.NewStreamRepository(db), repository.NewPerformanceRepository(db))}
					r.setupRoutes()
					req := httptest.NewRequest(tc.method, prefix+tc.suffix, strings.NewReader(tc.body))
					if u.token != "" {
						req.Header.Set("Authorization", "Bearer "+u.token)
					}
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)
					if fixture.unexpected != "" {
						t.Fatal(fixture.unexpected)
					}
					body := json.RawMessage(w.Body.Bytes())
					want, ok := fixtures[key]
					if !ok {
						t.Fatalf("missing contract for %s", key)
					}
					var g, v any
					if err := json.Unmarshal(body, &g); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(want.Body, &v); err != nil {
						t.Fatal(err)
					}
					if w.Code != want.Status || !reflect.DeepEqual(g, v) {
						t.Fatalf("%s: response=(%d,%s) want=(%d,%s)", prefix, w.Code, body, want.Status, want.Body)
					}
				}
			})
		}
	}
	if len(fixtures) != len(cases)*len(users) {
		t.Fatal("contract has extra or missing cases")
	}
}
