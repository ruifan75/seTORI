package handler

import (
	"encoding/json"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// **include_collabs も必須**（issue #60）。項目が無いのを false と読むと、
// 古い画面から保存しただけで客串の対象が黙って外れる。
func TestUpdateAutoFillSettingsRequiresIncludeCollabs(t *testing.T) {
	r := &Router{}
	for _, body := range []string{
		`{"enabled":true,"interval_hours":6,"refresh_days":30}`,
		`{"enabled":true,"interval_hours":6,"refresh_days":30,"include_collabs":null}`,
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/auto-fill/settings", strings.NewReader(body))
		r.handleUpdateAutoFillSettings(w, req)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "include_collabs") {
			t.Errorf("body=%s: status=%d %s, want 400（include_collabs が必須）", body, w.Code, w.Body.String())
		}
	}
}

// 保存と取得の handler を実際に通し、true/false の両方を応答と DB で確認する。
func TestUpdateAutoFillSettingsAcceptsAllFields(t *testing.T) {
	db := reviewTestDB(t)
	settings := repository.NewAppSettingsRepository(db)
	svc := service.NewAutoFillService(settings, nil, nil, nil, nil, nil)
	r := &Router{autoFillService: svc}
	for _, collabs := range []bool{true, false} {
		body := `{"enabled":true,"interval_hours":6,"refresh_days":30,"include_collabs":false}`
		if collabs {
			body = strings.Replace(body, `"include_collabs":false`, `"include_collabs":true`, 1)
		}
		w := httptest.NewRecorder()
		r.handleUpdateAutoFillSettings(w, httptest.NewRequest(http.MethodPut, "/api/auto-fill/settings", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
		}
		assertCollabs := func(w *httptest.ResponseRecorder) {
			t.Helper()
			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["include_collabs"] != collabs || got["enabled"] != true || got["refresh_days"] != float64(30) {
				t.Fatalf("設定の応答: %v", got)
			}
		}
		assertCollabs(w)
		get := httptest.NewRecorder()
		r.handleGetAutoFillSettings(get, httptest.NewRequest(http.MethodGet, "/api/auto-fill/settings", nil))
		if get.Code != http.StatusOK {
			t.Fatalf("GET: %d", get.Code)
		}
		assertCollabs(get)
		// 欠落した古い形式の PUT は保存済みの旗を変更しない。
		bad := httptest.NewRecorder()
		r.handleUpdateAutoFillSettings(bad, httptest.NewRequest(http.MethodPut, "/api/auto-fill/settings", strings.NewReader(`{"enabled":true,"interval_hours":6,"refresh_days":30}`)))
		if bad.Code != http.StatusBadRequest || svc.GetSettings().IncludeCollabs != collabs {
			t.Fatalf("欠落した PUT が設定を変えた: %d %+v", bad.Code, svc.GetSettings())
		}
	}
}
