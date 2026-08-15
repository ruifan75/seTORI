package songmatch

import "testing"

// TestTitleKey_RealMisses は実際に照合を外していた表記を並べる。
// 左右が同じキーになれば直っている。
func TestTitleKey_RealMisses(t *testing.T) {
	same := []struct {
		comment string // コメントから抽出された表記
		db      string // DB に入っている表記
	}{
		{"ZIGG-ZAGG", "ZIGG-ZAGG (feat. 初音ミク)"},
		{"少女レイ", "少女レイ"},
		{"はじまりの日", "はじまりの日 (feat. Mummy-D)"},
		{"First Love (Acoustic Ver.)", "First Love"},
		{"幾億光年 piano ver.", "幾億光年"},
		{"愛おぼえていますか", "愛・おぼえていますか"},
		{"ZIGG ZAGG", "ZIGG-ZAGG"},
		{"ワールドイズマイン", "ワールドイズマイン"},
		{"炉心融解（１コーラス）", "炉心融解"},
	}
	for _, tc := range same {
		if got, want := TitleKey(tc.comment), TitleKey(tc.db); got != want {
			t.Errorf("TitleKey(%q)=%q, TitleKey(%q)=%q — 一致すべき", tc.comment, got, tc.db, want)
		}
	}
}

// TestTitleKey_KeepsVersions は「別の音源」を潰していないことを確かめる。
// ここが壊れると誤って別曲どうしを統合してしまうので、取りこぼしより重い。
// 事例はすべて実データ（曲名を素朴に括弧除去すると衝突する 13 組から）。
func TestTitleKey_KeepsVersions(t *testing.T) {
	distinct := [][2]string{
		{"モザイクロール (Reloaded)", "モザイクロール"},
		{"弱虫モンブラン (Reloaded)", "弱虫モンブラン"},
		{"Starry night (instrumental)", "Starry night"},
		{"seaglass (instrumental)", "seaglass"},
		{"思い出とペトリコール (instrumental)", "思い出とペトリコール"},
		{"ナラタージュ (instrumental)", "ナラタージュ"},
		{"シンデレラ (Giga First Night Remix)", "シンデレラ"},
		{"ワールドイズマイン (CPK! Remix)", "ワールドイズマイン"},
		{"トロイメライ (The Herb Shop Remix)", "トロイメライ"},
		{"モニタリング (Best Friend Remix)", "モニタリング"},
		{"さくら(独唱)", "さくら"},
	}
	for _, tc := range distinct {
		if a, b := TitleKey(tc[0]), TitleKey(tc[1]); a == b {
			t.Errorf("TitleKey(%q) と TitleKey(%q) がどちらも %q — 別音源なので分かれるべき", tc[0], tc[1], a)
		}
	}
}

// TestTitleKey_SameTitleDifferentSong は同名別曲がちゃんと同じキーになることを確かめる。
// これらはアーティストで判別する対象で、曲名キーの段階では衝突していてよい。
func TestTitleKey_SameTitleDifferentSong(t *testing.T) {
	collide := [][2]string{
		{"惑星ループ", "惑星ループ"},   // Eve / ナユタン星人
		{"オレンジ", "オレンジ"},     // SPYAIR / 逢坂大河ほか
		{"翼をください", "翼をください"}, // 赤い鳥 / 桜高軽音部
	}
	for _, tc := range collide {
		if a, b := TitleKey(tc[0]), TitleKey(tc[1]); a != b {
			t.Errorf("TitleKey(%q)=%q, TitleKey(%q)=%q — 同名なので同じキーになるべき", tc[0], a, tc[1], b)
		}
	}
}

func TestTitleKey_NeverEmpty(t *testing.T) {
	for _, name := range []string{"Piano", "acoustic", "フル", "Short"} {
		if TitleKey(name) == "" {
			t.Errorf("TitleKey(%q) が空 — 曲名が演奏方法と同じ語でも潰してはいけない", name)
		}
	}
}

func TestParseArtist(t *testing.T) {
	tests := []struct {
		in          string
		wantPrimary string
		wantTokens  []string
	}{
		{"みきとP feat. 初音ミク", "みきとp", []string{"みきとp", "初音ミク"}},
		{"みきとP", "みきとp", []string{"みきとp"}},
		{"ランカ・リー=中島愛", "ランカリー", []string{"ランカリー", "中島愛"}},
		{"ランカ・リー(中島愛)", "ランカリー", []string{"ランカリー", "中島愛"}},
		{"Junky(1Chorus)", "junky", []string{"junky"}},
		{"DECO*27", "deco27", []string{"deco27"}},
		{"ryo (supercell)", "ryo", []string{"ryo", "supercell"}},
		{"羽入(堀江由衣)", "羽入", []string{"堀江由衣", "羽入"}},
		{"涼宮ハルヒ(CV.平野綾)", "涼宮ハルヒ", []string{"平野綾", "涼宮ハルヒ"}},
		{"", "", nil},
	}
	for _, tc := range tests {
		got := ParseArtist(tc.in)
		if got.Primary != tc.wantPrimary {
			t.Errorf("ParseArtist(%q).Primary = %q, want %q", tc.in, got.Primary, tc.wantPrimary)
		}
		if !equalStrings(got.Tokens, tc.wantTokens) {
			t.Errorf("ParseArtist(%q).Tokens = %v, want %v", tc.in, got.Tokens, tc.wantTokens)
		}
	}
}

// TestCompareArtists_RealMisses は文字列の完全一致で落ちていたアーティスト表記。
func TestCompareArtists_RealMisses(t *testing.T) {
	tests := []struct {
		comment string
		db      string
		want    ArtistRelation
	}{
		// 実際に外れていた 4 件
		{"みきとP feat. 初音ミク", "みきとP", ArtistPrimary},
		{"東京真中 feat. 重音テト", "東京真中", ArtistPrimary},
		{"ランカ・リー(中島愛)", "ランカ・リー=中島愛", ArtistSame},
		{"Junky(1Chorus)", "Junky", ArtistSame},

		// 連名の一部だけ書かれている
		{"ryo (supercell)", "ryo (supercell), かぐや(cv.夏吉ゆうこ) & 月見ヤチヨ(cv.早見沙織)", ArtistPrimary},
		{"中島愛", "May'n & 中島愛", ArtistOverlap},

		// 別人はちゃんと別人と判定される（同名異曲の判別に使う）
		{"Eve", "ナユタン星人", ArtistNone},
		{"SPYAIR", "逢坂大河(釘宮理恵), 櫛枝実乃梨(堀江由衣) & 川嶋亜美(喜多村英梨)", ArtistNone},

		// 同一人物の別名義。文字列では解けないので Layer 2（別名テーブル）の担当。
		// ここでは「一致しない」と正直に出ることを固定しておく。
		{"松任谷由実", "荒井由実", ArtistNone},

		// 書かれていないときは否定の証拠にしない
		{"", "みきとP", ArtistUnknown},
	}
	for _, tc := range tests {
		got := CompareArtists(ParseArtist(tc.comment), ParseArtist(tc.db))
		if got != tc.want {
			t.Errorf("CompareArtists(%q, %q) = %v, want %v", tc.comment, tc.db, got, tc.want)
		}
	}
}

func TestSplitBrackets(t *testing.T) {
	outside, inner := splitBrackets("桜高軽音部 [平沢唯・秋山澪(CV:豊崎愛生)]")
	if got := trimSpace(outside); got != "桜高軽音部" {
		t.Errorf("outside = %q, want %q", got, "桜高軽音部")
	}
	if len(inner) != 2 {
		t.Fatalf("inner = %v, want 2 要素（入れ子も個別に拾う）", inner)
	}
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// アーティストが書かれていない行はコメント解析では普通に出る。
// Tokens が nil のまま DB へ渡すと NOT NULL 制約に当たるので、
// 空でも扱える形（空文字・空配列）になっていることを固定しておく。
func TestParseArtist_Empty(t *testing.T) {
	k := ParseArtist("")
	if k.Primary != "" {
		t.Errorf("Primary = %q, want empty", k.Primary)
	}
	if len(k.Tokens) != 0 {
		t.Errorf("Tokens = %v, want empty", k.Tokens)
	}
	if k.String() != "" {
		t.Errorf("String() = %q, want empty", k.String())
	}
	// 記号だけ、空白だけも同じ扱いになること
	for _, s := range []string{"   ", "()", "/", "&"} {
		if got := ParseArtist(s); len(got.Tokens) != 0 {
			t.Errorf("ParseArtist(%q).Tokens = %v, want empty", s, got.Tokens)
		}
	}
}
