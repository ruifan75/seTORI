package handler

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

func TestProcessedListPassesViewerAccess(t *testing.T) {
	for _, permissions := range [][]string{{"content:edit"}, {"content:edit", "restricted:view"}, {"*"}} {
		t.Run(fmt.Sprint(permissions), func(t *testing.T) {
			restriction := "NOT COALESCE(s.restriction_override, " + auditWantAutoRestricted("s") + ")"
			if len(permissions) > 1 || permissions[0] == "*" {
				restriction = "TRUE"
			}
			where := "s.is_processed <> $1 AND (EXISTS (SELECT 1 FROM performances p WHERE p.stream_id = s.id) AND " + restriction + ")"
			c := &auditSQLConnector{steps: []auditSQLConnector{
				{query: "SELECT COUNT(*) FROM streams s WHERE " + where, args: []driver.Value{true}, columns: 1, rows: [][]driver.Value{{int64(1)}}},
				{query: "SELECT s.id, s.title, s.stream_date, s.is_hidden, s.is_processed FROM streams s WHERE " + where + " ORDER BY s.stream_date DESC, s.id ASC LIMIT $2 OFFSET $3", args: []driver.Value{true, int64(100), int64(0)}, columns: 5, rows: [][]driver.Value{{"public00001", "title", time.Unix(1, 0), true, false}}},
			}}
			r := &Router{processedReview: repository.NewProcessedReviewRepository(auditTestDB(t, c, 2))}
			req := withUser(httptest.NewRequest("GET", "/api/processed-review?has_performances=true", nil), &models.User{Permissions: permissions})
			w := httptest.NewRecorder()
			r.handleProcessedCandidates(w, req)
			var response struct {
				Candidates []repository.ProcessedCandidate `json:"candidates"`
				Total      int                             `json:"total"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || response.Total != 1 || len(response.Candidates) != 1 || response.Candidates[0].ID != "public00001" {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
func TestProcessedFiltersRejectUnknownValues(t *testing.T) {
	for _, query := range []string{"is_processed=TRUE", "is_processed=all", "hidden=no", "has_performances=0", "from=2026-10-01", "until=bad", "from=2026-10-02T00:00:00Z&until=2026-10-01T00:00:00Z", "from=2026-10-01T00:00:00Z&until=2026-10-01T00:00:00Z", strings.Repeat("tag=singing&", 21)} {
		req := httptest.NewRequest("GET", "/api/processed-review?"+query, nil)
		w := httptest.NewRecorder()
		(&Router{}).handleProcessedCandidates(w, req)
		if w.Code != 400 {
			t.Fatal(query, w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("GET", "/api/processed-review?is_processed=false&hidden=true&has_performances=false&from=2026-10-01T00:00:00Z&until=2026-10-02T00:00:00Z", nil)
	f, err := parseProcessedFilters(req)
	if err != nil || f.AfterProcessed || f.Hidden == nil || !*f.Hidden || f.HasPerformances == nil || *f.HasPerformances || f.From == nil || f.Until == nil {
		t.Fatal(f, err)
	}
}
func TestProcessedInputStatusCodes(t *testing.T) {
	for _, body := range []string{`{}`, `{"stream_ids":["video"]}`, `{"stream_ids":["video"],"is_processed":null}`, `{"stream_ids":["video"],"is_processed":"false"}`} {
		w := httptest.NewRecorder()
		(&Router{}).handleProcessedPreview(w, httptest.NewRequest("POST", "/api/processed-review/preview", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
	for _, fn := range []func(*httptest.ResponseRecorder){
		func(w *httptest.ResponseRecorder) {
			(&Router{}).handleProcessedApply(w, httptest.NewRequest("POST", "/api/processed-review/runs/invalid/apply", nil))
		},
		func(w *httptest.ResponseRecorder) {
			(&Router{}).handleProcessedRevert(w, httptest.NewRequest("POST", "/api/processed-review/runs/invalid/revert", nil))
		},
	} {
		w := httptest.NewRecorder()
		fn(w)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
