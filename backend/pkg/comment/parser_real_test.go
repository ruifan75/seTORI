package comment

import "testing"

// 利用者から提供された実際のコメント例で、/年、/作品情報、[NN] 番号などの形式を検証する。
func TestParseCommentRealExamples(t *testing.T) {
	cases := []struct {
		line   string
		name   string
		artist string
		start  int
	}{
		// 例 1：曲名/歌手/[作品]/年
		{"0:04:32   お化けなんていないさ/弘田三枝子/1966", "お化けなんていないさ", "弘田三枝子", 4*60 + 32},
		{"0:11:15    少女レイ/みきとP feat.初音ミク/2018", "少女レイ", "みきとP feat.初音ミク", 11*60 + 15},
		{"0:22:10   ようかい体操第一(3番まで)/Dream5/アニメ「妖怪ウォッチ」ED/2014", "ようかい体操第一(3番まで)", "Dream5", 22*60 + 10},
		{"0:31:33   青と夏/Mrs. GREEN APPLE/映画「青夏 きみに恋した30日」主題歌/2018", "青と夏", "Mrs. GREEN APPLE", 31*60 + 33},
		{"1:30:45   海の幽霊/米津玄師/劇場アニメ「海獣の子供」主題歌/2019", "海の幽霊", "米津玄師", 90*60 + 45},

		// 例 2：[NN]曲名/歌手
		{"0:41:03 [01]サムライハート/SPYAIR", "サムライハート", "SPYAIR", 41*60 + 3},
		{"1:30:38 [09]嘘月/ヨルシカ", "嘘月", "ヨルシカ", 90*60 + 38},
		{"3:19:15 [20]スパークル/RADWIMPS", "スパークル", "RADWIMPS", 3*3600 + 19*60 + 15},
	}

	for _, c := range cases {
		got := ParseComment(c.line)
		if got == nil {
			t.Errorf("ParseComment(%q) = nil, 解析に成功することを期待", c.line)
			continue
		}
		if got.Name != c.name || got.OriginalArtist != c.artist {
			t.Errorf("ParseComment(%q)\n  got  name=%q artist=%q\n  want name=%q artist=%q",
				c.line, got.Name, got.OriginalArtist, c.name, c.artist)
		}
		if got.Start != c.start {
			t.Errorf("ParseComment(%q) start=%d, want %d", c.line, got.Start, c.start)
		}
	}
}
