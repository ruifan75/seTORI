package service

import (
	"testing"

	"github.com/ruifan75/setori/pkg/comment"
)

// キーワード辞書を適用する経路を固定する。
//
// grouped / two_stage のプロンプトは辞書と同じ category（雑談・開演・終了・告知・
// スパチャ読み・実況メモ）を、行の文脈まで見て判断している。後段に辞書を置くと
// 賢いほうの判断を馬鹿なほうが上書きし、実在の曲が消える（issue #11 の Week End）。
//
// 一方、正規表現だけになった退避経路には判断する者がいないので辞書が要る。
func TestDictionaryScopeByPath(t *testing.T) {
	// 実際に起きた誤爆。`end` は 3 文字以下 ASCII なので単語単位で一致する。
	weekEnd := comment.ParsedSong{Name: "Week End", OriginalComment: "0:52:18 Week End / 星野源"}
	dict := []string{"end", "op", "待機"}

	for _, tt := range []struct {
		path     string
		wantKept bool
		reason   string
	}{
		{"grouped", true, "AI が判断済み。辞書で上書きしない"},
		{"two_stage", true, "こちらも is_song を判断している"},
		{"regex", false, "判断する者がいないので辞書が効く"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			dictKW := dict
			if tt.path != "regex" {
				dictKW = nil
			}
			got := comment.FilterSongsWith([]comment.ParsedSong{weekEnd}, dictKW, nil, true)
			kept := len(got) > 0
			if kept != tt.wantKept {
				t.Errorf("path=%s: 残った=%v, want %v（%s）", tt.path, kept, tt.wantKept, tt.reason)
			}
		})
	}

	// 構造フィルタは経路を問わず常に効くこと（形の判断なので AI と競合しない）
	t.Run("構造フィルタは経路を問わない", func(t *testing.T) {
		for _, junk := range []comment.ParsedSong{
			{Name: "📸", OriginalComment: "0:10 📸"},
			{Name: "1", OriginalComment: "0:10 1"},
		} {
			if got := comment.FilterSongsWith([]comment.ParsedSong{junk}, nil, nil, true); len(got) > 0 {
				t.Errorf("%q が残った（辞書なしでも落ちるべき）", junk.Name)
			}
		}
	})
}
