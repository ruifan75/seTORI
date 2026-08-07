package repository

import "testing"

// normalizeEndSource は語彙外の値を unknown に丸める。
//
// DB 側の CHECK 制約に素通しすると、未知の由来が来たときに保存そのものが失敗し、
// 「由来が分からない」で済むはずの話が「歌唱記録を保存できない」に化ける。
// 由来は補助情報なので、本体の保存を守る側に倒す。
func TestNormalizeEndSource(t *testing.T) {
	valid := []string{
		EndSourceManual, EndSourceHolodex, EndSourceComment, EndSourceChat,
		EndSourceItunes, EndSourceNextStart, EndSourceDefault, EndSourceUnknown,
	}
	for _, v := range valid {
		t.Run("既知の値はそのまま/"+v, func(t *testing.T) {
			if got := normalizeEndSource(v); got != v {
				t.Errorf("normalizeEndSource(%q) = %q", v, got)
			}
		})
	}

	invalid := map[string]string{
		"空文字":    "",
		"語彙外":    "でたらめな値",
		"大文字違い":  "Chat",
		"前後の空白":  " chat ",
		"廃止された値": "human",
	}
	for name, in := range invalid {
		t.Run("不正な値は unknown へ/"+name, func(t *testing.T) {
			if got := normalizeEndSource(in); got != EndSourceUnknown {
				t.Errorf("normalizeEndSource(%q) = %q, want %q", in, got, EndSourceUnknown)
			}
		})
	}
}

// 定数と検証テーブルがずれていないこと。
// migration 030 の CHECK 制約とも一致させる必要がある。
func TestValidEndSourcesCoversAllConstants(t *testing.T) {
	all := []string{
		EndSourceManual, EndSourceHolodex, EndSourceComment, EndSourceChat,
		EndSourceItunes, EndSourceNextStart, EndSourceDefault, EndSourceUnknown,
	}
	if len(validEndSources) != len(all) {
		t.Errorf("validEndSources の件数 %d が定数の数 %d と一致しない", len(validEndSources), len(all))
	}
	for _, v := range all {
		if !validEndSources[v] {
			t.Errorf("定数 %q が validEndSources に無い", v)
		}
	}
}
