package comment

import (
	"strings"
)

// 需要過濾的關鍵字（非歌曲項目）
var filterKeywords = []string{
	// 開場/結尾
	"opening", "オープニング", "op", "intro", "イントロ",
	"ending", "エンディング", "ed", "outro", "アウトロ",
	"start", "end", "開始", "終了", "終わり",

	// 聊天/互動
	"トーク", "talk", "mc", "雑談", "おしゃべり", "chat",
	"スパチャ", "superchat", "super chat", "スーパーチャット",
	"marshmallow", "マシュマロ", "質問コーナー", "q&a",

	// 休息
	"休憩", "break", "intermission", "水分補給",

	// 通知/廣告
	"告知", "お知らせ", "notice", "announcement",
	"メン限", "member", "メンバー限定",

	// 其他
	"bgm", "se", "jingle", "ジングル",
	"待機", "waiting", "カウントダウン", "countdown",
	"テスト", "test", "チェック", "check",
	"自己紹介", "introduction",
}

// 需要保留的關鍵字（確認是歌曲）
var keepKeywords = []string{
	"cover", "カバー", "歌ってみた",
	"acoustic", "アコースティック",
	"piano", "ピアノ",
	"original", "オリジナル",
}

// FilterSongs 過濾非歌曲項目
func FilterSongs(songs []ParsedSong) []ParsedSong {
	var filtered []ParsedSong

	for _, song := range songs {
		if !shouldFilter(song) {
			filtered = append(filtered, song)
		}
	}

	return filtered
}

// shouldFilter 判斷是否應該過濾掉
func shouldFilter(song ParsedSong) bool {
	nameLower := strings.ToLower(song.Name)

	// 先檢查是否包含保留關鍵字
	for _, keyword := range keepKeywords {
		if strings.Contains(nameLower, strings.ToLower(keyword)) {
			return false
		}
	}

	// 檢查是否包含過濾關鍵字
	for _, keyword := range filterKeywords {
		if strings.Contains(nameLower, strings.ToLower(keyword)) {
			return true
		}
	}

	// 太短的名稱可能不是歌曲
	if len([]rune(song.Name)) < 2 {
		return true
	}

	// 如果只有數字，可能不是歌曲
	if isOnlyDigits(song.Name) {
		return true
	}

	return false
}

// isOnlyDigits 檢查字串是否只包含數字
func isOnlyDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// ContainsFilterKeyword 檢查是否包含過濾關鍵字（可用於 UI 警告）
func ContainsFilterKeyword(name string) bool {
	nameLower := strings.ToLower(name)
	for _, keyword := range filterKeywords {
		if strings.Contains(nameLower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}
