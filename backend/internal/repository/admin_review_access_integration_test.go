package repository

import (
	"testing"

	"github.com/google/uuid"
)

// 専用 DB・独立 schema で、管理一覧も content:edit と restricted:view を分けて濾す。
func TestAdminReviewAccessPostgres(t *testing.T) {
	db := reviewTestDB(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	const visible = "visible0001"
	const secret = "secret00001"
	exec(`INSERT INTO streams (id,title,stream_date,is_hidden,comment_songs)
 VALUES ($1,'visible',NOW(),TRUE,'[{"start":30,"name":"review song","tags":["piano"]}]'),
        ($2,'secret',NOW(),TRUE,'[{"start":30,"name":"review song","tags":["piano"]}]')`, visible, secret)
	exec(`INSERT INTO stream_stream_tags (stream_id,tag_id) VALUES ($1,'members_only')`, secret)
	songID := uuid.New()
	publicPerf := uuid.New()
	privatePerf := uuid.New()
	exec(`INSERT INTO songs (id,name,original_artist) VALUES ($1,'review song','artist')`, songID)
	exec(`INSERT INTO performances (id,stream_id,song_id,start_seconds,end_seconds,order_index)
 VALUES ($1,$2,$3,30,60,0),($4,$5,$3,30,60,0)`, publicPerf, visible, songID, privatePerf, secret)
	runs := NewBatchFillRepository(db)
	runID, err := runs.CreateRun("force", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.RecordGaps(runID, visible, []uuid.UUID{publicPerf}); err != nil {
		t.Fatal(err)
	}
	if err := runs.RecordGaps(runID, secret, []uuid.UUID{privatePerf}); err != nil {
		t.Fatal(err)
	}
	tags := NewTagRepository(db)
	streams := NewStreamRepository(db)
	for _, tc := range []struct {
		access ViewerAccess
		want   int
	}{{PublicAccess, 1}, {RestrictedView, 2}} {
		gaps, err := runs.ListGaps(runID, tc.access)
		if err != nil || len(gaps) != tc.want {
			t.Fatalf("batch gaps=%+v err=%v", gaps, err)
		}
		tagGaps, err := tags.FindTagGaps(100, tc.access)
		if err != nil || len(tagGaps) != tc.want {
			t.Fatalf("tag gaps=%+v err=%v", tagGaps, err)
		}
		candidates, err := streams.FindNonSingingCandidates(100, false, tc.access)
		if err != nil || len(candidates) != tc.want {
			t.Fatalf("candidates=%+v err=%v", candidates, err)
		}
	}
	// 否定の記録も同じ視界。候補の方だけ直す変更を検出する。
	for _, id := range []uuid.UUID{publicPerf, privatePerf} {
		if err := tags.DismissTagGap(id, "piano", nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{visible, secret} {
		if err := streams.SaveNonSingingCheck(id, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		access ViewerAccess
		want   int
	}{{PublicAccess, 1}, {RestrictedView, 2}} {
		rows, err := tags.ListTagGapDismissals(100, tc.access)
		if err != nil || len(rows) != tc.want {
			t.Fatalf("dismissed=%+v err=%v", rows, err)
		}
		candidates, err := streams.FindNonSingingCandidates(100, true, tc.access)
		if err != nil || len(candidates) != tc.want {
			t.Fatalf("dismissed candidates=%+v err=%v", candidates, err)
		}
	}
	// 記録時の配信 ID のまま秘匿を判定すると、移動後の歌唱が漏れる。
	exec(`UPDATE performances SET stream_id=$1,start_seconds=31 WHERE id=$2`, secret, publicPerf)
	gaps, err := runs.ListGaps(runID, PublicAccess)
	if err != nil || len(gaps) != 0 {
		t.Fatalf("moved private gaps=%+v err=%v", gaps, err)
	}
	exec(`UPDATE streams SET is_hidden=FALSE WHERE id=$1`, secret)
	gaps, err = runs.ListGaps(runID, PublicAccess)
	if err != nil || len(gaps) != 0 {
		t.Fatalf("unhidden private gaps=%+v err=%v", gaps, err)
	}
	// 人の公開裁定があれば陽性に戻る（is_hidden とは独立）。
	exec(`UPDATE streams SET restriction_override=FALSE WHERE id=$1`, secret)
	gaps, err = runs.ListGaps(runID, PublicAccess)
	if err != nil || len(gaps) != 2 {
		t.Fatalf("allowed gaps=%+v err=%v", gaps, err)
	}
	// 保存済みの提案も現在の公開可否に従う。対象削除は fail-closed。
	suggestionID := uuid.New()
	exec(`INSERT INTO edit_suggestions (id,target_type,target_id,target_label) VALUES ($1,'performance',$2,'snapshot')`, suggestionID, privatePerf)
	suggestions := NewSuggestionRepository(db)
	row, err := suggestions.FindByIDForViewer(suggestionID, PublicAccess)
	if err != nil || row == nil {
		t.Fatalf("public suggestion=%+v err=%v", row, err)
	}
	exec(`UPDATE streams SET restriction_override=TRUE WHERE id=$1`, secret)
	row, err = suggestions.FindByIDForViewer(suggestionID, PublicAccess)
	if err != nil || row != nil {
		t.Fatalf("private suggestion=%+v err=%v", row, err)
	}
	row, err = suggestions.FindByIDForViewer(suggestionID, RestrictedView)
	if err != nil || row == nil {
		t.Fatalf("privileged suggestion=%+v err=%v", row, err)
	}
	exec(`DELETE FROM performances WHERE id=$1`, privatePerf)
	row, err = suggestions.FindByIDForViewer(suggestionID, PublicAccess)
	if err != nil || row != nil {
		t.Fatalf("deleted target suggestion=%+v err=%v", row, err)
	}
}
