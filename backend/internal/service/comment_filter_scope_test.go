package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ruifan75/setori/pkg/comment"
)

// キーワード辞書を適用する経路を固定する。
//
// grouped / two_stage のプロンプトは辞書と同じ category（雑談・開演・終了・告知・
// スパチャ読み・実況メモ）を、行の文脈まで見て判断している。後段に辞書を置くと
// 賢いほうの判断を馬鹿なほうが上書きし、実在の曲が消える（issue #11 の Week End）。
// 正規表現だけの退避経路には判断する者がいないので、そこでは辞書が要る。
//
// **production の関数をそのまま呼ぶこと。** 条件をテスト側で書き写すと、
// production を変えてもテストが通ってしまう。
func TestFilterScopeForPath(t *testing.T) {
	dict := []string{"end", "op", "待機"}
	keep := []string{"original"}

	for _, tt := range []struct {
		path     string
		wantDict bool
		reason   string
	}{
		{"grouped", false, "AI が判断済み。辞書で上書きしない"},
		{"two_stage", false, "こちらも is_song を判断している"},
		{"none", false, "候補行が 0。どちらでも結果は同じ"},
		{"regex", true, "判断する者がいないので辞書が要る"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			gotDict, gotKeep := filterScopeForPath(tt.path, dict, keep)
			if (len(gotDict) > 0) != tt.wantDict {
				t.Errorf("dict の有無 = %v, want %v（%s）", len(gotDict) > 0, tt.wantDict, tt.reason)
			}
			// keep も辞書と歩調を合わせる（filter.go は keep を数字判定より先に返すため、
			// AI 経路で keep だけ残すと曲名 "1" の行が救われてしまう）
			if (len(gotKeep) > 0) != tt.wantDict {
				t.Errorf("keep の有無 = %v, want %v", len(gotKeep) > 0, tt.wantDict)
			}
		})
	}
}

// 実際に起きた誤爆を、経路ごとの結末として固定する。
func TestWeekEndSurvivesOnAIPaths(t *testing.T) {
	weekEnd := comment.ParsedSong{Name: "Week End", OriginalComment: "0:52:18 Week End / 星野源"}
	dict := []string{"end"}

	for _, tt := range []struct {
		path     string
		wantKept bool
	}{
		{"grouped", true},
		{"two_stage", true},
		{"regex", false},
	} {
		t.Run(tt.path, func(t *testing.T) {
			d, k := filterScopeForPath(tt.path, dict, nil)
			got := comment.FilterSongsWith([]comment.ParsedSong{weekEnd}, d, k, true)
			if kept := len(got) > 0; kept != tt.wantKept {
				t.Errorf("path=%s: 残った=%v, want %v", tt.path, kept, tt.wantKept)
			}
		})
	}
}

// 構造フィルタは経路を問わず効くこと（形の判断なので AI と競合しない）。
// keep を外したので、曲名が数字だけの行も AI 経路で落ちる。
func TestStructuralFilterAppliesOnEveryPath(t *testing.T) {
	junk := []comment.ParsedSong{
		{Name: "📸", OriginalComment: "0:10 📸"},
		{Name: "1", OriginalComment: "0:10 1 / original"}, // keep 語を含むが数字だけ
	}
	for _, path := range []string{"grouped", "two_stage", "none"} {
		t.Run(path, func(t *testing.T) {
			d, k := filterScopeForPath(path, []string{"end"}, []string{"original"})
			for _, j := range junk {
				if got := comment.FilterSongsWith([]comment.ParsedSong{j}, d, k, true); len(got) > 0 {
					t.Errorf("%q が残った", j.Name)
				}
			}
		})
	}
}

// 抽出規則を変えたら版を上げること。キャッシュ鍵に混ざっているので、
// 上げ忘れると保存済みの結果が失効せず「直したはずの不具合が直らない」。
func TestExtractionRulesSaltChangesWithVersion(t *testing.T) {
	if comment.RulesVersion < 2 {
		t.Errorf("辞書の適用範囲を変えた版は 2 以上のはず（現在 %d）", comment.RulesVersion)
	}
	if got := string(extractionRulesSalt()); got == "" {
		t.Error("salt が空。キャッシュ鍵に版が混ざらない")
	}
}

// hash 補正が旧規則の抽出結果を現行版へ昇格させないこと。
//
// 昇格させると、辞書に消された曲が消えたまま固定される（issue #11）。
// 表記形式の移行は再抽出なしでできるが、**規則の版は計算し直さない限り昇格できない。**
func TestHashBackfillDoesNotPromoteStaleExtractions(t *testing.T) {
	comments := []string{"0:10 Lemon / 米津玄師", "52:18 Week End / 星野源"}
	raw, err := json.Marshal(comments)
	if err != nil {
		t.Fatal(err)
	}

	oldFormat := hashBytes(raw)         // 旧アルゴリズム（生 bytes の sha256）
	canonical := hashComments(comments) // 現行（正規化 + 規則の版）

	if oldFormat == canonical {
		t.Fatal("前提が崩れている：旧形式と現行の hash が同じ")
	}

	// 補正が旧形式を canonical へ書き換えてしまうと、この 2 つが一致してしまい
	// 以後キャッシュが命中して再抽出されない。書き換えないことを、
	// BackfillCommentSongsHashes に Migrated へ加算する経路が無いことで担保している。
	// ここでは「両者が別物である」＝版が鍵に効いていることだけを固定する。
	if !strings.Contains(string(extractionRulesSalt()), "rules=") {
		t.Error("salt に規則の版が入っていない")
	}
}

// 旧世代の hash を両方とも「再分析が要る」と数えること。
//
// 本番に実際に残っているのは v1（正規化・salt 無し）で、v0（生 bytes）は
// 02dde1d が移行済み。v0 だけを見ていると本番の該当行が「未知形式」に落ち、
// needs_reanalysis が 0 と表示されて**運用者が「再解析不要」と誤判断する**。
func TestIsLegacyExtractionHash(t *testing.T) {
	comments := []string{"0:10 Lemon / 米津玄師", "52:18 Week End / 星野源"}
	// DB から返る raw はカンマ後に空白が入りうる（正規化前）
	raw := []byte(`["0:10 Lemon / 米津玄師", "52:18 Week End / 星野源"]`)
	marshaled, err := json.Marshal(comments)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		label  string
		stored string
		want   bool
	}{
		{"v1（正規化・salt 無し。本番の実体）", hashBytes(marshaled), true},
		{"v0（生 bytes）", hashBytes(raw), true},
		{"現行 v2（salt あり）", hashComments(comments), false},
		{"無関係な値", "deadbeef", false},
		{"空", "", false},
	} {
		t.Run(tt.label, func(t *testing.T) {
			if got := isLegacyExtractionHash(tt.stored, raw); got != tt.want {
				t.Errorf("= %v, want %v", got, tt.want)
			}
		})
	}
}
