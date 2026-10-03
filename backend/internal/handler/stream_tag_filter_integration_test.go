package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/auth"
)

func streamTagFixture(t *testing.T, db *sql.DB) *Router {
	t.Helper()
	_, err := db.Exec(`INSERT INTO singers(id,name,is_hidden,members_only_policy) VALUES
 ('visible','表示',false,NULL), ('hidden','非表示',true,NULL), ('allowed','許可済み',false,'allow');
 INSERT INTO streams(id,title,stream_date,is_hidden,restriction_override) VALUES
 ('both','両方','2026-01-01',false,NULL), ('singing_only','歌枠','2026-01-02',false,NULL),
 ('3d_only','3D','2026-01-03',false,NULL), ('untagged','タグなし','2026-01-04',false,NULL),
 ('restricted','会限','2026-01-05',false,NULL), ('allowed','会限公開可','2026-01-06',false,NULL),
 ('override','例外公開可','2026-01-07',false,false), ('multiowner','所有者が複数','2026-01-08',false,NULL),
 ('hidden_stream','非表示配信','2026-01-09',true,NULL), ('hidden_channel','非表示チャンネル','2026-01-10',false,NULL),
 ('guest','表示中の参加者','2026-01-11',false,NULL), ('no_channel','参加者なし','2026-01-12',false,NULL);
 INSERT INTO stream_singers(stream_id,singer_id,is_owner) VALUES
 ('both','visible',true), ('both','allowed',false), ('singing_only','visible',true),
 ('3d_only','visible',true), ('untagged','visible',true), ('restricted','visible',true),
 ('allowed','allowed',true), ('override','visible',true), ('multiowner','visible',true), ('multiowner','allowed',true),
 ('hidden_stream','visible',true), ('hidden_channel','hidden',true), ('guest','hidden',true), ('guest','visible',false);
 INSERT INTO stream_stream_tags(stream_id,tag_id)
 SELECT id,'singing' FROM streams WHERE id NOT IN ('3d_only','untagged');
 INSERT INTO stream_stream_tags(stream_id,tag_id)
 SELECT id,'3d' FROM streams WHERE id NOT IN ('singing_only','untagged');
 INSERT INTO stream_stream_tags(stream_id,tag_id)
 SELECT id,'members_only' FROM streams WHERE id IN ('restricted','allowed','override','multiowner');
 INSERT INTO songs(name,original_artist) VALUES ('秘匿される曲','原曲作者');
 INSERT INTO performances(stream_id,song_id,start_seconds,end_seconds,order_index)
 SELECT 'restricted',id,10,100,1 FROM songs WHERE name='秘匿される曲';
 UPDATE streams SET holodex_data='{"songs":[{"name":"解析の曲"}]}',comment_raw='[{"text":"元コメント"}]',comment_songs='[{"name":"解析の曲"}]' WHERE id='restricted';`)
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{mux: http.NewServeMux(), tagRepo: repository.NewTagRepository(db), streamService: service.NewStreamService(repository.NewStreamRepository(db), repository.NewPerformanceRepository(db))}
	r.setupRoutes()
	return r
}

func streamTagRequest(t *testing.T, r *Router, path string, user *models.User) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if user != nil {
		req = withUser(req, user)
	}
	if !authorizeRequest(req, user) {
		t.Fatalf("公開の配信情報ルートが拒否された: %s", path)
	}
	w := httptest.NewRecorder()
	r.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

// 実際の routing → handler → repository → Scan → JSON を通す。配信情報は公開、歌唱・解析は一覧に載せない。
func TestStreamTagFilterHTTP(t *testing.T) {
	db := reviewTestDB(t)
	r := streamTagFixture(t, db)
	viewers := []*models.User{nil, {Permissions: []string{}}, {Permissions: []string{auth.PermContentEdit}}, {Permissions: []string{auth.PermRestrictedView}}, {Permissions: []string{auth.PermAll}}}
	cases := []struct {
		query  string
		ids    []string
		counts map[string]int
	}{
		{"", []string{"both", "singing_only", "3d_only", "untagged", "restricted", "allowed", "override", "multiowner", "guest"}, map[string]int{"singing": 7, "3d": 7, "members_only": 4}},
		{"tag=singing", []string{"both", "singing_only", "restricted", "allowed", "override", "multiowner", "guest"}, map[string]int{"singing": 7, "3d": 6, "members_only": 4}},
		{"tag=singing&tag=3d", []string{"both", "restricted", "allowed", "override", "multiowner", "guest"}, map[string]int{"singing": 6, "3d": 6, "members_only": 4}},
		{"tag=singing&tag=3d&tag=singing&tag=%20%203d%20&tag=", []string{"both", "restricted", "allowed", "override", "multiowner", "guest"}, map[string]int{"singing": 6, "3d": 6, "members_only": 4}},
		{"tag=members_only", []string{"restricted", "allowed", "override", "multiowner"}, map[string]int{"singing": 4, "3d": 4, "members_only": 4}},
		{"tag=unknown", []string{}, map[string]int{}},
	}
	for u, user := range viewers {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("viewer%d/%s", u, tc.query), func(t *testing.T) {
				raw := streamTagRequest(t, r, "/api/streams?sort=date&dir=asc&limit=2&"+tc.query, user)
				var first dto.StreamListResponse
				if err := json.Unmarshal(raw, &first); err != nil {
					t.Fatal(err)
				}
				if first.Pagination.Total != len(tc.ids) || first.Pagination.TotalPages != (len(tc.ids)+1)/2 {
					t.Fatalf("件数: %+v want=%d", first.Pagination, len(tc.ids))
				}
				ids := []string{}
				for page := 1; page <= first.Pagination.TotalPages; page++ {
					raw := streamTagRequest(t, r, fmt.Sprintf("/api/streams?sort=date&dir=asc&limit=2&page=%d&%s", page, tc.query), user)
					var result struct {
						Streams []map[string]json.RawMessage `json:"streams"`
					}
					if err := json.Unmarshal(raw, &result); err != nil {
						t.Fatal(err)
					}
					for _, row := range result.Streams {
						var id string
						if err := json.Unmarshal(row["id"], &id); err != nil {
							t.Fatal(err)
						}
						ids = append(ids, id)
						for _, key := range []string{"performances", "performance_count", "holodex_timeline_songs", "comment_timeline_songs", "chapter_timeline_songs", "comment_songs_analyzed_at", "has_comment_raw", "chapter_count"} {
							if _, exists := row[key]; exists {
								t.Fatalf("一覧に中身が混ざる: %s %s", id, key)
							}
						}
						_, hasOperational := row["is_processed"]
						canEdit := user != nil && auth.HasPermission(user.Permissions, auth.PermContentEdit)
						if hasOperational != canEdit {
							t.Fatalf("is_processed の権限: %s", raw)
						}
						var restricted bool
						if err := json.Unmarshal(row["is_restricted"], &restricted); err != nil {
							t.Fatal(err)
						}
						if restricted != (id == "restricted" || id == "multiowner") {
							t.Fatalf("実効秘匿: %s=%v", id, restricted)
						}
					}
				}
				if !reflect.DeepEqual(ids, tc.ids) {
					t.Fatalf("一覧=%v want=%v", ids, tc.ids)
				}
				var counts struct {
					Counts map[string]int `json:"counts"`
				}
				if err := json.Unmarshal(streamTagRequest(t, r, "/api/streams/tag-counts?"+tc.query, user), &counts); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(counts.Counts, tc.counts) {
					t.Fatalf("タグ件数=%v want=%v", counts.Counts, tc.counts)
				}
			})
		}
	}
	// 非表示の包含は repository にだけある経路。既定の HTTP 一覧には権限があっても含めない。
	for _, hidden := range []bool{false, true} {
		rows, total, err := repository.NewStreamRepository(db).FindAll(100, 0, hidden, "date", "asc", []string{"singing", "3d", "singing"})
		if err != nil {
			t.Fatal(err)
		}
		want := 6
		if hidden {
			want = 9
		}
		if len(rows) != want || total != want {
			t.Fatalf("includeHidden=%v: rows=%d total=%d want=%d", hidden, len(rows), total, want)
		}
	}
}

func TestStreamTagFilterRejectsExcessConditions(t *testing.T) {
	db := reviewTestDB(t)
	r := streamTagFixture(t, db)
	tags := []string{}
	query := ""
	for i := 0; i < 21; i++ {
		id := fmt.Sprintf("limit_%02d", i)
		tags = append(tags, id)
		query += "&tag=" + id
		if _, err := db.Exec(`INSERT INTO stream_tags(id,display_name) VALUES ($1,$1)`, id); err != nil {
			t.Fatal(err)
		}
		// 最後の条件だけは持たない。切り捨てると both が誤って返る。
		if i < 20 {
			if _, err := db.Exec(`INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES ('both',$1)`, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, path := range []string{"/api/streams?", "/api/streams/tag-counts?"} {
		w := httptest.NewRecorder()
		r.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("条件を捨てず拒否する: %s status=%d %s", path, w.Code, w.Body.String())
		}
	}
	repo := repository.NewStreamRepository(db)
	if _, _, err := repo.FindAll(20, 0, false, "", "", tags); !errors.Is(err, repository.ErrTooManyStreamTags) {
		t.Fatalf("一覧の上限: %v", err)
	}
	if _, err := repo.CountByTagForList(tags); !errors.Is(err, repository.ErrTooManyStreamTags) {
		t.Fatalf("件数の上限: %v", err)
	}
	duplicates := strings.Repeat("&tag=singing", 30)
	// 上限は種類数に適用する。重複だけなら通常の singing 絞り込みを使える。
	for _, path := range []string{"/api/streams?", "/api/streams/tag-counts?"} {
		streamTagRequest(t, r, path+duplicates, nil)
	}
}

func TestStreamTagFilterNullableMetadata(t *testing.T) {
	db := reviewTestDB(t)
	r := streamTagFixture(t, db)
	if _, err := db.Exec(`INSERT INTO stream_tags(id,display_name,color) VALUES ('no_color','色なし',NULL);
 INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES ('both','no_color');`); err != nil {
		t.Fatal(err)
	}
	var list dto.StreamListResponse
	if err := json.Unmarshal(streamTagRequest(t, r, "/api/streams?tag=no_color", nil), &list); err != nil {
		t.Fatal(err)
	}
	if list.Pagination.Total != 1 || len(list.Streams) != 1 || list.Streams[0].ID != "both" {
		t.Fatalf("NULL 色の一覧: %+v", list)
	}
	if list.Streams[0].ThumbnailURL != nil || list.Streams[0].DurationSeconds != nil {
		t.Fatalf("NULL metadata: %+v", list.Streams[0])
	}
	for _, tag := range list.Streams[0].Tags {
		if tag.ID == "no_color" && tag.Color != "" {
			t.Fatalf("NULL 色: %+v", tag)
		}
	}
	streamTagRequest(t, r, "/api/streams/tag-counts?tag=no_color", nil)
	var tags []dto.StreamTagResponse
	if err := json.Unmarshal(streamTagRequest(t, r, "/api/stream-tags", nil), &tags); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tag := range tags {
		if tag.ID == "no_color" {
			found = true
			if tag.Color != "" {
				t.Fatalf("語彙の NULL 色: %+v", tag)
			}
		}
	}
	if !found {
		t.Fatal("語彙から色なしタグが消えた")
	}
	actual, err := repository.NewStreamRepository(db).GetTags("both")
	if err != nil || len(actual) != 3 {
		t.Fatalf("単件の NULL 色: %+v %v", actual, err)
	}
}
