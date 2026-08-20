package service

import "testing"

// 非表示の扱いは「既定は除く」が要。既定が変わると、通常運用の一括分析が
// 雑談・ゲーム配信まで毎回 AI にかけ始める（本番で 720 本）。
//
// そして**知らない値は既定へ倒さずエラーにする**。ここは読み取りではなく
// 数百本を AI にかける背景ジョブの起動口で、惜しい間違い（TRUE / yes / JSON の真偽値）を
// 黙って既定として受け付けると、意図と違う対象で走り出したうえ 202 が返る。
func TestParseHiddenFilter(t *testing.T) {
	tests := []struct {
		in      string
		want    string // nil / true / false
		wantErr bool
		label   string
	}{
		{"", "false", false, "未指定は従来どおり非表示を除く"},
		{"false", "false", false, "明示的に除く"},
		{"true", "true", false, "非表示だけ（規則を変えた後の棚卸し）"},
		{"all", "nil", false, "両方"},

		// ここから、以前は黙って「除く」に倒れていた値
		{"TRUE", "", true, "大文字は受け付けない"},
		{"yes", "", true, "yes は受け付けない"},
		{"1", "", true, "1 は受け付けない"},
		{"hidden", "", true, "無関係な文字列"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got, err := ParseHiddenFilter(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseHiddenFilter(%q) はエラーになるべき（got=%v）", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHiddenFilter(%q) が予期せずエラー: %v", tt.in, err)
			}
			var name string
			switch {
			case got == nil:
				name = "nil"
			case *got:
				name = "true"
			default:
				name = "false"
			}
			if name != tt.want {
				t.Errorf("ParseHiddenFilter(%q) = %s, want %s", tt.in, name, tt.want)
			}
		})
	}
}

// hiddenLabel は状態表示とログに出る。ParseHiddenFilter と往復すること。
func TestHiddenLabelRoundTrip(t *testing.T) {
	for _, in := range []string{"", "false", "true", "all"} {
		got, err := ParseHiddenFilter(in)
		if err != nil {
			t.Fatalf("ParseHiddenFilter(%q): %v", in, err)
		}
		label := hiddenLabel(got)
		back, err := ParseHiddenFilter(label)
		if err != nil {
			t.Fatalf("hiddenLabel の出力 %q を読み直せない: %v", label, err)
		}
		if (back == nil) != (got == nil) || (back != nil && *back != *got) {
			t.Errorf("往復しない: %q → %q → 別の値", in, label)
		}
	}
}
