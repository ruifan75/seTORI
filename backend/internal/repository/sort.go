package repository

import (
	"fmt"
	"strings"
	"unicode"
)

// 名前ソートの共通定義。
// 並び順：先頭文字グループ（数字 → 英字 → その他）→ 読みベースのキー → 名前。
// キーは読み（無ければ名前）を小文字化し、片仮名を平仮名に変換して比較する。
// これにより片仮名読み・漢字名（読み設定済み）も五十音順に揃う。

const katakanaChars = "ァアィイゥウェエォオカガキギクグケゲコゴサザシジスズセゼソゾタダチヂッツヅテデトドナニヌネノハバパヒビピフブプヘベペホボポマミムメモャヤュユョヨラリルレロヮワヰヱヲンヴヵヶ"
const hiraganaChars = "ぁあぃいぅうぇえぉおかがきぎくぐけげこごさざしじすずせぜそぞただちぢっつづてでとどなにぬねのはばぱひびぴふぶぷへべぺほぼぽまみむめもゃやゅゆょよらりるれろゎわゐゑをんゔゕゖ"

// normDir はユーザー入力の並び方向を "ASC" / "DESC" に正規化する（既定 ASC）。
// SQL に直接埋め込むため、必ずこの関数を通して安全な定数だけを使う。
func normDir(dir string) string {
	if strings.EqualFold(dir, "desc") {
		return "DESC"
	}
	return "ASC"
}

// dirOr は dir が空なら fallback を、指定があればそれを正規化して返す。
// 列ごとの「自然な既定方向」（歌唱回数・日付は降順など）を保つために使う。
func dirOr(dir, fallback string) string {
	if dir == "" {
		return normDir(fallback)
	}
	return normDir(dir)
}

// nameSortOrder は「数字 → 英字 → その他（読み順）」で並べる ORDER BY 句の中身を返す（昇順）。
func nameSortOrder(nameCol, readingCol string) string {
	return nameSortOrderDir(nameCol, readingCol, "asc")
}

// nameSortOrderDir は nameSortOrder に方向を適用した版。
// DESC 指定時は各キー（先頭文字グループ・読みキー・名前）すべてを反転し、五十音順を丸ごと逆順にする。
// nameCol / readingCol は SQL 内で安全な識別子のみ渡すこと。dir は normDir で無害化する。
func nameSortOrderDir(nameCol, readingCol, dir string) string {
	d := normDir(dir)
	return fmt.Sprintf(`
		CASE
			WHEN %[1]s ~ '^[0-9０-９]' THEN 0
			WHEN %[1]s ~ '^[A-Za-zＡ-Ｚａ-ｚ]' THEN 1
			ELSE 2
		END %[5]s,
		translate(lower(coalesce(nullif(%[2]s, ''), %[1]s)), '%[3]s', '%[4]s') %[5]s,
		%[1]s %[5]s`, nameCol, readingCol, katakanaChars, hiraganaChars, d)
}

// ContainsKatakana は文字列に片仮名が含まれるかを返す。
// 読みは平仮名で統一する方針のため、片仮名が残る読みは「要修正」とみなす。
func ContainsKatakana(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Katakana) {
			return true
		}
	}
	return false
}

// KataToHira は片仮名を平仮名に変換する（長音符・他の文字はそのまま）。
// 読みデータの取り込み時に平仮名へ正規化するために使う。
func KataToHira(s string) string {
	var b strings.Builder
	for _, r := range s {
		// ァ(U+30A1)〜ヶ(U+30F6) は -0x60 で ぁ〜ゖ に対応する
		if r >= 'ァ' && r <= 'ヶ' {
			r -= 0x60
		}
		b.WriteRune(r)
	}
	return b.String()
}
