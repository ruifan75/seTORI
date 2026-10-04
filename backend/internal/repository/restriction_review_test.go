package repository

import (
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 裁定の見直し（issue #26）の式を、**実装を呼ばずに**完全一致で固定する。
//
// 部品の文字列は `TestRestrictedExpressionsExact` と同じものを、ここでも直に書く。
// あちらの定数を共有しないのは、テスト同士が同じ間違いを共有しないため。
func TestRestrictionReviewExpressionsExact(t *testing.T) {
	const membersOnly = "EXISTS (SELECT 1 FROM stream_stream_tags mt" +
		" WHERE mt.stream_id = st.id AND mt.tag_id = 'members_only')"
	const allOwnersAllow = "COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow')" +
		" FROM stream_channels eo JOIN channels eg ON eg.id = eo.channel_id" +
		" WHERE eo.stream_id = st.id AND eo.is_owner), FALSE)"
	const auto = membersOnly + " AND NOT " + allOwnersAllow

	for _, c := range []struct {
		name string
		got  string
		want string
	}{
		{"AutoRestrictedExpr", AutoRestrictedExpr("st"), auto},
		// **実効値の第 2 引数と控えが同じ式であること。** 別の式から作ると、
		// 控えと現在値の比較が意味を失う。
		{"EffectiveRestrictedExpr", EffectiveRestrictedExpr("st"), "COALESCE(st.restriction_override, " + auto + ")"},
		// 3 条件：公開と裁定 / 今は伏せる判定 / 裁定の時点では伏せる判定ではなかった（または不明）。
		// `IS DISTINCT FROM TRUE` は NULL（この仕組みより前の裁定）を**出す側**へ倒す。
		// `= FALSE` にすると NULL が落ち、既存の裁定が 1 件も警告されない。
		{"RestrictionNeedsReviewExpr", RestrictionNeedsReviewExpr("st"),
			"st.restriction_override IS FALSE AND " + auto + " AND st.restriction_override_auto IS DISTINCT FROM TRUE"},
	} {
		if c.got != c.want {
			t.Errorf("%s が変わっている\n got: %s\nwant: %s", c.name, c.got, c.want)
		}
	}

	if strings.Contains(RestrictionNeedsReviewExpr("zz"), "st.") {
		t.Errorf("alias が効いていない: %s", RestrictionNeedsReviewExpr("zz"))
	}
}

// **裁定を書くときに、その時点の自動判定を同じ文で控える。**
//
// 控えを呼び出し側から渡す形にすると、読んでから書くまでの間に同期がタグを
// 付けたとき古い判定を控える。SQL の中で計算していることを、発行された文で確かめる。
func TestSetRestrictionOverrideRecordsBasis(t *testing.T) {
	for _, decision := range []bool{false, true} {
		t.Run(fmt.Sprintf("decision-%t", decision), func(t *testing.T) {
			db, rec := newRecordingDB(t)
			if err := NewStreamRepository(db).SetRestrictionOverride("abc", decision); err != nil {
				t.Fatal(err)
			}

			const auto = "EXISTS (SELECT 1 FROM stream_stream_tags mt" +
				" WHERE mt.stream_id = st.id AND mt.tag_id = 'members_only')" +
				" AND NOT COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow')" +
				" FROM stream_channels eo JOIN channels eg ON eg.id = eo.channel_id" +
				" WHERE eo.stream_id = st.id AND eo.is_owner), FALSE)"
			want := "UPDATE streams AS st SET restriction_override = $2, restriction_override_auto = (" + auto +
				"), updated_at = NOW() WHERE st.id = $1 RETURNING st.updated_at"

			issued := rec.all()
			if len(issued) != 1 {
				t.Fatalf("1 本のはずが %d 本: %q", len(issued), issued)
			}
			if got := strings.Join(strings.Fields(issued[0]), " "); got != want {
				t.Errorf("裁定の UPDATE が期待と違う\n got: %s\nwant: %s", got, want)
			}

			if len(rec.bindings) != 1 || !reflect.DeepEqual(rec.bindings[0], []driver.Value{"abc", decision}) {
				t.Fatalf("裁定の引数=%v want=[abc %t]", rec.bindings, decision)
			}
		})
	}
}

// **題名などの更新では裁定に触れない。** 書き戻すと控えの扱いが二重になり、
// 裁定と無関係な編集で警告が消える経路を作りうる。
func TestUpdateMetadataDoesNotTouchRestriction(t *testing.T) {
	db, rec := newRecordingDB(t)
	NewStreamRepository(db).UpdateMetadata("abc", "題名", time.Now(), false, false)

	issued := rec.all()
	if len(issued) != 1 {
		t.Fatalf("1 本のはずが %d 本: %q", len(issued), issued)
	}
	if strings.Contains(issued[0], "restriction_override") {
		t.Errorf("UpdateMetadata が裁定の列に触れている: %s", issued[0])
	}
	// 陽性対照：ちゃんと UPDATE を発行していること（何も出していないなら上の検査は無意味）。
	if !strings.Contains(issued[0], "UPDATE streams") {
		t.Errorf("UPDATE が発行されていない: %s", issued[0])
	}
}

// 一覧の WHERE は詳細の警告と同じ式だけ。終端まで完全一致で比べる
// （`OR TRUE` を足す・条件を外す改変が通らないように）。
func TestFindRestrictionReviewUsesReviewExpr(t *testing.T) {
	db, rec := newRecordingDB(t)
	NewStreamRepository(db).FindRestrictionReview(100)

	issued := rec.all()
	if len(issued) != 1 {
		t.Fatalf("1 本のはずが %d 本: %q", len(issued), issued)
	}
	want := "WHERE st.restriction_override IS FALSE AND " +
		"EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = st.id AND mt.tag_id = 'members_only')" +
		" AND NOT COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow')" +
		" FROM stream_channels eo JOIN channels eg ON eg.id = eo.channel_id" +
		" WHERE eo.stream_id = st.id AND eo.is_owner), FALSE)" +
		" AND st.restriction_override_auto IS DISTINCT FROM TRUE"
	if got := whereClause(t, issued[0]); got != want {
		t.Errorf("WHERE が期待と違う\n got: %s\nwant: %s", got, want)
	}
}

// 詳細の SELECT に警告の判定があること。Scan 後の値は
// TestFindByIDRestrictionValues / TestFindByIDRestrictionReviewPostgres で検査する。
func TestFindByIDSelectsNeedsReview(t *testing.T) {
	db, rec := newRecordingDB(t)
	NewStreamRepository(db).FindByID("abc")

	issued := rec.all()
	if len(issued) != 1 {
		t.Fatalf("1 本のはずが %d 本: %q", len(issued), issued)
	}
	norm := strings.Join(strings.Fields(issued[0]), " ")
	want := strings.Join(strings.Fields(RestrictionNeedsReviewExpr("streams")), " ") + " AS restriction_needs_review FROM streams"
	if !strings.Contains(norm, want) {
		t.Errorf("詳細の SELECT に警告の判定が無い: %s", norm)
	}
}
