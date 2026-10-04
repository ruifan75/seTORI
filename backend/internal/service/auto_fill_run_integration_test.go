package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/repository"
)

type collabTransport func(*http.Request) (*http.Response, error)

func (f collabTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// 実行中に設定を反転しても、取り直しとワーカーが開始時の範囲を使うこと。
// 素材がある客串の全行が、歌手未指定の審査へ保存されるところまで確認する。
func TestAutoFillRunUsesOneCollabSnapshot(t *testing.T) {
	for _, collabs := range []bool{false, true} {
		t.Run(fmt.Sprint(collabs), func(t *testing.T) {
			db := reviewTestDB(t)
			if _, err := db.Exec(`INSERT INTO singers (id, name, auto_fill_enabled) VALUES ('UCtarget', 'target', true), ('UCowner', 'owner', false)`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO streams (id, title, stream_date) VALUES ('guest123456', 'guest', NOW() - INTERVAL '1 day')`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO stream_singers (stream_id, singer_id, is_owner) VALUES ('guest123456', 'UCtarget', false), ('guest123456', 'UCowner', true)`); err != nil {
				t.Fatal(err)
			}
			settings := repository.NewAppSettingsRepository(db)
			streamRepo := repository.NewStreamRepository(db)
			singerRepo := repository.NewSingerRepository(db)
			h := NewHolodexService("test", "", "", streamRepo, singerRepo, "")
			comments := &CommentService{streamRepo: streamRepo, holodexService: h}
			songRepo := repository.NewSongRepository(db)
			itunesRepo := repository.NewSongItunesRepository(db)
			match := NewSongMatchService(repository.NewSongMatchRepository(db), songRepo, itunesRepo, repository.NewAliasRepository(db))
			norm := NewNormalizationService(nil, itunesRepo, match)
			perfRepo := repository.NewPerformanceRepository(db)
			perf := NewPerformanceService(perfRepo, songRepo, itunesRepo, repository.NewArtistRepository(db), streamRepo, match)
			suggestions := NewSuggestionService(repository.NewSuggestionRepository(db), settings, nil, nil, perf, match)
			h.SetAnalysisServices(norm, nil)
			batch := NewBatchFillService(streamRepo, perfRepo, repository.NewBatchFillRepository(db), comments, h, nil, norm, perf, suggestions)
			// 照合・明示 end・原曲歌手が揃った 2 曲。客串でなければ自動作成できる入力にする。
			songIDs := []uuid.UUID{uuid.New(), uuid.New()}
			for i, id := range songIDs {
				if _, err := db.Exec(`INSERT INTO songs(id,name,original_artist) VALUES($1,$2,'artist')`, id, fmt.Sprintf("song%d", i)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := match.RebuildKeys(); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE streams SET holodex_hash='fixture', holodex_data='{"songs":[{"name":"song0","original_artist":"artist","start":10,"end":100},{"name":"song1","original_artist":"artist","start":200,"end":300}]}', holodex_songs_hash='fixture', holodex_songs_normalized='[{"name":"song0","original_artist":"artist","start_seconds":10,"end_seconds":100},{"name":"song1","original_artist":"artist","start_seconds":200,"end_seconds":300}]' WHERE id='guest123456'`); err != nil {
				t.Fatal(err)
			}
			svc := NewAutoFillService(settings, singerRepo, streamRepo, h, comments, batch)
			if _, err := svc.UpdateSettings(false, 6, 30, collabs); err != nil {
				t.Fatal(err)
			}
			oldTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = oldTransport })
			commentsFetched := 0
			http.DefaultTransport = collabTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "holodex.net" {
					return nil, fmt.Errorf("unexpected URL: %s", r.URL)
				}
				var body string
				switch r.URL.Path {
				case "/api/v2/channels/UCtarget":
					// 最初の設定取得後、同期中に利用者がチェックを切り替えた状況。
					if _, err := svc.UpdateSettings(false, 6, 1, !collabs); err != nil {
						return nil, err
					}
					body = `{"id":"UCtarget","name":"target"}`
				case "/api/v2/videos":
					body = `[]`
				case "/api/v2/videos/guest123456":
					commentsFetched++
					body = `{"id":"guest123456","comments":[]}`
				default:
					return nil, fmt.Errorf("unexpected URL: %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			res, err := svc.RunOnce()
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for batch.Status().Running && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			st := batch.Status()
			if st.Running {
				t.Fatal("一括作成が終了しない")
			}
			want := 0
			if collabs {
				want = 1
			}
			if res.Failures != 0 || res.Refreshed != want || commentsFetched != want || res.FillRunID == "" || st.IncludeCollabs != collabs {
				t.Fatalf("開始時の範囲が伝わっていない: res=%+v status=%+v fetched=%d", res, st, commentsFetched)
			}
			// Status の旗だけではワーカーへの引き渡しを守れない。実際に積まれた提案と歌唱を見る。
			if st.Total != want || st.Done != want || st.Created != 0 || st.Review != 2*want {
				t.Fatalf("客串の作成結果: %+v", st)
			}
			var performances int
			if err := db.QueryRow(`SELECT COUNT(*) FROM performances WHERE stream_id='guest123456'`).Scan(&performances); err != nil {
				t.Fatal(err)
			}
			if performances != 0 {
				t.Fatalf("客串に歌唱を自動作成した: %d", performances)
			}
			rows, err := db.Query(`SELECT payload FROM edit_suggestions ORDER BY (payload->>'start_seconds')::int`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			count := 0
			for rows.Next() {
				var raw []byte
				if err := rows.Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var payload struct {
					StreamID      string   `json:"stream_id"`
					SongID        string   `json:"song_id"`
					SingerIDs     []string `json:"singer_ids"`
					ReviewReasons []string `json:"review_reasons"`
					Start         int      `json:"start_seconds"`
				}
				if err := json.Unmarshal(raw, &payload); err != nil {
					t.Fatal(err)
				}
				if count >= 2 || payload.StreamID != "guest123456" || payload.SongID != songIDs[count].String() || len(payload.SingerIDs) != 0 || len(payload.ReviewReasons) != 1 || payload.ReviewReasons[0] != "multi_singer" || payload.Start != []int{10, 200}[count] {
					t.Fatalf("審査の内容: %s", raw)
				}
				count++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if count != 2*want {
				t.Fatalf("審査件数: %d want %d", count, 2*want)
			}
			if svc.GetSettings().IncludeCollabs == collabs {
				t.Fatal("実行途中の設定変更が発生していない")
			}
			if !batch.hasMultipleSingers("guest123456") {
				t.Fatal("客串の参加者が複数と認識されていない")
			}
		})
	}
}

// SQL の参加者・所有者を取り違えず、無関係・非表示・会限を対象へ混ぜないこと。
func TestAutoFillCollabTargetRows(t *testing.T) {
	db := reviewTestDB(t)
	if _, err := db.Exec(`INSERT INTO singers (id,name) VALUES ('UCtarget','target'),('UCother','other')`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, singer            string
		owner, hidden, member bool
	}{
		{"owned", "UCtarget", true, false, false}, {"guest", "UCtarget", false, false, false},
		{"unrelated", "UCother", true, false, false}, {"hidden", "UCtarget", false, true, false},
		{"member", "UCtarget", false, false, true},
	} {
		if _, err := db.Exec(`INSERT INTO streams(id,title,stream_date,is_hidden,comment_raw) VALUES ($1,$1,NOW()-INTERVAL '1 day',$2,'["test"]')`, tc.id, tc.hidden); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO stream_singers(stream_id,singer_id,is_owner) VALUES($1,$2,$3)`, tc.id, tc.singer, tc.owner); err != nil {
			t.Fatal(err)
		}
		if tc.member {
			if _, err := db.Exec(`INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES($1,'members_only')`, tc.id); err != nil {
				t.Fatal(err)
			}
		}
	}
	repo := repository.NewStreamRepository(db)
	for _, collabs := range []bool{false, true} {
		ids, err := repo.FindStreamsNeedingCommentRefresh([]string{"UCtarget"}, 30, nil, collabs)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]bool{"owned": true}
		if collabs {
			want["guest"] = true
		}
		got := map[string]bool{}
		for _, id := range ids {
			got[id] = true
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("collabs=%v: refresh=%v want=%v", collabs, got, want)
		}
		streams, err := repo.FindStreamsForFill(BatchFillModeUnprocessed, []string{"UCtarget"}, collabs)
		if err != nil {
			t.Fatal(err)
		}
		// 一括作成は取り込み済み素材なら会限も扱える（取り直しとは異なる）。
		if collabs {
			want["member"] = true
		}
		got = map[string]bool{}
		for _, stream := range streams {
			got[stream.ID] = true
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("collabs=%v: fill=%v want=%v", collabs, got, want)
		}
	}
}
