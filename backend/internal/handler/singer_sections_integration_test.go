package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/auth"
)

func singerSectionsFixture(t *testing.T, db *sql.DB) *Router {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO organizations(key,display_name,sort_order) VALUES ('review_a','あ事務所',0),('review_b','い事務所',10)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO singers(id,name,english_name,organization,organization_override,is_hidden,auto_fill_enabled) VALUES
 ('visible_a','あ','Zulu','review_b',NULL,false,true),
 ('visible_i','い','Alpha','review_a',NULL,false,false),
 ('hidden_a','あ','Zulu','review_b',NULL,true,true),
 ('hidden_u','う','Bravo',NULL,NULL,true,false),
 ('hidden_e','え','Alpha','review_b','review_a',true,false)`); err != nil {
		t.Fatal(err)
	}
	return &Router{singerService: service.NewSingerService(repository.NewSingerRepository(db), repository.NewStreamRepository(db), nil)}
}
func sectionJSON(t *testing.T, r *Router, path string, user *models.User) map[string]json.RawMessage {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if user != nil {
		req = withUser(req, user)
	}
	w := httptest.NewRecorder()
	r.handleListSingers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status=%d %s", path, w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
func sectionIDs(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// セクションと件数だけでなく、画面がカードの分類・切替に使う旗まで読む。
func sectionHiddenFlags(t *testing.T, raw json.RawMessage, want map[string]bool) {
	t.Helper()
	var rows []struct {
		ID       string `json:"id"`
		IsHidden *bool  `json:"is_hidden"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(want) {
		t.Fatalf("行数: %d want %d", len(rows), len(want))
	}
	for _, row := range rows {
		hidden, ok := want[row.ID]
		if !ok || row.IsHidden == nil || *row.IsHidden != hidden {
			t.Fatalf("表示旗: id=%s is_hidden=%v want=%v", row.ID, row.IsHidden, hidden)
		}
	}
}

// handler の権限判定、実際の SELECT/Scan、JSON の省略までを確認する。
func TestSingerSectionsRespectViewerAndEditor(t *testing.T) {
	db := reviewTestDB(t)
	r := singerSectionsFixture(t, db)
	editor := &models.User{Permissions: []string{auth.PermContentEdit}}
	for _, user := range []*models.User{nil, {Permissions: []string{}}, editor} {
		canEdit := user == editor
		grouped := sectionJSON(t, r, "/api/singers?group=organization&include_hidden=true", user)
		var groups []struct {
			Organization string          `json:"organization"`
			Singers      json.RawMessage `json:"singers"`
		}
		if err := json.Unmarshal(grouped["groups"], &groups); err != nil {
			t.Fatal(err)
		}
		if len(groups) != 2 || groups[0].Organization != "review_a" || groups[1].Organization != "review_b" || !reflect.DeepEqual(sectionIDs(t, groups[0].Singers), []string{"visible_i"}) || !reflect.DeepEqual(sectionIDs(t, groups[1].Singers), []string{"visible_a"}) {
			t.Fatalf("表示中の事務所区分: %s", grouped["groups"])
		}
		sectionHiddenFlags(t, groups[0].Singers, map[string]bool{"visible_i": false})
		sectionHiddenFlags(t, groups[1].Singers, map[string]bool{"visible_a": false})
		total := 2
		if canEdit {
			total = 5
		}
		var actualTotal int
		json.Unmarshal(grouped["total"], &actualTotal)
		if actualTotal != total {
			t.Fatalf("総数=%d want=%d", actualTotal, total)
		}
		if canEdit {
			sectionHiddenFlags(t, grouped["hidden"], map[string]bool{"hidden_a": true, "hidden_u": true, "hidden_e": true})
			if !reflect.DeepEqual(sectionIDs(t, grouped["hidden"]), []string{"hidden_a", "hidden_u", "hidden_e"}) {
				t.Fatalf("非表示の名前順: %s", grouped["hidden"])
			}
		} else if _, exists := grouped["hidden"]; exists {
			t.Fatal("閲覧者へ hidden 欄が届いた")
		}
		list := sectionJSON(t, r, "/api/singers?limit=20&include_hidden=true", user)
		want := []string{"visible_a", "visible_i"}
		if canEdit {
			want = append(want, "hidden_a", "hidden_u", "hidden_e")
		}
		if !reflect.DeepEqual(sectionIDs(t, list["singers"]), want) {
			t.Fatalf("権限別の一覧: %s", list["singers"])
		}
		flags := map[string]bool{"visible_a": false, "visible_i": false}
		if canEdit {
			flags["hidden_a"], flags["hidden_u"], flags["hidden_e"] = true, true, true
		}
		sectionHiddenFlags(t, list["singers"], flags)
		if canEdit {
			var n int
			if err := json.Unmarshal(list["hidden_total"], &n); err != nil || n != 3 {
				t.Fatalf("非表示の件数: %s %v", list["hidden_total"], err)
			}
		} else if _, exists := list["hidden_total"]; exists {
			t.Fatal("閲覧者へ非表示件数が届いた")
		}
		// 運用情報を持つ fixture の欄も閲覧者には届かない。
		var singers []map[string]json.RawMessage
		json.Unmarshal(list["singers"], &singers)
		for _, singer := range singers {
			_, exists := singer["auto_fill_enabled"]
			if exists != canEdit {
				t.Fatalf("権限別の運用情報: %s", list["singers"])
			}
		}
	}
}

func TestSingerSectionsKeepOrderAcrossPages(t *testing.T) {
	db := reviewTestDB(t)
	r := singerSectionsFixture(t, db)
	editor := &models.User{Permissions: []string{auth.PermContentEdit}}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"sort=name&dir=asc", []string{"visible_a", "visible_i", "hidden_a", "hidden_u", "hidden_e"}},
		{"sort=name&dir=desc", []string{"visible_i", "visible_a", "hidden_e", "hidden_u", "hidden_a"}},
		{"sort=organization&dir=asc", []string{"visible_i", "visible_a", "hidden_e", "hidden_a", "hidden_u"}},
		{"sort=organization&dir=desc", []string{"visible_a", "visible_i", "hidden_a", "hidden_e", "hidden_u"}},
	} {
		// limit=2 のページを連結して、可視性の区がページをまたいでも崩れないことを見る。
		got := []string{}
		for _, page := range []string{"1", "2", "3"} {
			raw := sectionJSON(t, r, "/api/singers?include_hidden=true&limit=2&page="+page+"&"+tc.query, editor)
			got = append(got, sectionIDs(t, raw["singers"])...)
			var pagination struct {
				Total int `json:"total"`
				Pages int `json:"total_pages"`
			}
			json.Unmarshal(raw["pagination"], &pagination)
			var hidden int
			json.Unmarshal(raw["hidden_total"], &hidden)
			if pagination.Total != 5 || pagination.Pages != 3 || hidden != 3 {
				t.Fatalf("ページングの件数: %+v hidden=%d", pagination, hidden)
			}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got=%v want=%v", tc.query, got, tc.want)
		}
	}
}

func TestSingerSectionsReturnEmptyHiddenToEditor(t *testing.T) {
	db := reviewTestDB(t)
	r := singerSectionsFixture(t, db)
	// 全行はこのテストの fixture。既存の利用者データには触れない。
	if _, err := db.Exec(`UPDATE singers SET is_hidden=false`); err != nil {
		t.Fatal(err)
	}
	editor := &models.User{Permissions: []string{auth.PermContentEdit}}
	raw := sectionJSON(t, r, "/api/singers?group=organization&include_hidden=true", editor)
	if string(raw["hidden"]) != "[]" {
		t.Fatalf("非表示 0 件が省略され、画面の件数が消える: hidden=%s", raw["hidden"])
	}
	viewer := sectionJSON(t, r, "/api/singers?group=organization&include_hidden=true", nil)
	if _, exists := viewer["hidden"]; exists {
		t.Fatal("閲覧者へ hidden 欄が届いた")
	}
}
