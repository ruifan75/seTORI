package comment

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/util"
)

// aiLineSelection AI 回傳的單行解析結果
type aiLineSelection struct {
	LineIndex int             `json:"line"`  // 1-based index of the input line
	Start     json.RawMessage `json:"start"` // start seconds
	End       json.RawMessage `json:"end"`   // end seconds (0 if unknown)
	IsSong    bool            `json:"is_song"`
	Name      string          `json:"name"`   // 曲名（原文逐字，需通過 verbatim 驗證）
	Artist    string          `json:"artist"` // 歌手（原文逐字，需通過 verbatim 驗證）
}

const commentAISystemPrompt = `あなたはYouTubeのコメントから歌枠のセットリストを抽出するアシスタントです。

入力は番号付きのコメント行です。各行について判定し、JSON配列のみを返してください:
- 出力はJSON配列のみ（説明文やコードブロックは禁止）
- 各要素は {"line":行番号,"start":開始秒数,"end":終了秒数,"is_song":true/false,"name":"曲名","artist":"歌手名"}
- line は入力の行番号（1始まり）
- start/end は秒数の整数。end が不明な場合は 0。1行に複数のタイムスタンプがあれば最初を start、2つ目を end とする
- is_song: 歌曲なら true、雑談・開始・終了・告知などは false
- name と artist は【入力行に書かれている文字をそのままコピー】してください。翻訳・補完・表記の修正・並べ替えは一切禁止。入力に存在しない文字を出力してはいけません
- name は曲名のみ、artist は歌手のみ。年号や「アニメ『X』OP」などの付加情報は含めない
- artist が不明なら artist は空文字列にする
`

var aiTimestampRe = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2})|(\d{1,2}:\d{2})`)

// ParseCommentsWithAI uses an LLM to select and deduplicate song lines from raw comments.
// AI only returns line indices; actual text is extracted from original lines via regex.
func ParseCommentsWithAI(aiClient ai.Chatter, comments []string) ([]ParsedSong, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}

	lines := extractTimestampLines(comments)
	if len(lines) == 0 {
		return nil, fmt.Errorf("no timestamp lines")
	}

	userMessage := "以下のコメント行から歌のセットリストを抽出してください。\n\n"
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

	result := make([]ParsedSong, 0, len(selections))
	for _, sel := range selections {
		if !sel.IsSong {
			continue
		}
		if sel.LineIndex < 1 || sel.LineIndex > len(lines) {
			continue
		}

		originalLine := lines[sel.LineIndex-1]

		// 正則 baseline（AI 不可用 / 未通過驗證時的保底）
		parsed := ParseComment(originalLine)
		if parsed == nil {
			parsed = &ParsedSong{OriginalComment: originalLine, IsEndTimeEstimated: true}
		}

		// AI 抽取的歌名/歌手：僅在「逐字出現在原始行」時才採用，杜絕幻覺竄改
		if name := strings.TrimSpace(sel.Name); name != "" && isVerbatim(name, originalLine) {
			parsed.Name = name
		}
		if artist := strings.TrimSpace(sel.Artist); artist != "" && isVerbatim(artist, originalLine) {
			parsed.OriginalArtist = artist
		}

		// 時間：AI 有提供就用 AI 的
		if start := parseAISeconds(sel.Start); start > 0 {
			parsed.Start = start
		}
		if end := parseAISeconds(sel.End); end > 0 {
			parsed.End = end
			parsed.IsEndTimeEstimated = false
		}

		if parsed.Name == "" {
			continue
		}
		result = append(result, *parsed)
	}

	return result, nil
}

// isVerbatim 判斷 candidate 是否「逐字」出現在 source 中（NFKC + 去空白 + lower 後比對子字串）。
// 用來確保 AI 回傳的歌名/歌手確實來自原文，而非模型自行生成/竄改。
func isVerbatim(candidate, source string) bool {
	c := normalizeForMatch(candidate)
	if c == "" {
		return false
	}
	return strings.Contains(normalizeForMatch(source), c)
}

func normalizeForMatch(s string) string {
	s = util.NormalizeUnicode(s) // NFKC：全形→半形等
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	return strings.ToLower(s)
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
