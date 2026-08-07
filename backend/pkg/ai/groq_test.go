package ai

import (
	"strings"
	"testing"
)

// TestEstimateMaxTokens は出力枠の見積もりが実データの分布を満たすことを確認する。
//
// 見積もりが小さすぎると応答が途中で切れて JSON パースに失敗する（＝解析が劣化する）。
// 大きすぎるとレート制限を予約分で食い潰す。両側を守れているかを見る。
func TestEstimateMaxTokens(t *testing.T) {
	user := func(chars int) []ChatMessage {
		return []ChatMessage{
			{Role: "system", Content: strings.Repeat("あ", 1668)}, // system は枠に影響しない
			{Role: "user", Content: strings.Repeat("あ", chars)},
		}
	}

	t.Run("短い入力でも下限を確保する", func(t *testing.T) {
		if got := estimateMaxTokens(user(10)); got != maxTokensFloor {
			t.Errorf("estimateMaxTokens(10字) = %d, want %d", got, maxTokensFloor)
		}
	})

	t.Run("長い入力でも上限を超えない", func(t *testing.T) {
		if got := estimateMaxTokens(user(100000)); got != maxTokensCeil {
			t.Errorf("estimateMaxTokens(100000字) = %d, want %d", got, maxTokensCeil)
		}
	})

	t.Run("system prompt の長さは枠に影響しない", func(t *testing.T) {
		short := []ChatMessage{
			{Role: "system", Content: "短い"},
			{Role: "user", Content: strings.Repeat("あ", 2000)},
		}
		long := []ChatMessage{
			{Role: "system", Content: strings.Repeat("あ", 50000)},
			{Role: "user", Content: strings.Repeat("あ", 2000)},
		}
		if estimateMaxTokens(short) != estimateMaxTokens(long) {
			t.Errorf("system の長さで枠が変わっている: %d vs %d",
				estimateMaxTokens(short), estimateMaxTokens(long))
		}
	})

	// 実測分布（コメントを持つ 845 配信）。user 文字数と、その配信で必要になる出力量。
	// 必要量は「タイムスタンプ行数 × 1要素あたり約110字」から算出している。
	t.Run("実測分布で必要な出力量を満たす", func(t *testing.T) {
		cases := []struct {
			name      string
			userChars int
			lines     int
		}{
			{"中央値", 390, 14},
			{"平均", 528, 18},
			{"p90", 1364, 43},
			{"最大", 3694, 99},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := estimateMaxTokens(user(c.userChars))
				need := int(float64(c.lines*110) / charsPerToken)
				if got < need {
					t.Errorf("枠 %d < 必要 %d（応答が切り詰められる）", got, need)
				}
			})
		}
	})

	// 固定 8192 に対してどれだけ予約を減らせたか。無料枠の消費に直結する。
	t.Run("中央値ケースで予約を大幅に減らせている", func(t *testing.T) {
		got := estimateMaxTokens(user(390))
		if got >= 8192 {
			t.Errorf("中央値ケースで枠が %d のまま。予約が減っていない", got)
		}
	})
}
