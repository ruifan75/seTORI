package handler

import (
	"database/sql/driver"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
)

// 単件・強制・一括承認が一覧と同じ対象の視界で提案を引くことを、発行 SQL 全体で固定する。
func TestSuggestionApprovalLookupUsesViewerAccess(t *testing.T) {
	data, err := os.ReadFile("testdata/suggestion_view.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, privileged := range []bool{false, true} {
		for _, action := range []string{"approve", "force", "batch"} {
			t.Run(fmt.Sprintf("%t/%s", privileged, action), func(t *testing.T) {
				id := uuid.MustParse("527b14c9-f292-4e20-836c-9c41c0ae944d")
				query := strings.ReplaceAll(string(data), "{{RESTRICTION}}", "NOT COALESCE(st.restriction_override, "+auditWantAutoRestricted("st")+")")
				perms := []string{"content:edit"}
				var rows [][]driver.Value
				if privileged {
					perms = append(perms, "restricted:view")
					query = `SELECT id, target_type, target_id, target_key, target_label, kind, before_data, after_data, payload, note, status,
 created_by, created_by_name, client_hint, reviewed_by, review_note, created_at, reviewed_at FROM edit_suggestions WHERE id = $1`
					// 既処理の行が読めたところで止まる。ここは衝突判定ではなく参照する SQL の検査。
					rows = [][]driver.Value{{id.String(), "performance", id.String(), "", "private target", "field", []byte(`{"end_seconds":"100"}`), []byte(`{"end_seconds":"110"}`), []byte("{}"), "", "approved", nil, "", "", nil, "", time.Now(), nil}}
				}
				c := &auditSQLConnector{query: query, args: []driver.Value{id.String()}, columns: 18, rows: rows}
				svc := service.NewSuggestionService(repository.NewSuggestionRepository(auditTestDB(t, c, 1)), nil, nil, nil, nil, nil)
				user := &models.User{ID: uuid.New(), Permissions: perms}
				if action == "batch" {
					response := svc.BatchReview([]uuid.UUID{id}, "approve", user, false, "")
					expected := service.ErrSuggestionNotFound.Error()
					if privileged {
						expected = service.ErrAlreadyReviewed.Error()
					}
					if response.Failed != 1 || len(response.Results) != 1 || response.Results[0].Error != expected {
						t.Fatalf("response=%+v", response)
					}
				} else {
					r := &Router{suggestionService: svc}
					path := "/api/suggestions/" + id.String() + "/approve"
					if action == "force" {
						path += "?force=1"
					}
					req := withUser(httptest.NewRequest("POST", path, nil), user)
					req.SetPathValue("id", id.String())
					w := httptest.NewRecorder()
					r.handleApproveSuggestion(w, req)
					status := 404
					expected := service.ErrSuggestionNotFound.Error()
					if privileged {
						status = 409
						expected = service.ErrAlreadyReviewed.Error()
					}
					if w.Code != status || !strings.Contains(w.Body.String(), expected) {
						t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
					}
				}
			})
		}
	}
}
