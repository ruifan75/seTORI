package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"
)

// SETORI_TEST_DATABASE_URL の専用 DB 内の独立 schema に全 migration（070 含む）を適用する。
func processedState(t *testing.T, db *sql.DB, id string) bool {
	t.Helper()
	var v bool
	if err := db.QueryRow(`SELECT is_processed FROM streams WHERE id=$1`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}
func processedFixture(t *testing.T, db *sql.DB, id string, value, hidden bool) {
	t.Helper()
	visibilityExec(t, db, `INSERT INTO streams(id,title,stream_date,is_processed,is_hidden) VALUES ($1,$1,'2026-10-03T12:00:00Z',$2,$3)`, id, value, hidden)
}
func TestProcessedPostgresApplyRevertBothDirections(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			db := visibilityPostgres(t)
			r := NewProcessedReviewRepository(db)
			for _, id := range []string{"one", "two", "gone", "value"} {
				processedFixture(t, db, id, !after, true)
			}
			run, err := r.Preview([]string{"one", "two", "gone", "value"}, after, nil)
			if err != nil {
				t.Fatal(err)
			}
			if processedState(t, db, "one") != !after {
				t.Fatal("preview wrote to streams")
			}
			// 確認後に自動処理が解析素材・更新時刻を書いても、前値は変わらない。
			visibilityExec(t, db, `UPDATE streams SET updated_at=NOW(), comment_raw='[{"text":"before apply"}]'::jsonb WHERE id='one'`)
			var changedTimes int
			if err := db.QueryRow(`SELECT COUNT(*) FROM processed_review_items i JOIN streams s ON s.id=i.stream_id
 WHERE i.run_id=$1 AND s.updated_at <> i.before_updated_at`, run).Scan(&changedTimes); err != nil || changedTimes != 1 {
				t.Fatal("test did not advance pre-apply timestamp", changedTimes, err)
			}
			n, err := r.Apply(run)
			if err != nil || n != 4 {
				t.Fatal(n, err)
			}
			if processedState(t, db, "one") != after {
				t.Fatal("apply did not write destination")
			}
			var audit int
			if err := db.QueryRow(`SELECT COUNT(*) FROM processed_review_items i JOIN streams s ON s.id=i.stream_id
 WHERE i.run_id=$1 AND i.field_name='is_processed' AND i.before_processed=$2 AND i.after_processed=$3
 AND i.before_updated_at IS NOT NULL AND i.after_updated_at=s.updated_at`, run, !after, after).Scan(&audit); err != nil || audit != 4 {
				t.Fatal("missing audit values/timestamps", audit, err)
			}
			// 同期・コメント再取得は取り消しを妨げず、対象外の変更を保つ。
			visibilityExec(t, db, `UPDATE streams SET updated_at=NOW(), comment_raw='[{"text":"after apply"}]'::jsonb WHERE id IN ('one','two')`)
			visibilityExec(t, db, `UPDATE streams SET title='edited' WHERE id='two'`)
			if err := db.QueryRow(`SELECT COUNT(*) FROM processed_review_items i JOIN streams s ON s.id=i.stream_id
 WHERE i.run_id=$1 AND s.id IN ('one','two') AND s.updated_at <> i.after_updated_at`, run).Scan(&changedTimes); err != nil || changedTimes != 2 {
				t.Fatal("test did not advance post-apply timestamps", changedTimes, err)
			}
			// 人が処理済み状態を変えた行・削除済みの行は取り消しで触らない。
			visibilityExec(t, db, `UPDATE streams SET is_processed=$1 WHERE id='value'`, !after)
			visibilityExec(t, db, `DELETE FROM streams WHERE id='gone'`)
			n, skipped, err := r.Revert(run)
			if err != nil || n != 2 || skipped != 2 {
				t.Fatal(n, skipped, err)
			}
			if processedState(t, db, "one") != !after || processedState(t, db, "two") != !after || processedState(t, db, "value") != !after {
				t.Fatal("revert lost later changes")
			}
			var title, comment string
			var hidden bool
			if err := db.QueryRow(`SELECT title,is_hidden,comment_raw->0->>'text' FROM streams WHERE id='two'`).Scan(&title, &hidden, &comment); err != nil || title != "edited" || !hidden || comment != "after apply" {
				t.Fatal(title, hidden, comment, err)
			}
			var restoredIDs string
			if err := db.QueryRow(`SELECT string_agg(stream_id,',' ORDER BY stream_id) FROM processed_review_items WHERE run_id=$1 AND reverted_at IS NOT NULL`, run).Scan(&restoredIDs); err != nil || restoredIDs != "one,two" {
				t.Fatal("wrong rows restored", restoredIDs, err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM processed_review_items WHERE run_id=$1`, run).Scan(&audit); err != nil || audit != 4 {
				t.Fatal("deletion erased audit", audit, err)
			}
			if _, _, err := r.Revert(run); !errors.Is(err, ErrProcessedConflict) {
				t.Fatal("replayed revert", err)
			}
		})
	}
}
func TestProcessedPostgresConflictAllOrNothing(t *testing.T) {
	for _, after := range []bool{false, true} {
		for _, change := range []string{"value", "deleted"} {
			t.Run(fmt.Sprintf("after=%t/%s", after, change), func(t *testing.T) {
				db := visibilityPostgres(t)
				r := NewProcessedReviewRepository(db)
				processedFixture(t, db, "one", !after, false)
				processedFixture(t, db, "two", !after, false)
				run, err := r.Preview([]string{"one", "two"}, after, nil)
				if err != nil {
					t.Fatal(err)
				}
				switch change {
				case "value":
					visibilityExec(t, db, `UPDATE streams SET is_processed=$1 WHERE id='two'`, after)
				case "deleted":
					visibilityExec(t, db, `DELETE FROM streams WHERE id='two'`)
				}
				if _, err := r.Apply(run); !errors.Is(err, ErrProcessedConflict) {
					t.Fatal("stale snapshot applied", err)
				}
				if processedState(t, db, "one") != !after {
					t.Fatal("partial write committed")
				}
				var applied int
				if err := db.QueryRow(`SELECT COUNT(*) FROM processed_review_items WHERE run_id=$1 AND after_updated_at IS NOT NULL`, run).Scan(&applied); err != nil || applied != 0 {
					t.Fatal("audit partly applied", applied, err)
				}
				var status string
				if err := db.QueryRow(`SELECT status FROM processed_review_runs WHERE id=$1`, run).Scan(&status); err != nil || status != "preview" {
					t.Fatal(status, err)
				}
			})
		}
	}
}
func TestProcessedPostgresFiltersAndRestriction(t *testing.T) {
	db := visibilityPostgres(t)
	r := NewProcessedReviewRepository(db)
	visibilityExec(t, db, `INSERT INTO channels(id,name,is_hidden) VALUES ('owner','owner',FALSE),('guest','guest',TRUE)`)
	visibilityExec(t, db, `INSERT INTO songs(id,name,original_artist) VALUES ($1,'test','artist')`, visibilityID)
	for _, id := range []string{"public", "member", "empty", "processed"} {
		processedFixture(t, db, id, id == "processed", id == "member")
		visibilityExec(t, db, `INSERT INTO stream_channels(stream_id,channel_id,is_owner) VALUES ($1,'owner',TRUE),($1,'guest',FALSE)`, id)
		visibilityExec(t, db, `INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES ($1,'singing')`, id)
		if id != "empty" {
			visibilityExec(t, db, `INSERT INTO performances(stream_id,song_id,start_seconds,end_seconds,order_index) VALUES ($1,$2,10,60,1)`, id, visibilityID)
		}
	}
	visibilityExec(t, db, `INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES ('member','members_only')`)
	yes, no := true, false
	from := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	until := from.AddDate(0, 0, 1)
	cases := []struct {
		name   string
		f      ProcessedReviewFilters
		access ViewerAccess
		want   []string
	}{
		{"default", ProcessedReviewFilters{AfterProcessed: true}, PublicAccess, []string{"empty", "member", "public"}},
		{"remove", ProcessedReviewFilters{AfterProcessed: false}, PublicAccess, []string{"processed"}},
		{"public-has", ProcessedReviewFilters{AfterProcessed: true, HasPerformances: &yes}, PublicAccess, []string{"public"}},
		{"public-none", ProcessedReviewFilters{AfterProcessed: true, HasPerformances: &no}, PublicAccess, []string{"empty", "member"}},
		{"restricted-has", ProcessedReviewFilters{AfterProcessed: true, HasPerformances: &yes}, RestrictedView, []string{"member", "public"}},
		{"restricted-none", ProcessedReviewFilters{AfterProcessed: true, HasPerformances: &no}, RestrictedView, []string{"empty"}},
		{"combined", ProcessedReviewFilters{AfterProcessed: true, ChannelID: "guest", TagIDs: []string{"singing", "members_only", "singing"}, Hidden: &yes, From: &from, Until: &until, HasPerformances: &yes}, RestrictedView, []string{"member"}},
		{"combined-public", ProcessedReviewFilters{AfterProcessed: true, ChannelID: "guest", TagIDs: []string{"singing", "members_only"}, HasPerformances: &yes}, PublicAccess, []string{}},
		{"from-exclusive-result", ProcessedReviewFilters{AfterProcessed: true, From: &until}, RestrictedView, []string{}},
		{"until-exclusive-result", ProcessedReviewFilters{AfterProcessed: true, Until: &from}, RestrictedView, []string{}},
		{"query", ProcessedReviewFilters{AfterProcessed: true, Query: "public", Hidden: &no}, PublicAccess, []string{"public"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, n, err := r.Candidates(tc.f, 100, 0, tc.access)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			sort.Strings(ids)
			if n != len(tc.want) || !reflect.DeepEqual(ids, tc.want) {
				t.Fatal(ids, n, "want", tc.want)
			}
		})
	}
	run, err := r.Preview([]string{"member"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(run); err != nil {
		t.Fatal(err)
	}
	// 処理済み変更から秘匿・非表示は変更されない。陽性と陰性を両方見る。
	perf := NewPerformanceRepository(db)
	for _, tc := range []struct {
		id     string
		access ViewerAccess
		want   int
	}{{"public", PublicAccess, 1}, {"member", PublicAccess, 0}, {"member", RestrictedView, 1}} {
		rows, err := perf.FindByStreamID(tc.id, tc.access)
		if err != nil || len(rows) != tc.want {
			t.Fatal(tc, len(rows), err)
		}
	}
	if !visibilityHidden(t, db, "member") {
		t.Fatal("processed change also changed visibility")
	}
	// 候補でない行が1件でも混ざった preview は台帳ごと確定しない。
	var before, after int
	if err := db.QueryRow(`SELECT COUNT(*) FROM processed_review_runs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Preview([]string{"empty", "processed"}, true, nil); !errors.Is(err, ErrProcessedConflict) {
		t.Fatal("mixed-state selection accepted", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM processed_review_runs`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("failed preview persisted a run")
	}
}

func TestProcessedPostgresTitleSearchLiteral(t *testing.T) {
	db := visibilityPostgres(t)
	r := NewProcessedReviewRepository(db)
	for _, x := range []struct{ id, title string }{
		{"percent", "歌50%曲"}, {"percent-other", "歌500曲"},
		{"underscore", "歌a_b曲"}, {"underscore-other", "歌axb曲"},
		{"escape", "歌wow!曲"}, {"escape-other", "歌wow曲"},
		{"backslash", `歌C:\music曲`}, {"backslash-other", "歌C:music曲"},
		{"combined", `歌!50%_\曲`}, {"combined-other", `歌!500x\曲`},
	} {
		visibilityExec(t, db, `INSERT INTO streams(id,title,stream_date,is_processed) VALUES ($1,$2,NOW(),FALSE)`, x.id, x.title)
	}
	for _, tc := range []struct{ query, id string }{
		{"歌50%", "percent"}, {"a_b", "underscore"}, {"wow!", "escape"},
		{`C:\music`, "backslash"}, {`!50%_\`, "combined"}, {"missing%_", ""},
	} {
		t.Run(tc.query, func(t *testing.T) {
			rows, n, err := r.Candidates(ProcessedReviewFilters{AfterProcessed: true, Query: tc.query}, 100, 0, PublicAccess)
			want := 1
			if tc.id == "" {
				want = 0
			}
			if err != nil || n != want || len(rows) != want || (want == 1 && rows[0].ID != tc.id) {
				t.Fatal(rows, n, "want", tc.id, err)
			}
		})
	}
}
