package comment

import (
	"strings"
	"unicode"
)

// FilterSongs 過濾非歌曲項目（使用 DB 載入的關鍵字）
func FilterSongs(songs []ParsedSong, filterKW, keepKW []string) []ParsedSong {
	var filtered []ParsedSong

	for _, song := range songs {
		if !shouldFilter(song, filterKW, keepKW) {
			filtered = append(filtered, song)
		}
	}

	return filtered
}

// shouldFilter 判斷是否應該過濾掉
func shouldFilter(song ParsedSong, filterKW, keepKW []string) bool {
	nameLower := strings.ToLower(song.Name)

	// 先檢查是否包含保留關鍵字
	for _, keyword := range keepKW {
		if containsKeyword(nameLower, strings.ToLower(keyword)) {
			return false
		}
	}

	// 檢查是否包含過濾關鍵字
	for _, keyword := range filterKW {
		if containsKeyword(nameLower, strings.ToLower(keyword)) {
			return true
		}
	}

	// 如果只有數字，可能不是歌曲
	if isOnlyDigits(song.Name) {
		return true
	}

	return false
}

// containsKeyword 檢查 text 是否包含 keyword
// 短 keyword（ASCII 3字以下）使用全字比對，避免 "op" 匹配到 "shop"
func containsKeyword(text, keyword string) bool {
	kwRunes := []rune(keyword)

	// 短 ASCII keyword → 全字比對（前後必須是非英數字元或邊界）
	if len(kwRunes) <= 3 && isAllASCIIAlpha(keyword) {
		return containsWholeWord(text, keyword)
	}

	// 長 keyword 或非 ASCII → 子字串比對
	return strings.Contains(text, keyword)
}

// containsWholeWord 全字比對：keyword 前後必須是邊界或非英數字元
func containsWholeWord(text, keyword string) bool {
	textRunes := []rune(text)
	kwRunes := []rune(keyword)
	kwLen := len(kwRunes)

	for i := 0; i <= len(textRunes)-kwLen; i++ {
		// 比對 keyword
		match := true
		for j := 0; j < kwLen; j++ {
			if textRunes[i+j] != kwRunes[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}

		// 檢查前後邊界
		beforeOK := i == 0 || !isWordChar(textRunes[i-1])
		afterOK := i+kwLen >= len(textRunes) || !isWordChar(textRunes[i+kwLen])

		if beforeOK && afterOK {
			return true
		}
	}

	return false
}

// isWordChar 判斷是否為「單字內字元」（英數字）
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) && r < 0x3000 || unicode.IsDigit(r)
}

// isAllASCIIAlpha 判斷是否全為 ASCII 英文字母
func isAllASCIIAlpha(s string) bool {
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return len(s) > 0
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
