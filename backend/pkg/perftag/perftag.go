// Package perftag は演奏バージョンタグ（short / piano / acappella …）の語彙を持つ。
//
// **コメント経路と Holodex 経路の両方が通る。** 元はコメント側の AI 応答を
// 濾すためだけに書かれていたので、Holodex から来た曲にはタグの寄せも
// 原文からの補完も効かず、`そばかす (1 Chorus)` が Short Ver. にならなかった。
// 語彙を 2 か所に持つと必ず片方だけ直されるので、ここへ出してある。
package perftag

import (
	"strings"

	"github.com/ruifan75/setori/internal/logger"
)

// tagSynonyms は「同じ演奏バージョンを指す別の言い方」を正規のタグへ寄せる。
//
// AI は原文にある語をそのままタグにしがちで、`1chorus` のような書き方は
// allowedTags に無いので黙って捨てられていた。捨てるとタグが付かないだけでなく、
// **その情報が二度と復元できない**（正規化後の曲名からは既に削られている）。
var tagSynonyms = map[string]string{
	"1chorus":   "short",
	"1coruhs":   "short", // 実データにある綴り間違い
	"onechorus": "short",
	"1コーラス":     "short",
	"ワンコーラス":    "short",
	"1番のみ":      "short",
	"ショート":      "short",
	"フル":        "full",
	"アカペラ":      "acappella",
	"ピアノ":       "piano",
	"アコースティック":  "acoustic",
	"メドレー":      "medley",
}

// shortMarkers は原文の曲名から short を導ける語。
// AI がタグを付け忘れても、原文に書いてあれば拾えるようにしておく。
var shortMarkers = []string{"1chorus", "1 chorus", "1コーラス", "ワンコーラス", "1番のみ", "short ver", "ショートver"}

// allowedTags は正規化で許可する演奏バージョンタグ。
// AI が語彙外の値を返しても DB を汚さないよう、Go 側で必ず濾す。
var allowedTags = map[string]bool{
	"acoustic":  true,
	"piano":     true,
	"弾き語り":      true,
	"acappella": true,
	"short":     true,
	"full":      true,
	"medley":    true,
}

// normalizeTags は AI が返したタグを正規の語彙へ寄せ、
// 原文の曲名からも導けるタグを補う。
func Normalize(tags []string, verbatim string) []string {
	out := filterAllowedTags(tags)

	// 原文に 1chorus 等が書かれていれば short を補う。
	// 正規化後の曲名からは削られているので、ここで拾わないと失われる。
	low := strings.ToLower(verbatim)
	for _, m := range shortMarkers {
		if !strings.Contains(low, m) {
			continue
		}
		for _, t := range out {
			if t == "short" {
				return out
			}
		}
		return append(out, "short")
	}
	return out
}

func filterAllowedTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	var out []string
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		// 別の言い方は正規のタグへ寄せる（捨てると情報が復元できない）
		if canon, ok := tagSynonyms[strings.ToLower(t)]; ok {
			t = canon
		}
		if seen[t] {
			continue
		}
		if !allowedTags[t] {
			logger.Debugf("perftag: 語彙外のタグを落とす %q", t)
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
