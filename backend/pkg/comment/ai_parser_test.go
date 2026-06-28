package comment

import "testing"

// fakeChatter 回傳固定的 AI 回應，用來確定性地測試 verbatim 護欄與合併邏輯
type fakeChatter struct{ response string }

func (f fakeChatter) SimpleChat(_, _ string) (string, error) { return f.response, nil }

func TestParseCommentsWithAIVerbatimGuard(t *testing.T) {
	// 兩行：
	//  line1 只有空格分隔（正則切不開）→ AI 正確切分、且逐字 → 採用 AI
	//  line2 AI 的 artist 是幻覺（不在原文）→ 護欄拒絕 → 退回正則的 artist
	comments := []string{
		"1:23 曲名A アーティストA\n2:34 テスト曲/正アーティスト/2020",
	}
	ai := fakeChatter{response: `[
		{"line":1,"start":83,"end":0,"is_song":true,"name":"曲名A","artist":"アーティストA"},
		{"line":2,"start":154,"end":0,"is_song":true,"name":"テスト曲","artist":"偽アーティスト"}
	]`}

	got, err := ParseCommentsWithAI(ai, comments)
	if err != nil {
		t.Fatalf("ParseCommentsWithAI error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d songs, want 2: %+v", len(got), got)
	}

	// line1：AI 把空格分隔切開（正則做不到），逐字驗證通過 → 採用 AI
	if got[0].Name != "曲名A" || got[0].OriginalArtist != "アーティストA" || got[0].Start != 83 {
		t.Errorf("song1 = name=%q artist=%q start=%d; want 曲名A / アーティストA / 83",
			got[0].Name, got[0].OriginalArtist, got[0].Start)
	}

	// line2：AI artist「偽アーティスト」不在原文 → 護欄拒絕 → 退回正則「正アーティスト」
	if got[1].Name != "テスト曲" || got[1].OriginalArtist != "正アーティスト" {
		t.Errorf("song2 = name=%q artist=%q; want テスト曲 / 正アーティスト (regex fallback)",
			got[1].Name, got[1].OriginalArtist)
	}
}
