package repository

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// 参加チャンネル・歌った人・配信を別々にした fixture で、各読み取りを実行する。
// WHERE の字面だけでは通っていた JOIN の関連先の取り違えも、件数と ID で検出する。
func TestListedDiscoverySurfacesPostgres(t *testing.T) {
	db := reviewTestDB(t)
	a, b, empty, artist := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO singers(id,name,is_hidden) VALUES ('visible','表示',FALSE),('hidden','非表示',TRUE);
 INSERT INTO streams(id,title,stream_date,is_hidden,restriction_override) VALUES
 ('visible','表示のみ','2026-01-01',FALSE,NULL),('hidden','非表示のみ','2026-01-02',FALSE,NULL),
 ('mixed','非表示の所有者と表示のゲスト','2026-01-03',FALSE,NULL),('orphan','参加者なし','2026-01-04',FALSE,NULL),
 ('restricted','秘匿','2026-01-05',FALSE,TRUE),('hidden-stream','非表示配信','2026-01-06',TRUE,NULL);
 INSERT INTO stream_singers(stream_id,singer_id,is_owner) VALUES
 ('visible','visible',TRUE),('hidden','hidden',TRUE),('mixed','hidden',TRUE),('mixed','visible',FALSE),
 ('restricted','visible',TRUE),('hidden-stream','visible',TRUE);`)
	for i, id := range []uuid.UUID{a, b, empty} {
		mustExec(`INSERT INTO songs(id,name,original_artist) VALUES($1,$2,'原曲')`, id, fmt.Sprintf("曲%d", i))
	}
	mustExec(`INSERT INTO artists(id,name) VALUES($1,'原曲')`, artist)
	for _, id := range []uuid.UUID{a, b, empty} {
		mustExec(`INSERT INTO song_artists(song_id,artist_id) VALUES($1,$2)`, id, artist)
	}
	var allA, publicA, allTags, publicTags []uuid.UUID
	add := func(song uuid.UUID, stream string, start int, public bool) {
		id := uuid.New()
		mustExec(`INSERT INTO performances(id,song_id,stream_id,start_seconds,end_seconds,order_index) VALUES($1,$2,$3,$4,$5,$6)`, id, song, stream, start, start+20, start)
		// 歌った人を stream_singers と同一にはしない。発見面は参加チャンネルで決める。
		mustExec(`INSERT INTO performance_singers(performance_id,singer_id) VALUES($1,'hidden')`, id)
		mustExec(`INSERT INTO performance_performance_tags(performance_id,tag_id) VALUES($1,'acoustic')`, id)
		if song == a {
			allA = append(allA, id)
			if public {
				publicA = append(publicA, id)
			}
		}
		allTags = append(allTags, id)
		if public {
			publicTags = append(publicTags, id)
		}
	}
	for i, stream := range []string{"visible", "visible", "mixed", "hidden", "orphan", "restricted", "hidden-stream"} {
		add(a, stream, 100+i*30, i < 3)
	}
	for i := 0; i < 4; i++ {
		add(b, "visible", 500+i*30, true)
	}
	mustExec(`INSERT INTO performance_performance_tags(performance_id,tag_id) VALUES($1,'piano')`, allA[3])
	perfs, songs, tags, artists := NewPerformanceRepository(db), NewSongRepository(db), NewTagRepository(db), NewArtistRepository(db)
	for _, tc := range []struct {
		name          string
		access        ViewerAccess
		aIDs, tagIDs  []uuid.UUID
		counts        map[uuid.UUID]int
		order         []uuid.UUID
		piano, singer int
	}{
		{"public", PublicAccess, publicA, publicTags, map[uuid.UUID]int{a: 3, b: 4, empty: 0}, []uuid.UUID{b, a, empty}, 0, 9},
		{"restricted-view", RestrictedView, allA, allTags, map[uuid.UUID]int{a: 7, b: 4, empty: 0}, []uuid.UUID{a, b, empty}, 1, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, total, err := perfs.FindBySongID(a, 100, 0, tc.access)
			if err != nil {
				t.Fatal(err)
			}
			assertListedPerformanceIDs(t, rows, total, tc.aIDs)
			rows, total, err = perfs.FindByTagID("acoustic", 100, 0, tc.access)
			if err != nil {
				t.Fatal(err)
			}
			assertListedPerformanceIDs(t, rows, total, tc.tagIDs)
			for song, want := range tc.counts {
				got, err := songs.GetPerformanceCount(song, tc.access)
				if err != nil || got != want {
					t.Fatalf("song %s count=%d want=%d err=%v", song, got, want, err)
				}
			}
			counts, err := songs.GetPerformanceCounts([]uuid.UUID{a, b, empty}, tc.access)
			if err != nil {
				t.Fatal(err)
			}
			for song, want := range tc.counts {
				if counts[song] != want {
					t.Fatalf("一括 song %s count=%d want=%d", song, counts[song], want)
				}
			}
			for _, search := range []string{"", "曲"} {
				for _, dir := range []string{"desc", "asc"} {
					found, n, err := songs.FindAll(10, 0, search, "performances", dir, tc.access)
					if err != nil || n != 3 {
						t.Fatalf("人気の楽曲 n=%d err=%v", n, err)
					}
					var ids []uuid.UUID
					for _, song := range found {
						ids = append(ids, song.ID)
					}
					want := append([]uuid.UUID(nil), tc.order...)
					if dir == "asc" {
						want[0], want[2] = want[2], want[0]
					}
					if !reflect.DeepEqual(ids, want) {
						t.Fatalf("人気の楽曲 search=%q dir=%s IDs=%v want=%v", search, dir, ids, want)
					}
				}
			}
			for _, tag := range []struct {
				id    string
				count int
			}{{"acoustic", len(tc.tagIDs)}, {"piano", tc.piano}} {
				found, err := tags.SearchPerformanceTags(tag.id, 10, tc.access)
				if err != nil || len(found) != 1 || found[0].ID != tag.id || found[0].Count != tag.count {
					t.Fatalf("タグ %s found=%+v err=%v want count=%d", tag.id, found, err, tag.count)
				}
			}
			found, artistCounts, n, err := artists.FindSongsByArtist(artist, 10, 0, "", "", tc.access)
			if err != nil || n != 3 || len(found) != 3 || !reflect.DeepEqual(artistCounts, tc.counts) {
				t.Fatalf("アーティスト songs=%d n=%d counts=%v want=%v err=%v", len(found), n, artistCounts, tc.counts, err)
			}
			// チャンネルページには参加チャンネルの表示判定を足さない。
			singerCount, err := NewSingerRepository(db).GetPerformanceCount("hidden", tc.access)
			if err != nil || singerCount != tc.singer {
				t.Fatalf("チャンネル count=%d want=%d err=%v", singerCount, tc.singer, err)
			}
		})
	}
	// 表示に戻した場合も各集計・一覧が実データを読み直す。
	mustExec(`UPDATE singers SET is_hidden=FALSE WHERE id='hidden'`)
	got, err := songs.GetPerformanceCount(a, PublicAccess)
	if err != nil || got != 4 {
		t.Fatalf("表示へ戻した後 count=%d err=%v", got, err)
	}
	rows, total, err := perfs.FindBySongID(a, 100, 0, PublicAccess)
	if err != nil {
		t.Fatal(err)
	}
	assertListedPerformanceIDs(t, rows, total, allA[:4])
}

func assertListedPerformanceIDs(t *testing.T, rows []PerformanceWithDetails, total int, want []uuid.UUID) {
	t.Helper()
	if total != len(want) || len(rows) != len(want) {
		t.Fatalf("total=%d rows=%d want=%d", total, len(rows), len(want))
	}
	counts := make(map[uuid.UUID]int)
	for _, row := range rows {
		counts[row.ID]++
	}
	for _, id := range want {
		if counts[id] != 1 {
			t.Fatalf("歌唱 %s の出現回数=%d want=1", id, counts[id])
		}
	}
}
