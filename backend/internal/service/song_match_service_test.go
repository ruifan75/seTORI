package service

import (
	"testing"

	"github.com/ruifan75/setori/pkg/songmatch"
)

// 松任谷由実 = 荒井由実 を登録した状態の対応表。
// 代表はグループ内で辞書順が最小の name_key（LoadArtistAliasMap と同じ規則）。
func yumiCanon() map[string]string {
	a, b := songmatch.ParseArtist("荒井由実").Primary, songmatch.ParseArtist("松任谷由実").Primary
	rep := a
	if b < a {
		rep = b
	}
	return map[string]string{a: rep, b: rep}
}

func TestCompareArtists_AliasRescuesMismatch(t *testing.T) {
	q := songmatch.ParseArtist("松任谷由実")
	db := songmatch.ParseArtist("荒井由実")

	// 別名義が未登録なら「別人」。これが 030 時点の挙動。
	if rel, via := compareArtists(q, db, nil); rel != songmatch.ArtistNone || via {
		t.Errorf("別名義なし: rel=%v via=%v, want ArtistNone/false", rel, via)
	}

	// 登録後は一致し、「別名義で救われた」と分かる
	rel, via := compareArtists(q, db, yumiCanon())
	if rel < songmatch.ArtistOverlap {
		t.Errorf("別名義あり: rel=%v, want >= ArtistOverlap", rel)
	}
	if !via {
		t.Error("別名義で一致したことが呼び出し側に伝わっていない（判定理由の表示に使う）")
	}
}

// 別名義を登録しても、無関係なアーティストまで一致してはいけない。
func TestCompareArtists_AliasDoesNotLeak(t *testing.T) {
	canon := yumiCanon()
	pairs := [][2]string{
		{"Eve", "ナユタン星人"},
		{"松任谷由実", "SPYAIR"},
		{"荒井由実", "YOASOBI"},
	}
	for _, p := range pairs {
		rel, _ := compareArtists(songmatch.ParseArtist(p[0]), songmatch.ParseArtist(p[1]), canon)
		if rel != songmatch.ArtistNone {
			t.Errorf("compareArtists(%q, %q) = %v, want ArtistNone", p[0], p[1], rel)
		}
	}
}

// 素の表記で一致するものは別名義を経由したことにしない（理由表示が濁るため）。
func TestCompareArtists_DirectMatchNotFlaggedAsAlias(t *testing.T) {
	rel, via := compareArtists(
		songmatch.ParseArtist("みきとP feat. 初音ミク"),
		songmatch.ParseArtist("みきとP"),
		yumiCanon())
	if rel != songmatch.ArtistPrimary {
		t.Errorf("rel = %v, want ArtistPrimary", rel)
	}
	if via {
		t.Error("素の表記で一致したのに別名義扱いになっている")
	}
}

func TestCanonicalizeArtistKey(t *testing.T) {
	canon := yumiCanon()
	a := canonicalizeArtistKey(songmatch.ParseArtist("松任谷由実"), canon)
	b := canonicalizeArtistKey(songmatch.ParseArtist("荒井由実"), canon)
	if a.String() != b.String() {
		t.Errorf("別名義が同じキーに寄っていない: %q vs %q", a.String(), b.String())
	}
	// 対応表に無い名前はそのまま
	c := canonicalizeArtistKey(songmatch.ParseArtist("YOASOBI"), canon)
	if c.String() != songmatch.ParseArtist("YOASOBI").String() {
		t.Errorf("無関係な名前が書き換わっている: %q", c.String())
	}
}
