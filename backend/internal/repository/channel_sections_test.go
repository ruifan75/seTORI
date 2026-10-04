package repository

import (
	"strings"
	"testing"
)

// orderByOf は SQL の最外層の ORDER BY 句（LIMIT の手前まで）を取り出す。
func orderByOf(t *testing.T, sqlText string) string {
	t.Helper()
	norm := strings.Join(strings.Fields(sqlText), " ")
	i := strings.LastIndex(norm, " ORDER BY ")
	if i < 0 {
		t.Fatalf("ORDER BY が無い: %s", norm)
	}
	rest := norm[i+len(" ORDER BY "):]
	if j := strings.Index(rest, " LIMIT "); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// **非表示を含めるときは `is_hidden` を第 1 キーに**（issue #65）。表示中が先。
// ページングがあるので画面側で分けると 2 ページ目以降で区が割れる。
//
// 期待値は「含めないときの並びの先頭に `s.is_hidden ASC, ` を足したもの」と
// 完全一致させる。並びの中身（名前・事務所の式）は実装に任せるが、
// 第 1 キーであること・昇順であること・それ以外を変えないことは縛る。
func TestChannelListPutsVisibleFirst(t *testing.T) {
	for _, sortKey := range []string{"name", "organization"} {
		for _, dir := range []string{"asc", "desc"} {
			t.Run(sortKey+"/"+dir, func(t *testing.T) {
				capture := func(includeHidden bool) string {
					db, rec := newRecordingDB(t)
					NewChannelRepository(db).FindAll(20, 0, sortKey, dir, includeHidden)
					for _, q := range rec.all() {
						if strings.Contains(q, "ORDER BY") {
							return orderByOf(t, q)
						}
					}
					t.Fatalf("一覧の SQL が発行されていない: %q", rec.all())
					return ""
				}
				base, withHidden := capture(false), capture(true)
				if want := "s.is_hidden ASC, " + base; withHidden != want {
					t.Errorf("非表示を含めた並びが期待と違う\n got: %s\nwant: %s", withHidden, want)
				}
				// 閲覧者の並びは変えない（非表示はそもそも返らない）
				if strings.Contains(base, "is_hidden") {
					t.Errorf("非表示を含めないのに is_hidden で並べている: %s", base)
				}
			})
		}
	}
}

// 事務所別の組には**表示中だけ**を入れる。非表示は別の区（名前順）。
func TestChannelGroupedAndHiddenQueries(t *testing.T) {
	db, rec := newRecordingDB(t)
	repo := NewChannelRepository(db)
	repo.FindAllGrouped()
	repo.FindHiddenByName()

	issued := rec.all()
	if len(issued) != 2 {
		t.Fatalf("2 本のはずが %d 本: %q", len(issued), issued)
	}
	if got := whereClause(t, issued[0]); got != "WHERE s.is_hidden = FALSE" {
		t.Errorf("事務所別の WHERE = %q, want WHERE s.is_hidden = FALSE", got)
	}
	if got := whereClause(t, issued[1]); got != "WHERE s.is_hidden" {
		t.Errorf("非表示の区の WHERE = %q, want WHERE s.is_hidden", got)
	}
	// 非表示の区は事務所で組まない（名前順だけ）
	if ob := orderByOf(t, issued[1]); strings.Contains(ob, "organization") || strings.Contains(ob, "sort_order") {
		t.Errorf("非表示の区を事務所で並べている: %s", ob)
	}
}

// 件数は総数と非表示の数を同じ母集合から数える（見出しの件数が一覧と合うように）。
func TestChannelListCountsHiddenFromSameSet(t *testing.T) {
	for _, includeHidden := range []bool{false, true} {
		db, rec := newRecordingDB(t)
		NewChannelRepository(db).FindAll(20, 0, "name", "asc", includeHidden)
		var count string
		for _, q := range rec.all() {
			if strings.Contains(q, "COUNT(*)") {
				count = strings.Join(strings.Fields(q), " ")
			}
		}
		want := "SELECT COUNT(*), COUNT(*) FILTER (WHERE s.is_hidden) FROM channels s"
		if !includeHidden {
			want += " WHERE s.is_hidden = FALSE"
		}
		if count != want {
			t.Errorf("includeHidden=%v: 件数の SQL = %q\nwant %q", includeHidden, count, want)
		}
	}
}
