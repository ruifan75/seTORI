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

// GET handler → service → repository → JSON を通す。converter 単体では
// 呼び出し側が Analysis: isEditor に戻る変更を検出できない。
// 偽 DB は WHERE を評価せず、SQL 全文・権限の受け渡し・応答欄の選別を検査する。
func TestStreamDetailVisibilityBoundary(t *testing.T) {
	raw, err := os.ReadFile("../repository/testdata/association_queries.json")
	if err != nil {
		t.Fatal(err)
	}
	var queries map[string]string
	if err := json.Unmarshal(raw, &queries); err != nil {
		t.Fatal(err)
	}
	const id = "hVfDBfreYNI"
	const perfID = "10000000-0000-0000-0000-000000000001"
	const songID = "10000000-0000-0000-0000-000000000002"
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	singerSelect := `SELECT s.id, s.name, s.english_name, s.photo_url, COALESCE(s.organization_override, s.organization),
 o.display_name, COALESCE(o.is_unaffiliated, FALSE), s.metadata_source, s.created_at, s.updated_at
 FROM singers s LEFT JOIN organizations o ON COALESCE(s.organization_override, s.organization) = o.key
 JOIN stream_singers ss ON s.id = ss.singer_id WHERE ss.stream_id = $1`
	singerRow := []driver.Value{"owner", "公開のチャンネル名", nil, nil, nil, nil, false, "manual", now, now}
	for _, user := range []struct {
		name               string
		perms              []string
		editor, privileged bool
	}{
		{"anonymous", nil, false, false}, {"logged-in", []string{}, false, false},
		{"editor", []string{"content:edit"}, true, false},
		{"restricted-view-only", []string{"restricted:view"}, false, true},
		{"both", []string{"content:edit", "restricted:view"}, true, true},
		{"admin", []string{"*"}, true, true},
	} {
		for _, hidden := range []bool{false, true} {
			for _, restricted := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/hidden=%t/restricted=%t", user.name, hidden, restricted), func(t *testing.T) {
					row := auditStreamRow(id, hidden, restricted)
					row[5] = []byte(`{"songs":[{"name":"analysis fixture","start":30,"end":60}]}`)
					row[7] = []byte(`[{"text":"analysis fixture"}]`)
					row[8] = []byte(`[{"start":30,"name":"analysis fixture","original_comment":"0:30 analysis fixture"}]`)
					row[9] = now
					row[10] = []byte(`[{"start_time":30,"title":"analysis fixture"}]`)
					row[11] = []byte(`[{"start":30,"name":"analysis fixture"}]`)
					row[12], row[15], row[16] = true, now, true
					steps := []auditSQLConnector{
						{query: auditWantStreamQuery(), args: []driver.Value{id}, columns: 24, rows: [][]driver.Value{row}},
						{query: `SELECT st.id, st.display_name, COALESCE(st.color, ''), st.created_at FROM stream_tags st
       JOIN stream_stream_tags sst ON st.id = sst.tag_id WHERE sst.stream_id = $1`, args: []driver.Value{id}, columns: 4,
							rows: [][]driver.Value{{"singing", "歌枠", "blue", now}}},
						{query: singerSelect, args: []driver.Value{id}, columns: 10, rows: [][]driver.Value{singerRow}},
						{query: singerSelect + " AND ss.is_owner = TRUE LIMIT 1", args: []driver.Value{id}, columns: 10, rows: [][]driver.Value{singerRow}},
					}
					view := "public"
					if user.privileged {
						view = "restricted"
					}
					showPerformances := !restricted || user.privileged
					performance := auditSQLConnector{query: queries["stream-"+view], args: []driver.Value{id}, columns: 16}
					if showPerformances {
						performance.rows = [][]driver.Value{{perfID, id, songID, int64(30), int64(60), int64(0), nil, "{}", now,
							"manual", true, "setlist fixture", "artist", nil, nil, restricted}}
					}
					steps = append(steps, performance)
					if showPerformances {
						array := `{"` + perfID + `"}`
						steps = append(steps,
							auditSQLConnector{query: queries["performance-tags"], args: []driver.Value{array}, columns: 5},
							auditSQLConnector{query: queries["performance-singers"], args: []driver.Value{array}, columns: 12},
							auditSQLConnector{query: queries["performance-artists"], args: []driver.Value{`{"` + songID + `"}`}, columns: 3},
						)
					}
					c := &auditSQLConnector{steps: steps}
					db := auditTestDB(t, c, len(steps))
					r := &Router{mux: http.NewServeMux(), streamService: service.NewStreamService(repository.NewStreamRepository(db), repository.NewPerformanceRepository(db))}
					r.setupRoutes()
					req := httptest.NewRequest("GET", "/api/streams/"+id, nil)
					if user.name != "anonymous" {
						req = withUser(req, &models.User{Permissions: user.perms})
					}
					w := httptest.NewRecorder()
					r.mux.ServeHTTP(w, req)
					if w.Code != 200 {
						t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
					}
					var response map[string]json.RawMessage
					if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					if string(response["id"]) != `"`+id+`"` || string(response["title"]) != `"title"` || string(response["is_hidden"]) != fmt.Sprint(hidden) || string(response["is_restricted"]) != fmt.Sprint(restricted) {
						t.Fatalf("配信の公開メタデータが変わった: %s", w.Body.String())
					}
					// GetByID が無視する関連取得エラーも、陽性の応答欄で検出する。
					for _, key := range []string{"participants", "channel_owner", "tags"} {
						if len(response[key]) == 0 || string(response[key]) == "[]" || string(response[key]) == "null" {
							t.Fatalf("公開欄 %s が消えた: %s", key, w.Body.String())
						}
					}
					wantAnalysis := user.editor && (!restricted || user.privileged)
					for _, key := range []string{"holodex_timeline_songs", "comment_timeline_songs", "chapter_timeline_songs", "comment_songs_analyzed_at", "has_comment_raw", "chapter_count", "holodex_uploaded_at", "holodex_upload_unknown"} {
						_, present := response[key]
						if present != wantAnalysis {
							t.Fatalf("%s present=%t want=%t body=%s", key, present, wantAnalysis, w.Body.String())
						}
					}
					if strings.Contains(w.Body.String(), "analysis fixture") != wantAnalysis {
						t.Fatalf("解析の原文の可視性: %s", w.Body.String())
					}
					_, operational := response["is_processed"]
					if operational != user.editor {
						t.Fatalf("運用の状態 present=%t want=%t", operational, user.editor)
					}
					var perfs []json.RawMessage
					if err := json.Unmarshal(response["performances"], &perfs); err != nil {
						t.Fatal(err)
					}
					wantCount := 0
					if showPerformances {
						wantCount = 1
					}
					if len(perfs) != wantCount || strings.Contains(w.Body.String(), "setlist fixture") != showPerformances {
						t.Fatalf("歌唱の可視性: %s", w.Body.String())
					}
				})
			}
		}
	}
}
