package repository

import (
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// 専用 DB の独立 schema で、非表示と秘匿の独立性・保存後の秘匿化・削除を確認する。
// SQL が落ちて常に空になる偽陽性を避け、公開行の返却と err == nil も毎回確認する。
func TestIssue4StoredCopiesPostgres(t *testing.T) {
	db := reviewTestDB(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner, otherUser, playlist, song := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,username,password_hash,role_id)
 SELECT $1,'issue4-owner','unused',id FROM roles WHERE name='editor'`, owner)
	exec(`INSERT INTO users(id,username,password_hash,role_id)
 SELECT $1,'issue4-other','unused',id FROM roles WHERE name='editor'`, otherUser)
	exec(`INSERT INTO channels(id,name,is_hidden) VALUES('issue4-owner','fixture channel',FALSE)`)
	exec(`INSERT INTO songs(id,name,original_artist) VALUES($1,'fixture song','fixture artist')`, song)
	exec(`INSERT INTO playlists(id,user_id,name,visibility,share_slug) VALUES($1,$2,'fixture','public','issue4-slug')`, playlist, owner)
	fixtures := []struct {
		stream                                        string
		hidden, restricted                            bool
		perf, performanceSuggestion, streamSuggestion uuid.UUID
	}{
		{"public00001", false, false, uuid.New(), uuid.New(), uuid.New()},
		{"hidden00001", true, false, uuid.New(), uuid.New(), uuid.New()},
		{"secret00001", false, true, uuid.New(), uuid.New(), uuid.New()},
		{"secret00002", true, true, uuid.New(), uuid.New(), uuid.New()},
	}
	for i, f := range fixtures {
		exec(`INSERT INTO streams(id,title,stream_date,is_hidden) VALUES($1,'fixture',NOW(),$2)`, f.stream, f.hidden)
		exec(`INSERT INTO stream_channels(stream_id,channel_id,is_owner) VALUES($1,'issue4-owner',TRUE)`, f.stream)
		if f.restricted {
			exec(`INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES($1,'members_only')`, f.stream)
		}
		exec(`INSERT INTO performances(id,stream_id,song_id,start_seconds,end_seconds,order_index) VALUES($1,$2,$3,30,60,0)`, f.perf, f.stream, song)
		exec(`INSERT INTO playlist_items(playlist_id,performance_id,position) VALUES($1,$2,$3)`, playlist, f.perf, i)
		exec(`INSERT INTO edit_suggestions(id,target_type,target_id,target_label,created_by)
 VALUES($1,'performance',$2,'saved performance name and time',$3)`, f.performanceSuggestion, f.perf, owner)
		exec(`INSERT INTO edit_suggestions(id,target_type,target_id,target_key,target_label,created_by)
 VALUES($1,'stream',$2,$4,'saved stream name and time',$3)`, f.streamSuggestion, uuid.Nil, owner, f.stream)
	}
	independent := uuid.New()
	exec(`INSERT INTO edit_suggestions(id,target_type,target_id,target_label,created_by) VALUES($1,'song',$2,'ordinary song',$3)`, independent, song, owner)
	// 新しい対象種別が認可を決めずに公開になる変更と、別人の提案が混ざる変更も検出する。
	exec(`INSERT INTO edit_suggestions(target_type,target_id,created_by) VALUES('future-target',$1,$2),('song',$3,$4)`, uuid.New(), owner, song, otherUser)
	perfs := NewPerformanceRepository(db)
	playlists := NewPlaylistRepository(db, perfs)
	suggestions := NewSuggestionRepository(db)
	for _, phase := range []struct {
		name, update string
		deleted      bool
		public       []int
	}{
		{"default-deny", "", false, []int{0, 1}},
		{"channel-allow", `UPDATE channels SET members_only_policy='allow' WHERE id='issue4-owner'`, false, []int{0, 1, 2, 3}},
		{"individual-deny-wins", `UPDATE streams SET restriction_override=TRUE WHERE id='secret00002'`, false, []int{0, 1, 2}},
		{"channel-deny", `UPDATE channels SET members_only_policy='deny' WHERE id='issue4-owner'`, false, []int{0, 1}},
		{"unhide-does-not-allow", `UPDATE streams SET is_hidden=FALSE WHERE id='secret00002'`, false, []int{0, 1}},
		{"delete-target-fails-closed", `DELETE FROM streams WHERE id='secret00002'`, true, []int{0, 1}},
		{"individual-allow-wins", `UPDATE streams SET restriction_override=FALSE WHERE id='secret00001'`, true, []int{0, 1, 2}},
	} {
		t.Run(phase.name, func(t *testing.T) {
			if phase.update != "" {
				exec(phase.update)
			}
			meta, err := playlists.FindByIDWithMeta(playlist)
			if err != nil || meta == nil || meta.ItemCount != len(phase.public) {
				t.Fatalf("public item_count=%+v err=%v", meta, err)
			}
			for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
				wantPerfs := make([]uuid.UUID, 0)
				wantSuggestions := []uuid.UUID{independent}
				for i, f := range fixtures {
					present := !(phase.deleted && i == 3)
					readable := present && (access == RestrictedView || slices.Contains(phase.public, i))
					byID, err := perfs.FindByID(f.perf, access)
					if err != nil || (byID != nil) != readable {
						t.Fatalf("%s/%s byID=%+v err=%v", f.stream, accessName(access), byID, err)
					}
					direct, err := perfs.FindByStreamID(f.stream, access)
					if err != nil || (len(direct) == 1) != readable || len(direct) > 1 {
						t.Fatalf("%s direct=%+v err=%v", f.stream, direct, err)
					}
					overlap, err := perfs.FindOverlapping(f.stream, 0, 0, uuid.Nil, access)
					if err != nil || (len(overlap) == 1) != readable || len(overlap) > 1 {
						t.Fatalf("%s overlaps=%+v err=%v", f.stream, overlap, err)
					}
					if readable {
						wantPerfs = append(wantPerfs, f.perf)
					}
					// restricted:view は過去の提案も読める。公開視界は対象の存在・現在の裁定で判断する。
					if access == RestrictedView || readable {
						wantSuggestions = append(wantSuggestions, f.performanceSuggestion, f.streamSuggestion)
					}
				}
				items, err := playlists.ListItems(playlist, access)
				if err != nil {
					t.Fatal(err)
				}
				gotIDs := make([]uuid.UUID, 0, len(items))
				for _, p := range items {
					gotIDs = append(gotIDs, p.ID)
				}
				if !reflect.DeepEqual(gotIDs, wantPerfs) {
					t.Fatalf("%s playlist=%v want=%v", accessName(access), gotIDs, wantPerfs)
				}
				for _, status := range []string{"", "pending"} {
					rows, total, err := suggestions.ListByCreator(owner, status, 100, 0, access)
					if err != nil {
						t.Fatal(err)
					}
					gotIDs := make([]uuid.UUID, 0, len(rows))
					for _, s := range rows {
						gotIDs = append(gotIDs, s.ID)
					}
					if access == PublicAccess {
						slices.SortFunc(gotIDs, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
						slices.SortFunc(wantSuggestions, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
						if !reflect.DeepEqual(gotIDs, wantSuggestions) || total != len(wantSuggestions) {
							t.Fatalf("public own suggestions=%v total=%d want=%v", gotIDs, total, wantSuggestions)
						}
					} else if total != 10 || len(rows) != 10 {
						t.Fatalf("privileged own suggestions=%d total=%d want=10", len(rows), total)
					}
				}
			}
		})
	}
	// 削除済みの配信に台帳は書けない。外部送信の前にこのエラーで止める。
	if err := NewStreamRepository(db).MarkHolodexUploadAttempt("secret00002"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing upload ledger: %v", err)
	}
	if err := NewStreamRepository(db).MarkHolodexUploadAttempt("public00001"); err != nil {
		t.Fatal(err)
	}
	var recorded bool
	if err := db.QueryRow(`SELECT holodex_uploaded_at IS NOT NULL FROM streams WHERE id='public00001'`).Scan(&recorded); err != nil || !recorded {
		t.Fatalf("recorded=%t err=%v", recorded, err)
	}
	// 退避した曲名・時刻のコピーは DB から消さず、読み取りだけを濾していること。
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edit_suggestions WHERE created_by=$1`, owner).Scan(&count); err != nil || count != 10 {
		t.Fatalf("stored copies=%d err=%v", count, err)
	}
}
