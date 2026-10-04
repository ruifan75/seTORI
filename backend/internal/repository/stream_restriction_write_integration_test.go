package repository

import (
	"database/sql"
	"errors"
	"testing"
)

// SQL の字面だけでは分からない引数の反転・対象の取り違えを、保存後の列で検査する。
func TestRestrictionDecisionPersistence(t *testing.T) {
	db := reviewTestDB(t)
	if _, err := db.Exec(`INSERT INTO singers(id,name,is_hidden,members_only_policy) VALUES ('owner','所有者',false,NULL);
 INSERT INTO streams(id,title,stream_date) VALUES ('target','対象','2026-01-01'),('other','別の配信','2026-01-02');
 INSERT INTO stream_singers(stream_id,singer_id,is_owner) VALUES ('target','owner',TRUE);`); err != nil {
		t.Fatal(err)
	}
	repo := NewStreamRepository(db)
	for _, tc := range []struct{ detected, decision bool }{{false, false}, {true, false}, {true, true}, {false, true}} {
		if tc.detected {
			if err := repo.MarkMembersOnly("target"); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := db.Exec(`DELETE FROM stream_stream_tags WHERE stream_id='target' AND tag_id='members_only'`); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.SetRestrictionOverride("target", tc.decision); err != nil {
			t.Fatal(err)
		}
		var decision, basis sql.NullBool
		if err := db.QueryRow(`SELECT restriction_override,restriction_override_auto FROM streams WHERE id='target'`).Scan(&decision, &basis); err != nil {
			t.Fatal(err)
		}
		if !decision.Valid || decision.Bool != tc.decision || !basis.Valid || basis.Bool != tc.detected {
			t.Fatalf("保存した裁定=%+v 控え=%+v want=%+v", decision, basis, tc)
		}
		stream, err := repo.FindByID("target")
		if err != nil {
			t.Fatal(err)
		}
		if stream.IsRestrictedEffective != tc.decision || stream.RestrictionNeedsReview {
			t.Fatalf("確認直後の状態: %+v", stream)
		}
		var otherDecision, otherBasis sql.NullBool
		if err := db.QueryRow(`SELECT restriction_override,restriction_override_auto FROM streams WHERE id='other'`).Scan(&otherDecision, &otherBasis); err != nil {
			t.Fatal(err)
		}
		if otherDecision.Valid || otherBasis.Valid {
			t.Fatalf("別の配信が書き換わった: %+v %+v", otherDecision, otherBasis)
		}
	}
	if err := repo.SetRestrictionOverride("missing", false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("存在しない対象: %v", err)
	}
	// 検出後の警告と、再確認での解消。非表示でも警告一覧には残す。
	if err := repo.SetRestrictionOverride("target", false); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkMembersOnly("target"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE streams SET is_hidden=TRUE WHERE id='target'`); err != nil {
		t.Fatal(err)
	}
	stream, err := repo.FindByID("target")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := repo.FindRestrictionReview(10)
	if err != nil {
		t.Fatal(err)
	}
	if stream.IsRestrictedEffective || !stream.RestrictionNeedsReview || len(rows) != 1 || rows[0].ID != "target" || rows[0].BasisUnknown {
		t.Fatalf("検出後の警告: %+v rows=%+v", stream, rows)
	}
	if err := repo.UpdateMetadata("target", "題名だけ", stream.StreamDate, false, true); err != nil {
		t.Fatal(err)
	}
	stream, err = repo.FindByID("target")
	if err != nil {
		t.Fatal(err)
	}
	if !stream.RestrictionNeedsReview {
		t.Fatal("題名編集で警告が消えた")
	}
	if err := repo.SetRestrictionOverride("target", false); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.FindRestrictionReview(10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("再確認後の警告: %+v %v", rows, err)
	}
}
