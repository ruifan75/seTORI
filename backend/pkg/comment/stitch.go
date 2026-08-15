package comment

import (
	"regexp"
	"strings"
)

// 2 行形式のセットリストを結合する前処理。
// コメントによってはタイムスタンプと曲名が 2 行に分かれている：
//
//	17:51    with 水科葵
//	セブンティーン / YOASOBI
//
// 行単位で解析するとタイムスタンプのない曲名行が捨てられるため、正規表現／AI 解析の前に、
// 「タイムスタンプ行（後ろはコラボ相手の情報または空白のみ）+ 直後のタイムスタンプなし行」を 1 行にまとめる。
// 結合時は曲名行をタイムスタンプの後ろへ置き、コラボ相手の情報を最後の / フィールドへ移す。
// これにより parseSongAndArtist の「先頭 2 フィールドを使う」規則で曲名と歌手を正しく解析できる。

// collabMarkerRe は「残りの文字列全体がコラボ相手の情報」であることを示す先頭マーカー（with ○○、feat. ○○ など）に一致する。
var collabMarkerRe = regexp.MustCompile(`(?i)^(?:with\b|w/|feat(?:\.|\b)|ft(?:\.|\b)|[×✕✖]|コラボ|ゲスト)`)

// stitchMinTimestampLines はコメント内のタイムスタンプ付き行がこの数以上ならセットリストとみなし、結合を有効にする。
// 「12:08 13:17 + 次の行に歌詞の引用」のような単発の感想コメントが誤って楽曲に結合されるのを防ぐ。
const stitchMinTimestampLines = 3

// stitchTwoLineEntries は 2 行形式の楽曲項目を 1 行にまとめ、その他の行はそのまま残す。
// 入力は一つのコメントを行分割した結果で、結合の閾値もコメント単位で計算する。
func stitchTwoLineEntries(lines []string) []string {
	if countTimestampLines(lines) < stitchMinTimestampLines {
		return lines
	}

	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		tsEnd := lastTimestampEnd(line)
		if tsEnd < 0 {
			out = append(out, lines[i])
			continue
		}

		remainder := trimLeadingSeparators(strings.TrimSpace(line[tsEnd:]))
		collabOnly := remainder != "" && collabMarkerRe.MatchString(remainder)
		if remainder != "" && !collabOnly {
			// タイムスタンプの後ろに曲名があれば通常の 1 行形式
			out = append(out, lines[i])
			continue
		}

		// 直後の行が空でなくタイムスタンプもない場合だけ結合し、空行をまたいで無関係な文を拾わない
		if i+1 >= len(lines) {
			out = append(out, lines[i])
			continue
		}
		next := strings.TrimSpace(lines[i+1])
		if next == "" || timestampRe.MatchString(next) {
			out = append(out, lines[i])
			continue
		}

		stitched := strings.TrimSpace(line[:tsEnd]) + " " + next
		if collabOnly {
			stitched += " / " + remainder
		}
		out = append(out, stitched)
		i++
	}
	return out
}

func countTimestampLines(lines []string) int {
	n := 0
	for _, line := range lines {
		if timestampRe.MatchString(line) {
			n++
		}
	}
	return n
}

// lastTimestampEnd は行内で最後のタイムスタンプが終わる位置を返し、なければ -1 を返す。
func lastTimestampEnd(line string) int {
	locs := timestampRe.FindAllStringIndex(line, -1)
	if len(locs) == 0 {
		return -1
	}
	return locs[len(locs)-1][1]
}
