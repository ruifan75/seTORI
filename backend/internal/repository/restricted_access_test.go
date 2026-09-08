package repository

import (
	"strings"
	"testing"
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
