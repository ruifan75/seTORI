package comment

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsedSong は解析後の楽曲情報。
type ParsedSong struct {
	Start              int    `json:"start"`                 // 開始秒数
	End                int    `json:"end"`                   // 終了秒数（0 は不明）
	Name               string `json:"name"`                  // 曲名（原文に現れる形のまま）
	OriginalArtist     string `json:"original_artist"`       // 原曲アーティスト（原文のまま）
	OriginalComment    string `json:"original_comment"`      // 元のコメント本文
	IsEndTimeEstimated bool   `json:"is_end_time_estimated"` // 終了時刻が推定値か

	// 以下は抽出と正規化を 1 回の AI 呼び出しで行う経路（ParseAndNormalizeWithAI）でのみ埋まる。
	// 2 段階の経路では空のままで、後段の BatchAINormalization が担当する。
	NormalizedName          string   `json:"normalized_name,omitempty"`
	NormalizedNameReading   string   `json:"normalized_name_reading,omitempty"`
	NormalizedArtist        string   `json:"normalized_artist,omitempty"`
	NormalizedArtistReading string   `json:"normalized_artist_reading,omitempty"`
	Tags                    []string `json:"tags,omitempty"`
	Confidence              float64  `json:"confidence,omitempty"`
}

// タイムスタンプの正規表現
var (
	// タイムスタンプ形式に一致：HH:MM:SS, H:MM:SS, MM:SS, M:SS
	timestampRe = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})|(\d{1,2}):(\d{2})`)

	// 行頭の曲番号形式：[01] / (1) / （1） / 01. / 01) / 01、 / ① など
	leadingNumberRe = regexp.MustCompile(`^\s*(?:\[\s*\d{1,3}\s*\]|[(（]\s*\d{1,3}\s*[)）]|\d{1,3}\s*[.．、)）]|[\x{2460}-\x{2473}])\s*`)
)

// stripLeadingNumber は曲名の前にある曲番号（[01]、01.、① など）を取り除く。
func stripLeadingNumber(s string) string {
	return leadingNumberRe.ReplaceAllString(s, "")
}

// ParseComment はコメントを 1 行解析する。
func ParseComment(line string) *ParsedSong {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// まずタイムスタンプを軸に解析する（区切り文字には依存しない）
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

// ParseComments は複数行のコメントを解析する。
func ParseComments(comments []string) []ParsedSong {
	var songs []ParsedSong

	for _, comment := range comments {
		// 行ごとに分割し、先に「タイムスタンプ行 + 曲名行」の 2 行形式を結合する
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

// parseTimestamp はタイムスタンプを秒数に変換する。
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

// parseSongAndArtist は楽曲部分から曲名とアーティストを取り出す。
// スラッシュ（/ または ／）を優先して「曲名/歌手/[作品情報]/[年]」のフィールドに分け、先頭の二つだけを使う。
func parseSongAndArtist(songPart string) (name, artist string) {
	songPart = stripLeadingNumber(strings.TrimSpace(songPart))

	// スラッシュ区切り（半角／全角）では年や作品注記など後続フィールドを捨てる
	normalized := strings.ReplaceAll(songPart, "／", "/")
	if strings.Contains(normalized, "/") {
		fields := strings.Split(normalized, "/")
		name = strings.TrimSpace(fields[0])
		if len(fields) > 1 {
			artist = strings.TrimSpace(fields[1])
		}
		return name, artist
	}

	// その他の区切り文字（ダッシュ、括弧）
	for _, sep := range []string{" - ", " ー ", "（", " ("} {
		if idx := strings.Index(songPart, sep); idx != -1 {
			name = strings.TrimSpace(songPart[:idx])
			artist = strings.TrimSpace(songPart[idx+len(sep):])
			artist = strings.TrimSuffix(artist, ")")
			artist = strings.TrimSuffix(artist, "）")
			return name, artist
		}
	}

	// 区切り文字がなければ全体を曲名として扱う
	return strings.TrimSpace(songPart), ""
}

func trimLeadingSeparators(text string) string {
	return strings.TrimLeft(text, " \t;:：-–→~〜～")
}

// FormatTimestamp は秒数をタイムスタンプ形式に整える。
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
