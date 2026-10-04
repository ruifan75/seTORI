package repository

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"

	"github.com/ruifan75/setori/internal/models"
)

type streamSearchFixture struct {
	id         string
	all        int // 全IDのANDを満たす条件: 参加者=1, vocalist=2, 配信タグ=4, 歌唱タグ=8
	restricted bool
	members    bool
	override   any
	guestOwner bool
	noOwner    bool
}

// 独立 schema に実データを作り、修正前の相関SQLと変更後の実repositoryを比較する。
// 曲ごとのタグ・歌った人を別の歌唱へ分散し、繰り返しも置く。COUNT(*)への置換、
// ORへの緩和、参加者と歌った人の混同、秘匿・非表示条件の誤変更を結果で検出する。
func TestStreamSearchMatchesLegacyPostgres(t *testing.T) {
	db := reviewTestDB(t)
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO channels(id,name,is_hidden,members_only_policy) VALUES
 ('owner','所有者',TRUE,'allow'),('guest','参加者',TRUE,NULL),
 ('voice-a','歌った人A',FALSE,NULL),('voice-b','歌った人B',FALSE,NULL);
 INSERT INTO songs(id,name,original_artist) VALUES ('00000000-0000-0000-0000-000000000001','検証曲','原曲');`)
	fixtures := []streamSearchFixture{
		{id: "all", all: 15},
		{id: "hidden-all", all: 15},
		{id: "missing-participant", all: 14},
		{id: "missing-vocalist", all: 13},
		{id: "missing-stream-tag", all: 11},
		{id: "missing-performance-tag", all: 7},
		{id: "secret-override", all: 15, restricted: true, override: true},
		{id: "secret-member", all: 15, restricted: true, members: true, guestOwner: true},
		{id: "allowed-policy", all: 15, members: true},
		{id: "allowed-override", all: 15, members: true, override: false, guestOwner: true},
		{id: "ownerless-member", all: 15, restricted: true, members: true, noOwner: true},
		{id: "no-performances", all: 5},
		{id: "nothing", all: 0},
	}
	for i, f := range fixtures {
		mustExec(`INSERT INTO streams(id,title,stream_date,is_hidden,restriction_override) VALUES($1,$2,'2026-01-01'::date+$3::int,$4,$5)`, f.id, "集計 "+f.id, i, f.id == "hidden-all", f.override)
		if f.all == 0 {
			continue
		}
		mustExec(`INSERT INTO stream_channels(stream_id,channel_id,is_owner) VALUES($1,'owner',$2)`, f.id, !f.noOwner)
		if f.all&1 != 0 {
			mustExec(`INSERT INTO stream_channels(stream_id,channel_id,is_owner) VALUES($1,'guest',$2)`, f.id, f.guestOwner && !f.noOwner)
		}
		mustExec(`INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES($1,'singing')`, f.id)
		if f.all&4 != 0 {
			mustExec(`INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES($1,'3d')`, f.id)
		}
		if f.members {
			mustExec(`INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES($1,'members_only')`, f.id)
		}
		if f.all&10 == 0 {
			continue
		}
		for j := 0; j < 3; j++ {
			id := fmt.Sprintf("00000000-0000-0000-%04d-%012d", i, j)
			mustExec(`INSERT INTO performances(id,stream_id,song_id,start_seconds,end_seconds,order_index) VALUES($1,$2,'00000000-0000-0000-0000-000000000001',$3,$4,$5)`, id, f.id, 100+j*100, 150+j*100, j)
			voice, tag := "voice-a", "acoustic"
			if j == 1 && f.all&2 != 0 {
				voice = "voice-b"
			}
			if j == 1 && f.all&8 != 0 {
				tag = "piano"
			}
			mustExec(`INSERT INTO performance_singers(performance_id,singer_id) VALUES($1,$2)`, id, voice)
			mustExec(`INSERT INTO performance_performance_tags(performance_id,tag_id) VALUES($1,$2)`, id, tag)
		}
	}

	for mask := 0; mask < 16; mask++ {
		f := models.StreamSearchFilters{}
		if mask&1 != 0 {
			f.ParticipantIDs = []string{"owner", "guest"}
		}
		if mask&2 != 0 {
			f.VocalistIDs = []string{"voice-a", "voice-b"}
		}
		if mask&4 != 0 {
			f.StreamTagIDs = []string{"singing", "3d"}
		}
		if mask&8 != 0 {
			f.PerformanceTagIDs = []string{"acoustic", "piano"}
		}
		for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
			t.Run(fmt.Sprintf("mask=%02d/access=%d", mask, access), func(t *testing.T) {
				var want []string
				// SQLと独立したfixtureの期待値も見る。秘匿タイトルは条件なしでは公開。
				for i := len(fixtures) - 1; i >= 0; i-- {
					x := fixtures[i]
					if x.all&mask == mask && (mask&10 == 0 || access == RestrictedView || !x.restricted) {
						want = append(want, x.id)
					}
				}
				assertStreamSearchLegacyResults(t, db, f, access, want)
			})
		}
	}
	for _, tc := range []struct {
		name        string
		filters     models.StreamSearchFilters
		performance bool
		match       func(streamSearchFixture) bool
	}{
		{"one-participant", models.StreamSearchFilters{ParticipantIDs: []string{"owner"}}, false, func(x streamSearchFixture) bool { return x.id != "nothing" }},
		{"one-vocalist", models.StreamSearchFilters{VocalistIDs: []string{"voice-a"}}, true, func(x streamSearchFixture) bool { return x.id != "nothing" && x.id != "no-performances" }},
		{"one-stream-tag", models.StreamSearchFilters{StreamTagIDs: []string{"singing"}}, false, func(x streamSearchFixture) bool { return x.id != "nothing" }},
		{"one-performance-tag", models.StreamSearchFilters{PerformanceTagIDs: []string{"acoustic"}}, true, func(x streamSearchFixture) bool { return x.id != "nothing" && x.id != "no-performances" }},
		{"query-owner", models.StreamSearchFilters{Query: "集計", OwnerID: "owner"}, false, func(x streamSearchFixture) bool { return x.id != "nothing" && !x.noOwner }},
		{"all-with-query-owner", models.StreamSearchFilters{Query: "集計", OwnerID: "owner", ParticipantIDs: []string{"owner", "guest"}, VocalistIDs: []string{"voice-a", "voice-b"}, StreamTagIDs: []string{"singing", "3d"}, PerformanceTagIDs: []string{"acoustic", "piano"}}, true, func(x streamSearchFixture) bool { return x.all == 15 && !x.noOwner }},
		{"all-no-results", models.StreamSearchFilters{ParticipantIDs: []string{"owner", "guest"}, VocalistIDs: []string{"voice-a", "voice-b"}, StreamTagIDs: []string{"singing", "3d"}, PerformanceTagIDs: []string{"acoustic", "absent"}}, true, func(streamSearchFixture) bool { return false }},
		{"duplicate-participant-direct", models.StreamSearchFilters{ParticipantIDs: []string{"owner", "owner"}}, false, func(streamSearchFixture) bool { return false }},
		{"duplicate-vocalist-direct", models.StreamSearchFilters{VocalistIDs: []string{"voice-a", "voice-a"}}, true, func(streamSearchFixture) bool { return false }},
		{"duplicate-stream-tag-direct", models.StreamSearchFilters{StreamTagIDs: []string{"singing", "singing"}}, false, func(streamSearchFixture) bool { return false }},
		{"duplicate-performance-tag-direct", models.StreamSearchFilters{PerformanceTagIDs: []string{"acoustic", "acoustic"}}, true, func(streamSearchFixture) bool { return false }},
		{"absent-participant", models.StreamSearchFilters{ParticipantIDs: []string{"owner", "absent"}}, false, func(streamSearchFixture) bool { return false }},
		{"absent-vocalist", models.StreamSearchFilters{VocalistIDs: []string{"voice-a", "absent"}}, true, func(streamSearchFixture) bool { return false }},
		{"absent-stream-tag", models.StreamSearchFilters{StreamTagIDs: []string{"singing", "absent"}}, false, func(streamSearchFixture) bool { return false }},
		{"absent-performance-tag", models.StreamSearchFilters{PerformanceTagIDs: []string{"acoustic", "absent"}}, true, func(streamSearchFixture) bool { return false }},
		{"absent-query", models.StreamSearchFilters{Query: "不存在"}, false, func(streamSearchFixture) bool { return false }},
		{"vocalist-is-not-owner", models.StreamSearchFilters{OwnerID: "voice-a"}, false, func(streamSearchFixture) bool { return false }},
	} {
		for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
			t.Run(fmt.Sprintf("%s/access=%d", tc.name, access), func(t *testing.T) {
				var want []string
				for i := len(fixtures) - 1; i >= 0; i-- {
					x := fixtures[i]
					if tc.match(x) && (!tc.performance || access == RestrictedView || !x.restricted) {
						want = append(want, x.id)
					}
				}
				assertStreamSearchLegacyResults(t, db, tc.filters, access, want)
			})
		}
	}
}

func assertStreamSearchLegacyResults(t *testing.T, db *sql.DB, f models.StreamSearchFilters, access ViewerAccess, want []string) {
	t.Helper()
	where, args := legacyStreamSearchWhere(f, access)
	var oldTotal int
	if err := db.QueryRow("SELECT COUNT(*) FROM streams s "+where, args...).Scan(&oldTotal); err != nil {
		t.Fatal(err)
	}
	if oldTotal != len(want) {
		t.Fatalf("legacy count=%d want=%d", oldTotal, len(want))
	}
	for _, page := range []struct{ limit, offset int }{{100, 0}, {2, 1}, {2, 1000}} {
		oldArgs := append(append([]any(nil), args...), page.limit, page.offset)
		rows, err := db.Query("SELECT "+streamSearchWantColumns+" FROM streams s "+where+fmt.Sprintf(" ORDER BY s.stream_date DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2), oldArgs...)
		if err != nil {
			t.Fatal(err)
		}
		var old []models.Stream
		for rows.Next() {
			x, err := scanStreamRow(rows)
			if err != nil {
				rows.Close()
				t.Fatal(err)
			}
			old = append(old, x)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		current, total, err := NewStreamRepository(db).SearchStreams(f, page.limit, page.offset, access)
		if err != nil {
			t.Fatal(err)
		}
		if total != oldTotal || !reflect.DeepEqual(current, old) {
			t.Fatalf("limit=%d offset=%d total=%d legacy=%d\n current=%+v\n legacy=%+v", page.limit, page.offset, total, oldTotal, current, old)
		}
		start := min(page.offset, len(want))
		end := min(start+page.limit, len(want))
		var ids []string
		for _, x := range current {
			ids = append(ids, x.ID)
		}
		if !reflect.DeepEqual(ids, append([]string(nil), want[start:end]...)) {
			t.Fatalf("IDs=%v want=%v", ids, want[start:end])
		}
	}
}
