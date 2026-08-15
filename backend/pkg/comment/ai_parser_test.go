package comment

import "testing"

// fakeChatter は固定の AI レスポンスを返し、verbatim ガードと統合ロジックを決定論的にテストする。
type fakeChatter struct{ response string }

func (f fakeChatter) SimpleChat(_, _ string) (string, error) { return f.response, nil }

func TestParseCommentsWithAIVerbatimGuard(t *testing.T) {
	// 2 行：
	// line1 は空白区切りのみ（正規表現では分割不可）→ AI が正しく分割し、原文どおりなので AI の結果を採用する
	//  line2 の AI の artist は幻覚（原文にない）→ ガードで拒否 → 正規表現の artist へ戻る
	comments := []string{
		"1:23 曲名A アーティストA\n2:34 テスト曲/正アーティスト/2020",
	}
	ai := fakeChatter{response: `[
		{"line":1,"is_song":true,"start_ts":"1:23","name":"曲名A","artist":"アーティストA"},
		{"line":2,"is_song":true,"start_ts":"2:34","name":"テスト曲","artist":"偽アーティスト"}
	]`}

	got, err := ParseCommentsWithAI(ai, comments)
	if err != nil {
		t.Fatalf("ParseCommentsWithAI error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d songs, want 2: %+v", len(got), got)
	}

	// line1：AI が空白区切りを分割（正規表現では不可）。原文検証を通過したので AI の結果を採用する
	if got[0].Name != "曲名A" || got[0].OriginalArtist != "アーティストA" || got[0].Start != 83 {
		t.Errorf("song1 = name=%q artist=%q start=%d; want 曲名A / アーティストA / 83",
			got[0].Name, got[0].OriginalArtist, got[0].Start)
	}

	// line2：AI の artist「偽アーティスト」は原文に無い → ガードで拒否 → 正規表現の「正アーティスト」へ戻す
	if got[1].Name != "テスト曲" || got[1].OriginalArtist != "正アーティスト" {
		t.Errorf("song2 = name=%q artist=%q; want テスト曲 / 正アーティスト (regex fallback)",
			got[1].Name, got[1].OriginalArtist)
	}
}
