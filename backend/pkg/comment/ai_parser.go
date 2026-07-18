package comment

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/util"
)

// aiLineSelection AI 回傳的單行解析結果
// 我們優先讓 AI 回傳「原文中出現的時間字串」（start_ts / end_ts），
// 後端再用確定性的 parseTimestampString 轉成秒數。
// 這樣即使留言格式很特殊，AI 也能準確指出「這一段文字是時間」。
type aiLineSelection struct {
	LineIndex int             `json:"line"`     // 1-based index of the input line
	Start     json.RawMessage `json:"start"`    // legacy: numeric seconds or old format
	End       json.RawMessage `json:"end"`      // legacy
	StartTS   string          `json:"start_ts"` // 推薦：原文裡的時間字串，例如 "01:12:42" 或 "1:12:42"
	EndTS     string          `json:"end_ts"`   // 推薦：如果行內有第二個時間
	IsSong    bool            `json:"is_song"`
	Name      string          `json:"name"`     // 曲名（必須逐字出現在原始行）
	Artist    string          `json:"artist"`   // 歌手（必須逐字出現在原始行）
}

const commentAISystemPrompt = `あなたはYouTubeのコメントから歌枠のセットリストを抽出するアシスタントです。

**最重要指示（必ず守れ）**:
- 出力は**必ず "[" で始まり "]" で終わる純粋なJSON配列**にすること。
- 1つでもオブジェクトだけ、またはオブジェクトをカンマで並べただけの出力は厳禁。
- 前後に一切の説明文、"** Output only JSON."、"以下は" などのテキストを絶対に付けない。
- コードブロック（バッククォート三つで囲む記法）も絶対に使用禁止。
- 出力はJSON配列**のみ**。余計な文字は1つも書くな。

入力は番号付きのコメント行です。**入力の各行について必ず1要素ずつ**判定し、JSON配列のみを返してください:
- **必須**: 配列の要素数は入力行数と一致すること。行の省略・統合・スキップは厳禁。
- 各要素は {"line":行番号,"is_song":true/false,"start_ts":"...","end_ts":"...","name":"曲名","artist":"歌手名"}
- line は入力の行番号（1始まり）。1から入力行数まで、欠番なくすべて含めること。
- is_song: 歌曲なら true、雑談・開演・幕開け・終了・閉幕・告知・スパチャ読みなどは false
- start_ts / end_ts には、**入力行に実際に書かれているタイムスタンプ文字列をそのままコピー**してください。
  例: "01:12:42", "1:12:42", "0:27:36", "58:35" など。自分で秒数に変換してはいけません。
  1行に2つのタイムスタンプ（開始;終了）がある場合は、最初のものを start_ts、2つ目を end_ts に。
  is_song=false の行でも、入力行にタイムスタンプがあれば start_ts を必ず含めること。
- name と artist は【入力行に書かれている文字をそのままコピー】してください。翻訳・補完・表記の修正・並べ替えは一切禁止。入力に存在しない文字を出力してはいけません
- is_song=true のとき: name は曲名のみ、artist は歌手のみ。年号や「アニメ『X』OP」などの付加情報は含めない
- is_song=false のとき: name と artist は空文字列にする
- artist が不明なら artist は空文字列にする

正しい出力例（3行入力 → 3要素。非歌曲行も省略せず返す）:
[
  {"line":1,"is_song":false,"start_ts":"0:00:00","end_ts":"","name":"","artist":""},
  {"line":2,"is_song":true,"start_ts":"0:13:25","end_ts":"","name":"Stand By You","artist":"Official髭男dism"},
  {"line":3,"is_song":true,"start_ts":"0:13:24","end_ts":"0:17:28","name":"Stand By You","artist":"Official髭男dism"}
]
`

// aiTimestampRe 用來預先篩選「可能包含時間戳」的行，減少送給 AI 的 token 量。
// 故意寫得比較寬鬆（包含常見變形），但仍然不是萬能。
// 如果某種極端格式還是漏掉，後續我們可以繼續放寬，或考慮直接把更多 raw lines 餵給 AI。
var aiTimestampRe = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2})|(\d{1,2}:\d{2})|\[\s*\d{1,2}:\d{2}|\d{1,2}[:：]\d{2}`)

// ParseCommentsWithAI uses an LLM to intelligently select song lines and identify
// name/artist even in irregular formats.
//
// Strategy:
// - We still do a loose pre-filter (extractTimestampLines) to reduce tokens.
// - AI receives numbered lines and is asked to return raw timestamp *strings*
//   exactly as they appear in the comment (start_ts / end_ts), plus verbatim name/artist.
// - Backend always uses deterministic parsing (parseTimestampString + ParseComment)
//   on either the AI-pointed raw strings or the full original line.
// - This way AI handles "weird format recognition", while time calculation and
//   safety (verbatim) stay reliable.
func ParseCommentsWithAI(aiClient ai.Chatter, comments []string) ([]ParsedSong, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}

	lines := extractTimestampLines(comments)
	if len(lines) == 0 {
		return nil, fmt.Errorf("no timestamp lines")
	}

	userMessage := fmt.Sprintf(
		"以下のコメント行をすべて解析してください。入力は %d 行です。各行について is_song を判定し、**%d 要素**を含む純粋なJSON配列のみを返してください（行の省略禁止）。\n\n",
		len(lines), len(lines),
	)
	for i, line := range lines {
		userMessage += fmt.Sprintf("%d) %s\n", i+1, line)
	}

	logger.Debugf("AI comment input userMessage (len=%d): %s", len(userMessage), userMessage)
	response, err := aiClient.SimpleChat(commentAISystemPrompt, userMessage)
	if err != nil {
		return nil, err
	}
	// 為了方便比對，完整記錄 AI 原始回覆（如果太長會被 console 截，但至少有）
	logger.Debugf("AI comment raw response (len=%d): %s", len(response), response)

	selections, err := parseAILineSelections(response)
	if err == nil && len(selections) != len(lines) {
		logger.Warnf("AI comment parse: expected %d line selections, got %d", len(lines), len(selections))
	}
	if err != nil {
		// デバッグ用に元の応答も記録（長すぎる場合はプレビュー）
		origPreview := response
		if len(origPreview) > 600 {
			origPreview = origPreview[:300] + " ... [truncated] ... " + origPreview[len(origPreview)-200:]
		}
		logger.Warnf("AI raw response (before clean): %s", origPreview)
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

		// 先用全文 ParseComment 做 baseline（處理大部分常見格式 + 歌曲拆分）
		parsed := ParseComment(originalLine)
		if parsed == nil {
			parsed = &ParsedSong{OriginalComment: originalLine, IsEndTimeEstimated: true}
		}

		// 優先採用 AI 指出的「原文原始時間字串」（這是針對特殊格式的主要改善點）
		// AI 會把留言裡實際出現的時間文字（例如 "1:12:42" 或 "0:13:24 ; 0:17:28" 的片段）抄給我們
		if sel.StartTS != "" {
			if ts := parseTimestampString(sel.StartTS); ts > 0 {
				parsed.Start = ts
			}
		}
		if sel.EndTS != "" {
			if ts := parseTimestampString(sel.EndTS); ts > 0 {
				parsed.End = ts
				parsed.IsEndTimeEstimated = false
			}
		}

		// Legacy fallback：如果新欄位沒有值，才試舊的 numeric start/end
		if parsed.Start == 0 {
			if start := parseAISeconds(sel.Start); start > 0 {
				parsed.Start = start
			}
		}
		if parsed.End == 0 {
			if end := parseAISeconds(sel.End); end > 0 {
				parsed.End = end
				parsed.IsEndTimeEstimated = false
			}
		}

		// AI 抽取的歌名/歌手：僅在「逐字出現在原始行」時才採用，杜絕幻覺竄改
		if name := firstSlashField(sel.Name); name != "" && isVerbatim(name, originalLine) {
			parsed.Name = name
		}
		if artist := firstSlashField(sel.Artist); artist != "" && isVerbatim(artist, originalLine) {
			// AI 可能把縫合行尾端的合唱者資訊（with ○○ 等）當成歌手 → 視為未知
			if collabMarkerRe.MatchString(artist) {
				artist = ""
			}
			parsed.OriginalArtist = artist
		}

		if parsed.Name == "" {
			continue
		}
		result = append(result, *parsed)
	}

	logger.Infof("AI comment parse succeeded, extracted %d songs from %d timestamp lines", len(result), len(lines))

	// 額外記錄每首歌的來源，方便對照 AI 輸出 vs 實際結果
	for i, p := range result {
		logger.Debugf("AI parsed song %d: start=%d name=%q artist=%q from line=%s", i, p.Start, p.Name, p.OriginalArtist, p.OriginalComment)
	}
	return result, nil
}

// firstSlashField 把 AI 回傳的欄位裁到第一個斜線欄位為止，
// 與 parseSongAndArtist 的「歌名/歌手/其餘欄位忽略」語意保持一致。
// 縫合行的合唱者資訊掛在最後一個斜線欄位，AI 若整段照抄也會在這裡被裁掉。
func firstSlashField(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "／", "/")
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
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
		// 先縫合兩行式條目，讓沒有時間戳的歌名行不會在這裡被過濾掉
		for _, line := range stitchTwoLineEntries(strings.Split(comment, "\n")) {
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
	response = ai.CleanJSONResponse(response)

	// Use Decoder instead of Unmarshal to be tolerant of extra data after the JSON array
	// (common when LLMs add trailing text or extra ] )
	decoder := json.NewDecoder(strings.NewReader(response))
	var items []aiLineSelection
	if err := decoder.Decode(&items); err != nil {
		// ログ用にプレビューを付ける
		preview := response
		if len(preview) > 800 {
			preview = preview[:400] + " ... [truncated] ... " + preview[len(preview)-300:]
		}
		return nil, fmt.Errorf("unmarshal AI response: %w, response_preview: %s", err, preview)
	}
	// ignore any extra data after the array
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
