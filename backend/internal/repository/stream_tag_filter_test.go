package repository

import (
	"reflect"
	"strings"
	"testing"
)

// タグの絞り込みの式を**実装を呼ばずに**固定する（上の一覧のテストはこれを部品に使う）。
//
// AND の判定は「指定したタグのうち持っている種類の数 = 指定の数」。`DISTINCT` を外すと
// 同じタグの行が 2 つある配信で数が合わなくなり、`=` を `>=` にすると…同じだが、
// `>= 1`（どれか 1 つ＝OR）へ変える改変はここで止まる。
func TestStreamTagsAllExprExact(t *testing.T) {
	want := "(SELECT COUNT(DISTINCT tf.tag_id) FROM stream_stream_tags tf" +
		" WHERE tf.stream_id = s.id AND tf.tag_id = ANY($1::text[]))" +
		" = cardinality($1::text[])"
	if got := streamTagsAllExpr("s", "$1"); got != want {
		t.Errorf("式が変わっている\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(streamTagsAllExpr("zz", "$3"), "$1") || strings.Contains(streamTagsAllExpr("zz", "$3"), "s.id") {
		t.Errorf("alias / プレースホルダが効いていない: %s", streamTagsAllExpr("zz", "$3"))
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
	if got := NormalizeStreamTagFilter(many); len(got) != 20 {
		t.Errorf("上限 20 を超えている: %d", len(got))
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
