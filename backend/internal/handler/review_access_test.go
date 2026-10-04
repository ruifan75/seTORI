package handler

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
)

func TestTagGapListsPassViewerAccess(t *testing.T) {
	data, err := os.ReadFile("testdata/tag_gaps.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, privileged := range []bool{false, true} {
		t.Run(fmt.Sprint(privileged), func(t *testing.T) {
			perms := []string{"content:edit"}
			restriction := "NOT COALESCE(st.restriction_override, " + auditWantAutoRestricted("st") + ")"
			if privileged {
				perms = append(perms, "restricted:view")
				restriction = "TRUE"
			}
			gapRow := []driver.Value{"499a61c3-9ab3-49ca-a6a5-706fd755c264", "public00001", "title", int64(30), "527b14c9-f292-4e20-836c-9c41c0ae944d", "song", "artist", "{}", "{piano}", "{comment}", "song", true}
			dismissedRow := []driver.Value{"499a61c3-9ab3-49ca-a6a5-706fd755c264", "piano", "public00001", "title", int64(30), "song", "editor", time.Now()}
			c := &auditSQLConnector{steps: []auditSQLConnector{
				{query: strings.ReplaceAll(string(data), "{{RESTRICTION}}", restriction), args: []driver.Value{int64(300)}, rows: [][]driver.Value{gapRow}, columns: 12},
				{query: `SELECT k.performance_id, k.tag_id, p.stream_id, st.title, p.start_seconds, so.name,
 COALESCE(NULLIF(u.display_name, ''), u.username, ''), k.checked_at
 FROM performance_tag_checks k JOIN performances p ON p.id = k.performance_id
 JOIN streams st ON st.id = p.stream_id JOIN songs so ON so.id = p.song_id
 LEFT JOIN users u ON u.id = k.checked_by WHERE ` + restriction + ` ORDER BY k.checked_at DESC LIMIT $1`, args: []driver.Value{int64(300)}, rows: [][]driver.Value{dismissedRow}, columns: 8},
			}}
			r := &Router{tagRepo: repository.NewTagRepository(auditTestDB(t, c, 2))}
			req := withUser(httptest.NewRequest("GET", "/api/tag-gaps", nil), &models.User{Permissions: perms})
			w := httptest.NewRecorder()
			r.handleListTagGaps(w, req)
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var response struct {
				Gaps      []repository.TagGapRow          `json:"gaps"`
				Dismissed []repository.TagGapDismissalRow `json:"dismissed"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Gaps) != 1 || len(response.Dismissed) != 1 || response.Gaps[0].SongName != "song" || response.Dismissed[0].StartSeconds != 30 {
				t.Fatalf("response=%+v", response)
			}
		})
	}
}

func TestNonSingingCandidatesPassViewerAccess(t *testing.T) {
	for _, privileged := range []bool{false, true} {
		for _, dismissed := range []bool{false, true} {
			t.Run(fmt.Sprintf("privileged=%t/dismissed=%t", privileged, dismissed), func(t *testing.T) {
				perms := []string{"content:edit"}
				restriction := "NOT COALESCE(s.restriction_override, " + auditWantAutoRestricted("s") + ")"
				if privileged {
					perms = append(perms, "restricted:view")
					restriction = "TRUE"
				}
				exists := "NOT EXISTS"
				if dismissed {
					exists = "EXISTS"
				}
				query := `SELECT s.id, s.title, s.stream_date, jsonb_array_length(s.comment_songs) AS song_count,
 s.comment_songs_analyzed_at,
 COALESCE((SELECT array_agg(sst.tag_id ORDER BY sst.tag_id) FROM stream_stream_tags sst WHERE sst.stream_id = s.id), '{}')
 FROM streams s WHERE s.is_hidden AND ` + restriction + `
 AND jsonb_typeof(s.comment_songs) = 'array' AND jsonb_array_length(s.comment_songs) > 0
 AND ` + exists + ` (SELECT 1 FROM non_singing_checks c WHERE c.stream_id = s.id)
 ORDER BY jsonb_array_length(s.comment_songs) DESC, s.stream_date DESC LIMIT $1`
				c := &auditSQLConnector{query: query, args: []driver.Value{int64(100)}, columns: 6, rows: [][]driver.Value{{"public00001", "title", time.Now(), int64(2), nil, "{singing}"}}}
				streamRepo := repository.NewStreamRepository(auditTestDB(t, c, 1))
				r := &Router{streamService: service.NewStreamService(streamRepo, nil)}
				req := withUser(httptest.NewRequest("GET", fmt.Sprintf("/api/non-singing-candidates?dismissed=%t", dismissed), nil), &models.User{Permissions: perms})
				w := httptest.NewRecorder()
				r.handleListNonSingingCandidates(w, req)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"song_count":2`) {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
			})
		}
	}
}
