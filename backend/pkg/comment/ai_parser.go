package comment

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ruifan75/setori/pkg/ai"
)

type aiCommentSong struct {
	Start           json.RawMessage `json:"start"`
	End             json.RawMessage `json:"end"`
	Name            string          `json:"name"`
	OriginalArtist  string          `json:"original_artist"`
	OriginalComment string          `json:"original_comment"`
}

const commentAISystemPrompt = `あなたはYouTubeコメントから歌枠のタイムスタンプを抽出するアシスタントです。

以下のルールでJSON配列のみを返してください:
- 出力はJSON配列のみ（説明文やコードブロックは禁止）
- 各要素は {"start":秒数,"end":秒数,"name":"曲名","original_artist":"原曲アーティスト","original_comment":"元の行"}
- start/end は秒数の整数。end が不明な場合は 0。
- 1行に複数のタイムスタンプがあれば最初を start、2つ目を end とする
- 曲名やアーティストが不明な場合は空文字
- 歌曲以外（雑談/告知など）は可能なら除外
- 複数のコメントに同じ曲が含まれる場合は重複を除外し、1曲につき1エントリのみ返す
- 重複判定: 開始時間が近く（30秒以内）曲名が類似していれば同一曲とみなす
- 重複がある場合、start と end の両方がある方を優先する。次にアーティスト情報がある方を優先する
`

var aiTimestampRe = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2})|(\d{1,2}:\d{2})`)

// ParseCommentsWithAI uses Groq to extract song list from raw comments.
func ParseCommentsWithAI(aiClient *ai.Client, comments []string) ([]ParsedSong, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}

	lines := extractTimestampLines(comments)
	if len(lines) == 0 {
		return nil, fmt.Errorf("no timestamp lines")
	}

	userMessage := "以下のコメント行から歌のタイムスタンプを抽出してください。\n\n"
	for i, line := range lines {
		userMessage += fmt.Sprintf("%d) %s\n", i+1, line)
	}

	response, err := aiClient.SimpleChat(commentAISystemPrompt, userMessage)
	if err != nil {
		return nil, err
	}

	parsed, err := parseAICommentResponse(response)
	if err != nil {
		return nil, err
	}

	result := make([]ParsedSong, 0, len(parsed))
	for _, item := range parsed {
		start := parseAISeconds(item.Start)
		end := parseAISeconds(item.End)
		name := strings.TrimSpace(item.Name)
		artist := strings.TrimSpace(item.OriginalArtist)
		if name == "" && artist == "" {
			continue
		}
		result = append(result, ParsedSong{
			Start:              start,
			End:                end,
			Name:               name,
			OriginalArtist:     artist,
			OriginalComment:    strings.TrimSpace(item.OriginalComment),
			IsEndTimeEstimated: end == 0,
		})
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

func parseAICommentResponse(response string) ([]aiCommentSong, error) {
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var items []aiCommentSong
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
