package repository

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// 秘匿を濾すかどうかの判断は **NotRestrictedFor に集約する**。
//
// 判定式を書き写すと必ず食い違う ── 実際、以前は `EffectiveRestrictedExpr` を
// Go 側にも書いていて、材料を SELECT していない経路で「詳細は公開・一覧は秘匿」に
// なっていた。
func TestDiscoverableForDropsFilterOnlyForRestrictedView(t *testing.T) {
	if got := DiscoverableFor("st", RestrictedView); got != "TRUE" {
		t.Errorf("RestrictedView で濾している: %q", got)
	}
	pub := DiscoverableFor("st", PublicAccess)
	if pub == "TRUE" {
		t.Fatal("PublicAccess で濾していない")
	}
	for _, want := range []string{"members_only", "restriction_override"} {
		if !strings.Contains(pub, want) {
			t.Errorf("公開の条件に %q が含まれていない: %q", want, pub)
		}
	}
}

// **発見面は 2 つの軸をまとめて扱う。** 本番の会限 86 本のうち 78 本は
// is_hidden も立っているので、秘匿だけ外しても管理者からは見えないまま
// ──「歌唱があるのに 0 件に見える」を無くすのが目的なので、そこで軸を
// 分けると目的を達せられない。
func TestDiscoverableForCoversBothAxes(t *testing.T) {
	pub := DiscoverableFor("st", PublicAccess)
	for _, want := range []string{"st.is_hidden = FALSE", "members_only"} {
		if !strings.Contains(pub, want) {
			t.Errorf("公開の条件に %q が無い: %q", want, pub)
		}
	}
}

// **検索だけは is_hidden を混ぜない。** `GET /api/streams/search` は非表示も
// 意図的に含めるので、歌手・歌唱タグの絞り込みに is_hidden を入れると
// 条件に合う非表示の配信が外れる。
func TestSearchFilterDoesNotTouchIsHidden(t *testing.T) {
	for _, a := range []ViewerAccess{PublicAccess, RestrictedView} {
		if strings.Contains(NotRestrictedFor("st", a), "is_hidden") {
			t.Errorf("NotRestrictedFor が is_hidden を扱っている（access=%v）", a)
		}
	}
	src := readSourceForTest(t, "stream_repository.go")
	if strings.Contains(src, `DiscoverableFor("st", access)`) {
		t.Error("検索が DiscoverableFor を使っている（非表示が外れる）")
	}
}

// 発見面の SQL が **NotRestricted を直接呼んでいない**こと。
//
// 直接呼ぶと access に関わらず常に濾すので、権限を渡しても効かない
// ── これがまさに #53 の前の状態だった（型は必須引数なのに、
// 呼び出し側が全部 PublicAccess を選んでいた）。
func TestDiscoveryQueriesUseAccessAwareFilter(t *testing.T) {
	for _, f := range []string{
		"song_repository.go",
		"singer_repository.go",
		"artist_repository.go",
		"tag_repository.go",
		"song_match_repository.go",
		"stream_repository.go",
	} {
		src := readSourceForTest(t, f)
		// NotRestrictedFor は含んでよいので、素の NotRestricted( だけを探す。
		for _, line := range strings.Split(src, "\n") {
			if strings.Contains(line, "NotRestricted(") &&
				!strings.Contains(line, "NotRestrictedFor(") &&
				!strings.Contains(line, "DiscoverableFor(") {
				t.Errorf("%s: access を無視して濾している: %s", f, strings.TrimSpace(line))
			}
		}
	}
}

// 件数の内訳は**権限が無ければ問い合わせない**。
//
// `GetRestrictedPerformanceCount` の集計は秘匿の行そのものを数えるので、
// access を見ずに実行すると未ログインの利用者にも「秘匿が N 件ある」と返る
// ── 一覧から落としても件数から存在が漏れる、というこの機能全体の前提が崩れる。
// 早退しているかは「閉じた DB を渡しても成功する」ことで確かめられる。
// 問い合わせに進んでいれば sql.ErrConnDone が返るため。
func TestRestrictedPerformanceCountSkipsQueryWithoutPermission(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://nowhere/невозможно")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.Close() // 以後どんな問い合わせも sql.ErrConnDone になる
	repo := NewSongRepository(db)

	for _, access := range []ViewerAccess{PublicAccess} {
		got, err := repo.GetRestrictedPerformanceCount(uuid.New(), access)
		if err != nil {
			t.Errorf("access=%v で問い合わせに進んでいる: %v", access, err)
		}
		if got != 0 {
			t.Errorf("access=%v の内訳は 0 のはず: %d", access, got)
		}
	}

	// 陽性対照：権限があれば実際に問い合わせる（閉じた DB なのでエラーになる）。
	// これが無いと、中身を空にしただけの関数でも上のテストは通ってしまう。
	if _, err := repo.GetRestrictedPerformanceCount(uuid.New(), RestrictedView); err == nil {
		t.Error("RestrictedView で問い合わせていない（早退しすぎ）")
	}
}

// 内訳は**総数を絞り込んだものでなければならない**。
//
// 別々に条件を書き下ろすと、内訳が総数の部分集合でなくなる。実際、内訳にだけ
// `is_hidden = FALSE` を書いていて、**非表示かつ秘匿の歌唱が総数には入るのに
// 内訳から漏れて**いた（本番の会限 86 本のうち 78 本は `is_hidden` も立っている
// ので、少数派ではなく多数派のほうが間違う）。
//
// ここで確かめるのは「内訳のクエリが総数のクエリをそのまま含み、後ろに
// 秘匿の条件を足しただけか」。**集計結果の正しさは見ていない** ── それは
// 実 DB でないと確かめられないので、PR で端点を叩いて測ってある。
func TestRestrictedCountOnlyNarrowsTheTotal(t *testing.T) {
	for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
		total := songPerformanceCountQuery(access, false)
		restricted := songPerformanceCountQuery(access, true)

		// 母体は発見面の判定（2 軸）でなければならない。ここを秘匿だけの判定に
		// すり替えると、総数・内訳とも非表示の配信を数え始める（両方同時に
		// ずれるので、上の絞り込みの検査だけでは気付けない）。
		if want := DiscoverableFor("st", access); !strings.Contains(total, want) {
			t.Errorf("access=%v: 総数の母体が発見面の判定ではない\n総数: %q\n期待に含む: %q", access, total, want)
		}

		if !strings.HasPrefix(restricted, total) {
			t.Errorf("access=%v: 内訳が総数を絞り込む形になっていない\n総数: %q\n内訳: %q", access, total, restricted)
			continue
		}
		// 足されたぶんが秘匿の条件だけであること（母体を触っていない）。
		added := strings.TrimPrefix(restricted, total)
		if want := " AND " + EffectiveRestrictedExpr("st"); added != want {
			t.Errorf("access=%v: 秘匿の条件以外が足されている\n足された分: %q\n期待: %q", access, added, want)
		}
	}
}

// `discoverClause` を**完全一致で固定する**。
//
// **`DiscoverableFor` のテストでは守れない。** 見た目は似ているが 2 つは
// 別々の実装で、片方を変えてももう片方は変わらない（実測：`discoverClause` の
// `" AND "` を `" OR "` にしても既存のテストは全部通った）。
//
// ここが緩いと、これを部品として使っている検査
// （`promoted_scope_test.go` の期待値）も一緒に緩む ── **期待値に使う部品は、
// それ自体がどこかで固定されていなければ意味が無い。**
func TestDiscoverClauseExact(t *testing.T) {
	// **`RestrictedView` は両軸とも外す**（CLAUDE.md §2。本番の会限 86 本のうち
	// 78 本は is_hidden も立っているので、秘匿だけ外しても管理者から見えない）。
	if got := RestrictedView.discoverClause(); got != "" {
		t.Errorf("RestrictedView で濾している: %q", got)
	}

	want := " AND st.is_hidden = FALSE AND " + NotRestricted("st")
	if got := PublicAccess.discoverClause(); got != want {
		t.Errorf("PublicAccess の条件が変わっている\n got: %q\nwant: %q", got, want)
	}

	// **`AND` で繋がれていること。** `OR` にすると条件が無効になるのに、
	// 部分一致の検査では気付けない。
	if !strings.HasPrefix(PublicAccess.discoverClause(), " AND ") {
		t.Errorf("AND で繋がれていない: %q", PublicAccess.discoverClause())
	}
}

// 秘匿判定の式を、**実装の関数を一切呼ばずに**完全一致で固定する。
//
// **これが依存の根。** `NotRestricted` は
// `EffectiveRestrictedExpr` → `MembersOnlyDetectedExpr` / `allOwnersAllowExpr`
// と辿るので、どこか 1 つでも固定されていないと、そこを書き換えたときに
// **それを部品に使っている検査の期待値も一緒に変わる**。
// 実際、この 4 つはどれも書き換えても全テストが通っていた（レビューで実測）:
//
//	NotRestricted            末尾に OR TRUE      … 公開配信が左側だけで通り、
//	                                              後続のチャンネル条件を迂回できる
//	EffectiveRestrictedExpr  AND NOT → OR NOT
//	MembersOnlyDetectedExpr  EXISTS → NOT EXISTS
//	allOwnersAllowExpr       bool_and → bool_or  … 1 人でも allow なら公開になる
//
// **期待値を実装から作らないこと**が要点なので、ここは文字列を直に書く。
// 式を変えるときはここも変わる ── それが狙い（変更が必ず目に入る）。
func TestRestrictedExpressionsExact(t *testing.T) {
	const membersOnly = "EXISTS (SELECT 1 FROM stream_stream_tags mt" +
		" WHERE mt.stream_id = st.id AND mt.tag_id = 'members_only')"

	// **`bool_and`** … 所有者が複数なら**1 人でも allow でなければ伏せる**
	// （fail-closed）。`bool_or` にすると 1 人 allow で公開になる。
	// **`COALESCE(…, FALSE)`** … 所有者が居ない／方針が無いときは「allow ではない」。
	const allOwnersAllow = "COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow')" +
		" FROM stream_singers eo JOIN singers eg ON eg.id = eo.singer_id" +
		" WHERE eo.stream_id = st.id AND eo.is_owner), FALSE)"

	// **人の裁定（override）が自動判定に勝つ。** COALESCE の第 1 引数。
	const effective = "COALESCE(st.restriction_override, " + membersOnly + " AND NOT " + allOwnersAllow + ")"

	for _, c := range []struct {
		name string
		got  string
		want string
	}{
		{"MembersOnlyDetectedExpr", MembersOnlyDetectedExpr("st"), membersOnly},
		{"allOwnersAllowExpr", allOwnersAllowExpr("st"), allOwnersAllow},
		{"EffectiveRestrictedExpr", EffectiveRestrictedExpr("st"), effective},
		{"NotRestricted", NotRestricted("st"), "NOT " + effective},
	} {
		if c.got != c.want {
			t.Errorf("%s が変わっている\n got: %s\nwant: %s", c.name, c.got, c.want)
		}
	}

	// alias が全体に効くこと（片方だけ別名を見ていると、別のテーブルの列を
	// 読んでも気付けない）。
	if strings.Contains(NotRestricted("zz"), "st.") {
		t.Errorf("alias が効いていない: %s", NotRestricted("zz"))
	}
}
