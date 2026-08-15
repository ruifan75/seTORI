package perftag

import (
	"slices"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name     string
		tags     []string
		verbatim string
		want     []string
	}{
		{"既知のタグは通す", []string{"piano", "short"}, "曲名", []string{"piano", "short"}},
		{"語彙外は落とす", []string{"piano", "Piano ver.", "instrumental"}, "曲名", []string{"piano"}},
		{"別の言い方は寄せる", []string{"ピアノ"}, "曲名", []string{"piano"}},
		{"重複を除く", []string{"piano", "piano"}, "曲名", []string{"piano"}},

		// performance_tags.id は migration 019 で 'self-accompanied' に変わっている。
		// AI には日本語で出させているので、DB の ID へ寄せないと保存時に捨てられる。
		{"弾き語りは DB の ID へ寄せる", []string{"弾き語り"}, "曲名", []string{"self-accompanied"}},
		{"ギター弾き語りも同じ", []string{"ギター弾き語り"}, "曲名", []string{"self-accompanied"}},
		{"DB の ID を直接渡しても通る", []string{"self-accompanied"}, "曲名", []string{"self-accompanied"}},
		{"空は落とす", []string{"", "  "}, "曲名", nil},

		// 報告された実データ。Holodex の曲名にだけバージョン表記があり、
		// AI がタグを返さなくても曲名から拾えること
		{
			"Holodex の (1 Chorus) から short を導く",
			nil,
			"そばかす (1 Chorus) / Sobakasu",
			[]string{"short"},
		},
		{"1chorus（空白なし）", nil, "Junky(1Chorus)", []string{"short"}},
		{"全角のコーラス表記", nil, "炉心融解（1コーラス）", []string{"short"}},
		{"既に short があれば増やさない", []string{"short"}, "そばかす (1 Chorus)", []string{"short"}},
		{"バージョン表記が無ければ何も足さない", nil, "そばかす", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Normalize(c.tags, c.verbatim)
			if !slices.Equal(got, c.want) {
				t.Errorf("Normalize(%v, %q) = %v, want %v", c.tags, c.verbatim, got, c.want)
			}
		})
	}
}
