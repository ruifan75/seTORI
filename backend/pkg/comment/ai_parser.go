package comment

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/util"
)

// aiLineSelection は AI が返す 1 行分の解析結果。
// AI には「原文に現れた時刻文字列」（start_ts / end_ts）を優先して返させる。
// バックエンドで決定的な parseTimestampString を使い、後から秒数へ変換する。
// これにより特殊なコメント形式でも、AI は「この部分が時刻」と正確に示せる。
type aiLineSelection struct {
	LineIndex int             `json:"line"`     // 1-based index of the input line
	Start     json.RawMessage `json:"start"`    // legacy: numeric seconds or old format
	End       json.RawMessage `json:"end"`      // legacy
	StartTS   string          `json:"start_ts"` // 推奨：原文内の時刻文字列。例："01:12:42" または "1:12:42"
	EndTS     string          `json:"end_ts"`   // 推奨：行内に二つ目の時刻がある場合
	IsSong    bool            `json:"is_song"`
	Name      string          `json:"name"`   // 曲名（元の行に原文どおり存在すること）
	Artist    string          `json:"artist"` // 歌手（元の行に原文どおり存在すること）
}

// ErrNoTimestampLines はコメントにタイムスタンプらしき行が 1 つも無かったことを示す。
//
// これは AI の失敗ではなく、そもそも解析対象が無いという正常な結果。
// 呼び出し側はこれを「劣化」として扱ってはいけない（警告を出したり
// キャッシュ書き込みを止めたりすると、毎回無駄に再解析することになる）。
var ErrNoTimestampLines = errors.New("no timestamp lines")

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
- is_song: 楽曲なら true、雑談・開演・幕開け・終了・閉幕・告知・スパチャ読みなどは false
  - **特に注意（すべて is_song=false）**:
    - 「トピック 「発言内容」」のような実況・トークのメモ行（例: 挨拶運動「みんな、ただいまー!」、"祝福"後「YOASOBIさん合うね」）。行内に曲名が引用されていても、それは歌唱ではなく話題なので false。
    - 絵文字・記号だけの行（📸 🎸 🦋🌹 ??? など）。
    - リスナーの感想・反応（「ここ好き」「かわいい」「高音が綺麗」など）。
    - 罫線（┗ ┣ ┏ など）で本項目にぶら下げた小見出し・注釈行。
- start_ts / end_ts には、**入力行に実際に書かれているタイムスタンプ文字列をそのままコピー**してください。
  例: "01:12:42", "1:12:42", "0:27:36", "58:35" など。自分で秒数に変換してはいけません。
  1行に2つのタイムスタンプ（開始;終了）がある場合は、最初のものを start_ts、2つ目を end_ts に。
  is_song=false の行でも、入力行にタイムスタンプがあれば start_ts を必ず含めること。
- name と artist は【入力行に書かれている文字をそのままコピー】してください。翻訳・補完・表記の修正・並べ替えは一切禁止。入力に存在しない文字を出力してはいけません
- is_song=true のとき: name は曲名のみ、artist は歌手のみ。年号や「アニメ『X』OP」などの付加情報は含めない
- is_song=false のとき: name と artist は空文字列にする
- artist が不明なら artist は空文字列にする

正しい出力例（3行入力 → 3要素。楽曲でない行も省略せず返す）:
[
  {"line":1,"is_song":false,"start_ts":"0:00:00","end_ts":"","name":"","artist":""},
  {"line":2,"is_song":true,"start_ts":"0:13:25","end_ts":"","name":"Stand By You","artist":"Official髭男dism"},
  {"line":3,"is_song":true,"start_ts":"0:13:24","end_ts":"0:17:28","name":"Stand By You","artist":"Official髭男dism"}
]
`

// aiTimestampRe は「タイムスタンプを含む可能性がある」行を事前に絞り込み、AI へ送る token 量を減らす。
// 一般的な表記揺れを含めるため意図的に緩くしているが、万能ではない。
// 極端な形式を取りこぼす場合はさらに緩めるか、AI に渡す raw lines を増やすことを検討する。
var aiTimestampRe = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2})|(\d{1,2}:\d{2})|\[\s*\d{1,2}:\d{2}|\d{1,2}[:：]\d{2}`)

// ParseCommentsWithAI uses an LLM to intelligently select song lines and identify
// name/artist even in irregular formats.
//
// Strategy:
//   - We still do a loose pre-filter (extractTimestampLines) to reduce tokens.
//   - AI receives numbered lines and is asked to return raw timestamp *strings*
//     exactly as they appear in the comment (start_ts / end_ts), plus verbatim name/artist.
//   - Backend always uses deterministic parsing (parseTimestampString + ParseComment)
//     on either the AI-pointed raw strings or the full original line.
//   - This way AI handles "weird format recognition", while time calculation and
//     safety (verbatim) stay reliable.
func ParseCommentsWithAI(aiClient ai.Chatter, comments []string) ([]ParsedSong, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}

	lines := extractTimestampLines(comments)
	if len(lines) == 0 {
		return nil, ErrNoTimestampLines
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
	// 比較しやすいよう AI の元レスポンスをすべて記録する（長すぎると console で切れるが、記録は残る）
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

		// まず行全体を ParseComment に渡して基準結果を作る（一般的な形式と楽曲分割の大半を処理）
		parsed := ParseComment(originalLine)
		if parsed == nil {
			parsed = &ParsedSong{OriginalComment: originalLine, IsEndTimeEstimated: true}
		}

		// AI が示した「原文の時刻文字列」を優先する（特殊形式に対する主な改善点）
		// AI はコメントに実際にある時刻文字列（例："1:12:42" や "0:13:24 ; 0:17:28" の一部）をそのまま返す
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

		// 旧形式へのフォールバック：新フィールドが空の場合だけ numeric start/end を試す
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

		// AI が抽出した曲名／歌手は、元の行に原文どおり存在する場合だけ採用し、幻覚による改変を防ぐ
		if name := firstSlashField(sel.Name); name != "" && isVerbatim(name, originalLine) {
			parsed.Name = name
		}
		if artist := firstSlashField(sel.Artist); artist != "" && isVerbatim(artist, originalLine) {
			// AI が結合行末尾のコラボ情報（with ○○ など）を歌手とみなした場合は不明として扱う
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

	// AI の出力と実際の結果を比較しやすいよう、各曲の由来も記録する
	for i, p := range result {
		logger.Debugf("AI parsed song %d: start=%d name=%q artist=%q from line=%s", i, p.Start, p.Name, p.OriginalArtist, p.OriginalComment)
	}
	return result, nil
}

// firstSlashField は AI が返したフィールドを最初のスラッシュ区切りまでに切り詰める。
// parseSongAndArtist の「曲名/歌手/その他のフィールドは無視」という意味と揃える。
// 結合行のコラボ情報は最後のスラッシュ区切りにあるため、AI が全体をコピーしてもここで除かれる。
func firstSlashField(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "／", "/")
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// isVerbatim は candidate が source に原文どおり存在するかを判定する（NFKC 正規化、空白除去、小文字化の後に部分文字列を比較）。
// AI が返した曲名／歌手がモデルの生成や改変ではなく、確実に原文由来であることを保証する。
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
		// 先に 2 行形式の項目を結合し、タイムスタンプのない曲名行がここで除外されないようにする
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
