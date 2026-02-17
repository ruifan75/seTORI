package comment

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ruifan75/setori/pkg/ai"
)

// aiLineSelection AI 回傳的行選擇結果（只包含索引和時間，不包含日文文字）
type aiLineSelection struct {
	LineIndex int             `json:"line"`  // 1-based index of the input line
	Start     json.RawMessage `json:"start"` // start seconds
	End       json.RawMessage `json:"end"`   // end seconds (0 if unknown)
	IsSong    bool            `json:"is_song"`
}

const commentAISystemPrompt = `あなたはYouTubeコメントから歌枠のタイムスタンプ行を選別するアシスタントです。

入力は番号付きのコメント行です。以下のルールでJSON配列のみを返してください:
- 出力はJSON配列のみ（説明文やコードブロックは禁止）
- 各要素は {"line":行番号,"start":開始秒数,"end":終了秒数,"is_song":true/false}
- line は入力の行番号（1始まり）をそのまま返す
- start/end は秒数の整数。end が不明な場合は 0
- 1行に複数のタイムスタンプがあれば最初を start、2つ目を end とする
- is_song: その行が歌曲であれば true、雑談・告知・開始・終了などは false
- 日本語テキストは一切出力しないでください。行番号と数値のみ返してください
`

var aiTimestampRe = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2})|(\d{1,2}:\d{2})`)

// ParseCommentsWithAI uses Groq to select and deduplicate song lines from raw comments.
// AI only returns line indices; actual text is extracted from original lines via regex.
func ParseCommentsWithAI(aiClient *ai.Client, comments []string) ([]ParsedSong, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}

	lines := extractTimestampLines(comments)
	if len(lines) == 0 {
		return nil, fmt.Errorf("no timestamp lines")
	}

	userMessage := "以下のコメント行から歌のタイムスタンプ行を選別してください。\n\n"
	for i, line := range lines {
		userMessage += fmt.Sprintf("%d) %s\n", i+1, line)
	}

	response, err := aiClient.SimpleChat(commentAISystemPrompt, userMessage)
	if err != nil {
		return nil, err
	}

	selections, err := parseAILineSelections(response)
	if err != nil {
		return nil, err
	}

	// Use AI selections to pick lines, then parse text with regex
	result := make([]ParsedSong, 0, len(selections))
	for _, sel := range selections {
		if !sel.IsSong {
			continue
		}
		if sel.LineIndex < 1 || sel.LineIndex > len(lines) {
			continue
		}

		originalLine := lines[sel.LineIndex-1]

		// Parse the original line with regex to extract song name and artist
		parsed := ParseComment(originalLine)
		if parsed == nil {
			continue
		}

		// Use AI's time if provided, otherwise use regex-parsed time
		start := parseAISeconds(sel.Start)
		end := parseAISeconds(sel.End)
		if start > 0 {
			parsed.Start = start
		}
		if end > 0 {
			parsed.End = end
			parsed.IsEndTimeEstimated = false
		}

		result = append(result, *parsed)
	}

	return result, nil
}

func extractTimestampLines(comments []string) []string {
	var lines []string
	for _, comment := range comments {
		for _, line := range strings.Split(comment, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if aiTimestampRe.MatchString(trimmed) {
				lines = append(lines, trimmed)
			}
		}
	}
	return lines
}

func parseAILineSelections(response string) ([]aiLineSelection, error) {
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var items []aiLineSelection
	if err := json.Unmarshal([]byte(response), &items); err != nil {
		return nil, fmt.Errorf("unmarshal AI response: %w", err)
	}
	return items, nil
}

func parseAISeconds(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	// Try number
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		if num < 0 {
			return 0
		}
		return int(num)
	}
	// Try string timestamp
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return parseTimestampString(str)
	}
	return 0
}

func parseTimestampString(ts string) int {
	ts = strings.TrimSpace(ts)
	parts := strings.Split(ts, ":")
	if len(parts) == 2 {
		minutes := parseInt(parts[0])
		seconds := parseInt(parts[1])
		return minutes*60 + seconds
	}
	if len(parts) == 3 {
		hours := parseInt(parts[0])
		minutes := parseInt(parts[1])
		seconds := parseInt(parts[2])
		return hours*3600 + minutes*60 + seconds
	}
	return 0
}

func parseInt(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			continue
		}
		n = n*10 + int(r-'0')
	}
	return n
}
