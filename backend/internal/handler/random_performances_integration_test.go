package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/internal/service"
	"github.com/ruifan75/setori/pkg/auth"
)

// 同じコラボ配信の中でも、表示中チャンネルが実際に歌った曲だけをおすすめに出す。
// 実 Postgres と HTTP handler を通し、空の結果だけでなく残る曲・合唱も確認する。
func TestRandomPerformancesRequireVisibleSinger(t *testing.T) {
	db := reviewTestDB(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO channels(id,name,is_hidden) VALUES
		('visible','表示中',FALSE),('hidden','非表示',TRUE),('hidden-2','非表示2',TRUE);
		INSERT INTO streams(id,title,stream_date,is_hidden) VALUES
		('collab','コラボ','2026-01-01',FALSE),('unlisted','非表示チャンネルのみ','2026-01-01',FALSE);
		INSERT INTO stream_channels(stream_id,channel_id,is_owner) VALUES
		('collab','hidden',TRUE),('collab','visible',FALSE),('unlisted','hidden',TRUE)`)
	for _, f := range []struct {
		name    string
		stream  string
		singers []string
	}{
		{"visible-solo", "collab", []string{"visible"}},
		{"mixed-duet", "collab", []string{"visible", "hidden"}},
		{"hidden-solo", "collab", []string{"hidden"}},
		{"hidden-duet", "collab", []string{"hidden", "hidden-2"}},
		{"no-singers", "collab", nil},
		{"unlisted-stream", "unlisted", []string{"visible"}},
	} {
		songID, perfID := uuid.New(), uuid.New()
		exec(`INSERT INTO songs(id,name,original_artist) VALUES($1,$2,'原曲')`, songID, f.name)
		exec(`INSERT INTO performances(id,stream_id,song_id,start_seconds,end_seconds,order_index)
			VALUES($1,$2,$3,10,100,1)`, perfID, f.stream, songID)
		for _, singer := range f.singers {
			exec(`INSERT INTO performance_singers(performance_id,singer_id) VALUES($1,$2)`, perfID, singer)
		}
	}
	r := &Router{streamService: service.NewStreamService(repository.NewStreamRepository(db), repository.NewPerformanceRepository(db))}
	get := func(t *testing.T, user *models.User, query string) []dto.PerformanceResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/performances/random?"+query, nil)
		if user != nil {
			req = withUser(req, user)
		}
		w := httptest.NewRecorder()
		r.handleRandomPerformances(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var result dto.PerformanceListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Performances
	}
	for _, viewer := range []struct {
		name string
		user *models.User
	}{
		{"anonymous", nil},
		{"editor", &models.User{Permissions: []string{auth.PermContentEdit}}},
		{"restricted-view", &models.User{Permissions: []string{auth.PermRestrictedView}}},
	} {
		t.Run(viewer.name, func(t *testing.T) {
			perfs := get(t, viewer.user, "limit=100")
			names, songIDs := []string{}, []string{}
			for _, perf := range perfs {
				names = append(names, perf.SongName)
				songIDs = append(songIDs, perf.SongID.String())
			}
			slices.Sort(names)
			if want := []string{"mixed-duet", "visible-solo"}; !reflect.DeepEqual(names, want) {
				t.Fatalf("recommended songs=%v want=%v", names, want)
			}
			if got := get(t, viewer.user, "limit=1"); len(got) != 1 || !slices.Contains(names, got[0].SongName) {
				t.Fatalf("limit=1: %+v", got)
			}
			if got := get(t, viewer.user, "limit=100&exclude_song_ids="+strings.Join(songIDs, ",")); len(got) != 0 {
				t.Fatalf("excluded songs returned: %+v", got)
			}
		})
	}
	// 配信に別の表示中参加者が残っていても、歌った人の全員を隠すと対象外。
	exec(`INSERT INTO channels(id,name,is_hidden) VALUES('participant','参加のみ',FALSE);
		INSERT INTO stream_channels(stream_id,channel_id) VALUES('collab','participant');
		UPDATE channels SET is_hidden=TRUE WHERE id='visible'`)
	if got := get(t, nil, "limit=100"); len(got) != 0 {
		t.Fatalf("hidden singers still recommended: %+v", got)
	}
	exec(`UPDATE channels SET is_hidden=FALSE WHERE id='visible'`)
	if got := get(t, nil, "limit=100"); len(got) != 2 {
		t.Fatalf("restored visible singer: got %d songs, want 2", len(got))
	}
}
