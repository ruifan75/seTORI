package handler

import (
	"strings"
	"testing"
)

// 一括分析の起動口は、hidden の値ひとつで対象が 700 本近く変わる。
// 惜しい間違いを黙って既定へ倒すと、意図と違う対象で走り出したうえ 202 が返り、
// 呼んだ側は成功したと思い込む。だから厳しく弾くことを固定する。
func TestParseBatchAnalyzeRequest(t *testing.T) {
	tests := []struct {
		label      string
		body       string
		wantErr    bool
		wantHidden string
		wantMode   string
	}{
		{"空ボディは許容（既定へ）", "", false, "", ""},
		{"空白だけも許容", "   \n", false, "", ""},
		{"正常", `{"mode":"reanalyze","hidden":"true"}`, false, "true", "reanalyze"},
		{"hidden 省略", `{"mode":"reanalyze"}`, false, "", "reanalyze"},

		// ここから、以前は黙って既定（＝表示中だけ）で走り出していた形
		{"hidden に JSON の null", `{"mode":"reanalyze","hidden":null}`, true, "", ""},
		{"hidden に JSON の真偽値", `{"mode":"reanalyze","hidden":true}`, true, "", ""},
		{"後続の JSON 値", `{"hidden":"true"}{"hidden":"false"}`, true, "", ""},
		{"末尾のゴミ", `{"hidden":"true"} trailing`, true, "", ""},
		{"壊れた JSON", `{"hidden":`, true, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got, err := parseBatchAnalyzeRequest(strings.NewReader(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーになるべき（got=%+v）", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期せぬエラー: %v", err)
			}
			if got.Hidden != tt.wantHidden || got.Mode != tt.wantMode {
				t.Errorf("= (mode=%q hidden=%q), want (mode=%q hidden=%q)",
					got.Mode, got.Hidden, tt.wantMode, tt.wantHidden)
			}
		})
	}
}
