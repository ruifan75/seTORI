package comment

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsedSong 解析後的歌曲資訊
type ParsedSong struct {
	Start              int    `json:"start"`                // 開始秒數
	End                int    `json:"end"`                  // 結束秒數（0 表示未知）
	Name               string `json:"name"`                 // 歌曲名稱
	OriginalArtist     string `json:"original_artist"`      // 原唱藝人
	OriginalComment    string `json:"original_comment"`     // 原始 comment 文本
	IsEndTimeEstimated bool   `json:"is_end_time_estimated"` // 結束時間是否為估計值
}

// 時間戳正則表達式
var (
	// 匹配時間戳格式: HH:MM:SS, H:MM:SS, MM:SS, M:SS
	timestampRe = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})|(\d{1,2}):(\d{2})`)

	// 行首曲序編號樣式：[01] / (1) / （1） / 01. / 01) / 01、 / ① 等
	leadingNumberRe = regexp.MustCompile(`^\s*(?:\[\s*\d{1,3}\s*\]|[(（]\s*\d{1,3}\s*[)）]|\d{1,3}\s*[.．、)）]|[\x{2460}-\x{2473}])\s*`)
)

// stripLeadingNumber 移除歌名前的曲序編號（如 [01]、01.、①）
func stripLeadingNumber(s string) string {
	return leadingNumberRe.ReplaceAllString(s, "")
}

// ParseComment 解析單行評論
func ParseComment(line string) *ParsedSong {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// 先以時間戳為主解析（不依賴分隔符）
	matchIndexes := timestampRe.FindAllStringIndex(line, -1)
	if len(matchIndexes) >= 2 {
		startTime := parseTimestamp(line[matchIndexes[0][0]:matchIndexes[0][1]])
		endTime := parseTimestamp(line[matchIndexes[1][0]:matchIndexes[1][1]])
		songPart := strings.TrimSpace(line[matchIndexes[1][1]:])
		songPart = trimLeadingSeparators(songPart)
		name, artist := parseSongAndArtist(songPart)
		if name == "" && artist == "" {
			return nil
		}
		return &ParsedSong{
			Start:              startTime,
			End:                endTime,
			Name:               name,
			OriginalArtist:     artist,
			OriginalComment:    line,
			IsEndTimeEstimated: false,
		}
	}

	if len(matchIndexes) == 1 {
		startTime := parseTimestamp(line[matchIndexes[0][0]:matchIndexes[0][1]])
		songPart := strings.TrimSpace(line[matchIndexes[0][1]:])
		songPart = trimLeadingSeparators(songPart)
		name, artist := parseSongAndArtist(songPart)
		if name == "" && artist == "" {
			return nil
		}
		return &ParsedSong{
			Start:              startTime,
			End:                0, // 未知
			Name:               name,
			OriginalArtist:     artist,
			OriginalComment:    line,
			IsEndTimeEstimated: true,
		}
	}

	return nil
}

// ParseComments 解析多行評論
func ParseComments(comments []string) []ParsedSong {
	var songs []ParsedSong

	for _, comment := range comments {
		// 按行分割，並先縫合「時間戳行 + 歌名行」的兩行式條目
		lines := stitchTwoLineEntries(strings.Split(comment, "\n"))
		for _, line := range lines {
			song := ParseComment(line)
			if song != nil {
				songs = append(songs, *song)
			}
		}
	}

	return songs
}

// parseTimestamp 解析時間戳為秒數
func parseTimestamp(ts string) int {
	parts := strings.Split(ts, ":")
	if len(parts) == 2 {
		// MM:SS
		minutes, _ := strconv.Atoi(parts[0])
		seconds, _ := strconv.Atoi(parts[1])
		return minutes*60 + seconds
	} else if len(parts) == 3 {
		// HH:MM:SS
		hours, _ := strconv.Atoi(parts[0])
		minutes, _ := strconv.Atoi(parts[1])
		seconds, _ := strconv.Atoi(parts[2])
		return hours*3600 + minutes*60 + seconds
	}
	return 0
}

// parseSongAndArtist 從歌曲部分提取歌名和藝人
// 優先以斜線（/ 或 ／）切成欄位：歌名/歌手/[作品資訊]/[年份]，取前兩欄，其餘忽略。
func parseSongAndArtist(songPart string) (name, artist string) {
	songPart = stripLeadingNumber(strings.TrimSpace(songPart))

	// 斜線分隔（半形/全形）→ 丟棄年份、作品註記等後續欄位
	normalized := strings.ReplaceAll(songPart, "／", "/")
	if strings.Contains(normalized, "/") {
		fields := strings.Split(normalized, "/")
		name = strings.TrimSpace(fields[0])
		if len(fields) > 1 {
			artist = strings.TrimSpace(fields[1])
		}
		return name, artist
	}

	// 其他分隔符（破折號、括號）
	for _, sep := range []string{" - ", " ー ", "（", " ("} {
		if idx := strings.Index(songPart, sep); idx != -1 {
			name = strings.TrimSpace(songPart[:idx])
			artist = strings.TrimSpace(songPart[idx+len(sep):])
			artist = strings.TrimSuffix(artist, ")")
			artist = strings.TrimSuffix(artist, "）")
			return name, artist
		}
	}

	// 沒有分隔符，整個都是歌名
	return strings.TrimSpace(songPart), ""
}

func trimLeadingSeparators(text string) string {
	return strings.TrimLeft(text, " \t;:：-–→~〜～")
}

// FormatTimestamp 將秒數格式化為時間戳
func FormatTimestamp(seconds int) string {
	if seconds < 0 {
		return "0:00"
	}

	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return strconv.Itoa(hours) + ":" + padZero(minutes) + ":" + padZero(secs)
	}
	return strconv.Itoa(minutes) + ":" + padZero(secs)
}

func padZero(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
