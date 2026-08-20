package service

import "testing"

// 非表示の扱いは「既定は除く」が要。既定が変わると、通常運用の一括分析が
// 雑談・ゲーム配信まで毎回 AI にかけ始める（720 本ぶん）。
func TestParseHiddenFilter(t *testing.T) {
	tests := []struct {
		in    string
		want  string // nil / true / false
		label string
	}{
		{"", "false", "未指定は従来どおり非表示を除く"},
		{"false", "false", "明示的に除く"},
		{"true", "true", "非表示だけ（規則を変えた後の棚卸し）"},
		{"all", "nil", "両方"},
		{"yes", "false", "知らない値は安全側（除く）に倒す"},
		{"TRUE", "false", "大文字は受け付けない（語彙は singers の hidden と同じ）"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got := ParseHiddenFilter(tt.in)
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
			if name != hiddenLabel(got) && !(name == "nil" && hiddenLabel(got) == "all") {
				t.Errorf("hiddenLabel が往復しない: %s ↔ %s", name, hiddenLabel(got))
			}
		})
	}
}
