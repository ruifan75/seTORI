package comment

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsedSong 解析後的歌曲資訊
type ParsedSong struct {
	Start           int    // 開始秒數
	End             int    // 結束秒數（0 表示未知）
	Name            string // 歌曲名稱
	OriginalArtist  string // 原唱藝人
	OriginalComment string // 原始 comment 文本
}

// 時間戳正則表達式
var (
	// 匹配時間戳格式: HH:MM:SS, H:MM:SS, MM:SS, M:SS
	timestampRe = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})|(\d{1,2}):(\d{2})`)

	// 匹配帶有開始和結束時間的格式
	// 例如: "0:00 - 3:45 歌曲名 / 藝人" 或 "0:00;3:45 歌曲名"
	rangePatterns = []*regexp.Regexp{
		// HH:MM:SS ; HH:MM:SS 歌曲名
		regexp.MustCompile(`^(\d{1,2}:\d{2}:\d{2})\s*[;;\-–]\s*(\d{1,2}:\d{2}:\d{2})\s+(.+)$`),
		// MM:SS ; MM:SS 歌曲名
		regexp.MustCompile(`^(\d{1,2}:\d{2})\s*[;;\-–]\s*(\d{1,2}:\d{2})\s+(.+)$`),
		// HH:MM:SS - HH:MM:SS 歌曲名
		regexp.MustCompile(`^(\d{1,2}:\d{2}:\d{2})\s*[-–]\s*(\d{1,2}:\d{2}:\d{2})\s+(.+)$`),
		// MM:SS - MM:SS 歌曲名
		regexp.MustCompile(`^(\d{1,2}:\d{2})\s*[-–]\s*(\d{1,2}:\d{2})\s+(.+)$`),
	}

	// 匹配只有開始時間的格式
	// 例如: "0:00 歌曲名 / 藝人"
	startOnlyPatterns = []*regexp.Regexp{
		// HH:MM:SS 歌曲名
		regexp.MustCompile(`^(\d{1,2}:\d{2}:\d{2})\s+(.+)$`),
		// MM:SS 歌曲名
		regexp.MustCompile(`^(\d{1,2}:\d{2})\s+(.+)$`),
	}

	// 歌曲名和藝人的分隔符
	separators = []string{" / ", " - ", "／", "　/　", " ー ", "（", " ("}
)

// ParseComment 解析單行評論
func ParseComment(line string) *ParsedSong {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// 嘗試匹配帶有時間範圍的格式
	for _, pattern := range rangePatterns {
		if matches := pattern.FindStringSubmatch(line); matches != nil {
			startTime := parseTimestamp(matches[1])
			endTime := parseTimestamp(matches[2])
			songPart := strings.TrimSpace(matches[3])
			name, artist := parseSongAndArtist(songPart)

			return &ParsedSong{
				Start:           startTime,
				End:             endTime,
				Name:            name,
				OriginalArtist:  artist,
				OriginalComment: line,
			}
		}
	}

	// 嘗試匹配只有開始時間的格式
	for _, pattern := range startOnlyPatterns {
		if matches := pattern.FindStringSubmatch(line); matches != nil {
			startTime := parseTimestamp(matches[1])
			songPart := strings.TrimSpace(matches[2])
			name, artist := parseSongAndArtist(songPart)

			return &ParsedSong{
				Start:           startTime,
				End:             0, // 未知
				Name:            name,
				OriginalArtist:  artist,
				OriginalComment: line,
			}
		}
	}

	return nil
}

// ParseComments 解析多行評論
func ParseComments(comments []string) []ParsedSong {
	var songs []ParsedSong

	for _, comment := range comments {
		// 按行分割
		lines := strings.Split(comment, "\n")
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
func parseSongAndArtist(songPart string) (name, artist string) {
	// 嘗試各種分隔符
	for _, sep := range separators {
		if idx := strings.Index(songPart, sep); idx != -1 {
			name = strings.TrimSpace(songPart[:idx])
			artist = strings.TrimSpace(songPart[idx+len(sep):])

			// 如果是括號分隔，移除結尾的括號
			artist = strings.TrimSuffix(artist, ")")
			artist = strings.TrimSuffix(artist, "）")

			return name, artist
		}
	}

	// 沒有分隔符，整個都是歌名
	return strings.TrimSpace(songPart), ""
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
