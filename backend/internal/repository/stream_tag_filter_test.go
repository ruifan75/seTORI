package repository

import (
	"reflect"
	"strings"
	"testing"
)

// 式を独立した期待値と完全一致で固定し、AND の判定と相関を持たない形を確認する。
func TestStreamTagsAllExprExact(t *testing.T) {
	want := "(cardinality($1::text[]) = 0 OR s.id IN (" +
		"SELECT tf.stream_id FROM stream_stream_tags tf WHERE tf.tag_id = ANY($1::text[])" +
		" GROUP BY tf.stream_id HAVING COUNT(DISTINCT tf.tag_id) = cardinality($1::text[])))"
	if got := streamTagsAllExpr("s", "$1"); got != want {
		t.Errorf("式が変わっている\n got: %s\nwant: %s", got, want)
	}
	wantOther := "(cardinality($3::text[]) = 0 OR zz.id IN (" +
		"SELECT tf.stream_id FROM stream_stream_tags tf WHERE tf.tag_id = ANY($3::text[])" +
		" GROUP BY tf.stream_id HAVING COUNT(DISTINCT tf.tag_id) = cardinality($3::text[])))"
	if got := streamTagsAllExpr("zz", "$3"); got != wantOther {
		t.Errorf("alias / パラメータ: %s", got)
	}
}

// **重複を落とす**（落とさないと「持っている種類の数」が指定の数に届かず 0 件になる）。
func TestNormalizeStreamTagFilter(t *testing.T) {
	got := NormalizeStreamTagFilter([]string{" singing", "3d", "singing", "", "  ", "3d"})
	if want := []string{"singing", "3d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := NormalizeStreamTagFilter(nil); got == nil || len(got) != 0 {
		t.Errorf("nil で空の配列を返さない: %#v", got)
	}
	many := make([]string, 30)
	for i := range many {
		many[i] = string(rune('a' + i))
	}
	if got := NormalizeStreamTagFilter(many); len(got) != 30 {
		t.Errorf("正規化が条件を切り捨てた: %d", len(got))
	}
}

// タグごとの件数も**一覧と同じ式**で数える（チップの数字と一覧の件数が合うように）。
func TestCountByTagForListUsesListWhere(t *testing.T) {
	db, rec := newRecordingDB(t)
	NewStreamRepository(db).CountByTagForList(nil)
	issued := rec.all()
	if len(issued) != 1 {
		t.Fatalf("1 本のはずが %d 本: %q", len(issued), issued)
	}
	want := strings.Join(strings.Fields("WHERE "+streamListFilter("streams", false)+" AND "+streamTagsAllExpr("streams", "$1")), " ")
	if got := whereClause(t, issued[0]); got != want {
		t.Errorf("WHERE が一覧と違う\n got: %s\nwant: %s", got, want)
	}
}
