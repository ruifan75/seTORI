package handler

import (
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

// 陽性対照：4 項目が揃えば検証を通り過ぎる（service が nil なのでそこで panic する
// ＝検証では止まっていない）。これが無いと、上のテストは「常に 400」でも通る。
func TestUpdateAutoFillSettingsAcceptsAllFields(t *testing.T) {
	r := &Router{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/auto-fill/settings",
		strings.NewReader(`{"enabled":true,"interval_hours":6,"refresh_days":30,"include_collabs":false}`))
	defer func() {
		if recover() == nil && w.Code == http.StatusBadRequest {
			t.Errorf("4 項目が揃っているのに検証で止まった: %s", w.Body.String())
		}
	}()
	r.handleUpdateAutoFillSettings(w, req)
}
