package comment

import (
	"regexp"
	"strings"
)

// 兩行式セトリの縫合前處理。
// 有些留言會把時間戳和歌名拆成兩行：
//
//	17:51    with 水科葵
//	セブンティーン / YOASOBI
//
// 逐行解析會把沒有時間戳的歌名行丟掉，所以在進入正則/AI 解析之前，
// 先把「時間戳行（後面只有合唱者資訊或空白）+ 緊接的無時間戳行」併成一行。
// 合併時把歌名行放在時間戳後面、合唱者資訊移到最後一個 / 欄位，
// 讓 parseSongAndArtist 的「取前兩欄」規則能直接解出正確的歌名/歌手。

// collabMarkerRe 匹配「整段剩餘文字是合唱者資訊」的開頭標記（with ○○、feat. ○○ 等）
var collabMarkerRe = regexp.MustCompile(`(?i)^(?:with\b|w/|feat(?:\.|\b)|ft(?:\.|\b)|[×✕✖]|コラボ|ゲスト)`)

// stitchMinTimestampLines 留言內含時間戳的行數達到此值才視為セトリ並啟用縫合。
// 避免「12:08 13:17 + 下一行歌詞引用」之類的單則感想留言被誤併成假歌曲。
const stitchMinTimestampLines = 3

// stitchTwoLineEntries 將兩行式歌曲條目併成一行，其他行原樣保留。
// 輸入是單一留言拆行後的結果（縫合門檻以單一留言為單位計算）。
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
			// 時間戳後已有歌名 → 一般的一行式條目
			out = append(out, lines[i])
			continue
		}

		// 只在「緊接的下一行」非空且沒有時間戳時合併，避免跨過空行誤抓無關文字
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

// lastTimestampEnd 回傳行內最後一個時間戳結束的位置，沒有時間戳則回傳 -1
func lastTimestampEnd(line string) int {
	locs := timestampRe.FindAllStringIndex(line, -1)
	if len(locs) == 0 {
		return -1
	}
	return locs[len(locs)-1][1]
}
