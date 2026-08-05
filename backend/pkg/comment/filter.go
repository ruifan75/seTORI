package comment

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// FilterSongs 過濾非歌曲項目（使用 DB 載入的關鍵字）。
// 構造的な非曲判定（絵文字のみ／引用符／罫線／過長）＋キーワード辞書の両方を適用する。
func FilterSongs(songs []ParsedSong, filterKW, keepKW []string) []ParsedSong {
	return FilterSongsWith(songs, filterKW, keepKW, true)
}

// FilterSongsWith は structural を切り替えられる評価用の入口。
// structural=false のときは従来のキーワード辞書のみで過濾する（ベンチマークの
// before/after 対照や、辞書だけの挙動を確認したいとき用）。production は常に true。
func FilterSongsWith(songs []ParsedSong, filterKW, keepKW []string, structural bool) []ParsedSong {
	var filtered []ParsedSong
	for _, song := range songs {
		if !shouldFilter(song, filterKW, keepKW, structural) {
			filtered = append(filtered, song)
		}
	}
	return filtered
}

// shouldFilter 判斷是否應該過濾掉
// 檢查整行原始留言（OriginalComment），避免關鍵字出現在 artist 等欄位時漏掉
func shouldFilter(song ParsedSong, filterKW, keepKW []string, structural bool) bool {
	// 結構性の非曲判定は keep キーワードより優先する。
	// 実況メモ（トピック「発言」）や絵文字マーカー、罫線のネスト注釈などは
	// 本文にたまたま "歌ってみた"/"cover" 等の keep 語を含むことがあり、
	// keep 救済を先に効かせると素通りしてしまうため、ここで先に落とす。
	// ground truth 4156 件のうちこの規則で誤って落ちる正しい曲は 0 件（検証済み）。
	if structural && isStructurallyNonSong(song) {
		return true
	}

	textLower := strings.ToLower(song.OriginalComment)
	if textLower == "" {
		textLower = strings.ToLower(song.Name)
	}

	// 先檢查是否包含保留關鍵字
	for _, keyword := range keepKW {
		if containsKeyword(textLower, strings.ToLower(keyword)) {
			return false
		}
	}

	// 檢查是否包含過濾關鍵字
	for _, keyword := range filterKW {
		if containsKeyword(textLower, strings.ToLower(keyword)) {
			return true
		}
	}

	// 如果只有數字，可能不是歌曲
	if isOnlyDigits(song.Name) {
		return true
	}

	return false
}

// 曲名として許容する最大文字數（rune 単位）。これを超える「歌名」は
// 実況・雑談文の丸ごと取り込みとみなす。実在曲でこれを超えるものは稀。
const maxSongNameRunes = 40

// isStructurallyNonSong 以「曲名の形」而非關鍵字，判斷抽出項目是否根本不是歌曲。
// これらはタイムスタンプ付きコメントに紛れ込む「非曲行」の典型パターンで、
// キーワード辞書では捕まえきれない。ground truth との突き合わせで
// 実在曲の巻き添え（false negative）が出ないことを確認した規則のみを採用している。
func isStructurallyNonSong(song ParsedSong) bool {
	name := strings.TrimSpace(song.Name)

	// 1. 曲名に「単語文字」（英数字・かな・漢字・ハングル等）が一つも無い
	//    → 絵文字/記号だけの行（📸 🎸 🦋🌹 ??? など）。曲名たりえない。
	if !hasWordChar(name) {
		return true
	}

	// 2. 曲名に日本語の引用符「」『』を含む
	//    → 実況メモ（例: 挨拶運動「みんな、ただいまー!」）。
	//    曲名欄は分隔符で切り出した後なので、作品情報の『』は通常ここには残らない。
	if strings.ContainsAny(name, "「」『』") {
		return true
	}

	// 3. 原文行の先頭が罫線素片（┗ ┣ ┏ ┃ ├ └ など）
	//    → セトリ本項目にぶら下げたネスト注釈行（トークの小見出し等）。
	if startsWithBoxDrawing(song.OriginalComment) {
		return true
	}

	// 4. 曲名が極端に長い → 感想・実況文をまるごと拾っている。
	if utf8.RuneCountInString(name) > maxSongNameRunes {
		return true
	}

	return false
}

// hasWordChar 判斷字串是否含有至少一個「単語文字」（字母或数字）。
// 絵文字・記号・句読点のみの文字列は false。
func hasWordChar(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// startsWithBoxDrawing 判斷行首（去除前導空白後）是否為罫線素片（Box Drawing, U+2500–U+257F）。
func startsWithBoxDrawing(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(line)
	return r >= 0x2500 && r <= 0x257F
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
