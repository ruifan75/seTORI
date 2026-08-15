package comment

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// FilterSongs は DB から読み込んだキーワードで楽曲以外の項目を除外する。
// 構造的な非曲判定（絵文字のみ／引用符／罫線／過長）＋キーワード辞書の両方を適用する。
func FilterSongs(songs []ParsedSong, filterKW, keepKW []string) []ParsedSong {
	return FilterSongsWith(songs, filterKW, keepKW, true)
}

// FilterSongsWith は structural を切り替えられる評価用の入口。
// structural=false のときは従来のキーワード辞書だけで除外する（ベンチマークの
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

// shouldFilter は項目を除外すべきか判定する。
// キーワードが artist などのフィールドにある場合も見落とさないよう、元コメント（OriginalComment）の行全体を調べる。
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

	// 先に除外対象外のキーワードを含むか確認する
	for _, keyword := range keepKW {
		if containsKeyword(textLower, strings.ToLower(keyword)) {
			return false
		}
	}

	// 除外キーワードを含むか確認する
	for _, keyword := range filterKW {
		if containsKeyword(textLower, strings.ToLower(keyword)) {
			return true
		}
	}

	// 数字だけなら楽曲ではない可能性が高い
	if isOnlyDigits(song.Name) {
		return true
	}

	return false
}

// 曲名として許容する最大文字数（rune 単位）。これを超える「曲名」は
// 実況・雑談文の丸ごと取り込みとみなす。実在曲でこれを超えるものは稀。
const maxSongNameRunes = 40

// isStructurallyNonSong はキーワードではなく「曲名の形」から、抽出項目がそもそも楽曲ではないか判定する。
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
	//    曲名欄は区切り文字で切り出した後なので、作品情報の『』は通常ここには残らない。
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

	// 5. 全体が丸括弧で囲まれている → 実況の合いの手（例: (ここの揺れだいすき)）。
	if wrappedInParens(name) {
		return true
	}

	// 6. 末尾以外に句点がある → 文が 2 つ以上ある＝地の文。
	//    末尾の句点は落とさない。『らしく。』のように句点で終わる曲名は実在する。
	if hasInnerPeriod(name) {
		return true
	}

	// 7. 見出しの名詞化語尾で終わる → セトリではなく配信の目次
	//    （例: ぱんt警察の件 / 着せ替え大狂いの巻 / 10万人達成の瞬間）。
	if endsWithHeadingSuffix(name) {
		return true
	}

	return false
}

// 曲名として長さで落とす閾値を下げてはいけない。実在曲は 30 文字に達する
// （`One more time, One more chance`、`Crazy Party Night 〜ぱんぷきんの逆襲〜`）。
// 同じ理由で「読点を含む」「よ/ね/わ で終わる」「です/ます で終わる」も採れない
// ── `琥珀色の街、上海蟹の朝`、`死ぬのがいいわ`、`恋?で愛?で暴君です!` がすべて実在曲で、
// 本番 819 曲に対して測ると巻き添えが出る。下の規則は巻き添え 0 件を確認したものだけ。

// wrappedInParens は文字列全体が丸括弧で囲まれているか。
func wrappedInParens(s string) bool {
	return (strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")")) ||
		(strings.HasPrefix(s, "（") && strings.HasSuffix(s, "）"))
}

// hasInnerPeriod は末尾以外の位置に句点があるか。
func hasInnerPeriod(s string) bool {
	s = trimSentenceEnd(s)
	if s == "" {
		return false
	}
	r := []rune(s)
	return strings.ContainsRune(string(r[:len(r)-1]), '。')
}

// headingSuffixes は「〜という出来事」を指す名詞化語尾。曲名には使われない。
//
// `の話` だけは将来 実在曲とぶつかりうる（本番 819 曲では 0 件）。
// 巻き添えが出たらここから外すこと。
var headingSuffixes = []string{"の件", "の話", "の巻", "の瞬間", "の経緯", "旨"}

func endsWithHeadingSuffix(s string) bool {
	s = trimSentenceEnd(s)
	for _, suf := range headingSuffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}

// trimSentenceEnd は末尾の感嘆符・疑問符・空白を落とす（`〜の件!` を `〜の件` として見る）。
func trimSentenceEnd(s string) string {
	return strings.TrimRight(s, "!！?？ 　")
}

// hasWordChar は文字列に「単語文字」（文字または数字）が一つ以上あるか判定する。
// 絵文字・記号・句読点のみの文字列は false。
func hasWordChar(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// startsWithBoxDrawing は先頭の空白を除いた行頭が罫線素片（Box Drawing, U+2500–U+257F）か判定する。
func startsWithBoxDrawing(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(line)
	return r >= 0x2500 && r <= 0x257F
}

// containsKeyword は text が keyword を含むか確認する。
// 短い keyword（ASCII 3 文字以下）は単語全体で比較し、"op" が "shop" に一致するのを防ぐ。
func containsKeyword(text, keyword string) bool {
	kwRunes := []rune(keyword)

	// 短い ASCII keyword → 単語全体で比較（前後が英数字以外または境界であること）
	if len(kwRunes) <= 3 && isAllASCIIAlpha(keyword) {
		return containsWholeWord(text, keyword)
	}

	// 長い keyword または非 ASCII → 部分文字列で比較
	return strings.Contains(text, keyword)
}

// containsWholeWord は単語全体を比較する。keyword の前後は境界または英数字以外でなければならない。
func containsWholeWord(text, keyword string) bool {
	textRunes := []rune(text)
	kwRunes := []rune(keyword)
	kwLen := len(kwRunes)

	for i := 0; i <= len(textRunes)-kwLen; i++ {
		// keyword を比較する
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

		// 前後の境界を確認する
		beforeOK := i == 0 || !isWordChar(textRunes[i-1])
		afterOK := i+kwLen >= len(textRunes) || !isWordChar(textRunes[i+kwLen])

		if beforeOK && afterOK {
			return true
		}
	}

	return false
}

// isWordChar は「単語内の文字」（英数字）か判定する。
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) && r < 0x3000 || unicode.IsDigit(r)
}

// isAllASCIIAlpha はすべて ASCII 英字か判定する。
func isAllASCIIAlpha(s string) bool {
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return len(s) > 0
}

// isOnlyDigits は文字列が数字だけか確認する。
func isOnlyDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
