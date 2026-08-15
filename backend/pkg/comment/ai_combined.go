package comment

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/perftag"
)

// このファイルは「抽出＋正規化を 1 回の AI 呼び出しで行う」経路。
//
// 2 段階（ParseCommentsWithAI → BatchAINormalization）には構造的な情報欠落がある。
// 正規化には抽出後の名前しか渡らないため、元のコメント行にしか無い情報を使えない。
// 実データでは、バージョン表記の 51.8% が括弧を使わない裸の形（"幾億光年 piano ver./ Omoinotake"）で、
// 抽出後の名前からは復元できない。さらに "リバベレ「ファタール」shorts公開!" のように、
// 語が現れていても版種ではないケースがあり、判定には原文の文脈が要る。
//
// 一方、正規化は Holodex 由来の曲や編集画面の手動ボタンからも呼ばれる。
// そこには元コメントが存在しないため、単独の正規化経路（BatchAINormalization）は残す。
// つまり経路は 2 本：
//   - 元コメントがある  → こちら（1 回で抽出＋正規化）
//   - 元コメントが無い  → BatchAINormalization（正規化のみ）

// combinedAISystemPrompt は抽出と正規化を一度に行わせる指示。
//
// 設計の要点は「逐字」と「正規化後」を別フィールドに分けること。
// 抽出の安全性は "原文に逐語で現れること" を Go 側で検証することで担保しているが、
// 正規化はその文字列を意図的に変える作業なので、同じフィールドでは両立しない。
const combinedAISystemPrompt = `あなたはYouTubeのコメントから歌枠のセットリストを抽出し、同時に楽曲名を正規化するアシスタントです。

**出力は必ず "[" で始まり "]" で終わる純粋なJSON配列のみ。**説明文・コードブロック・前後の一切のテキストを付けてはいけません。

入力は番号付きのコメント行です。**入力の各行につき必ず1要素**を、入力と同じ順序で返してください。行の省略・統合は禁止です。

キーは短縮形です。出力量を抑えるため、必ずこの通りのキー名を使ってください。

| キー | 意味 |
|----|----|
| l  | 行番号（1始まり） |
| s  | 歌唱行なら true |
| ts | 開始時刻の文字列（原文のまま） |
| te | 終了時刻の文字列（原文のまま。無ければ ""） |
| nv | 曲名（原文のまま・逐字） |
| av | アーティスト（原文のまま・逐字） |
| n  | 正規化後の曲名 |
| nr | 曲名の平仮名読み |
| a  | 正規化後のアーティスト |
| ar | アーティストの平仮名読み |
| t  | タグの配列 |
| c  | 確信度 0.0〜1.0 |

## 判定

- s（歌唱行かどうか）: 実際に歌唱された曲なら true。以下はすべて false:
  - 雑談・開演・終了・告知・スパチャ読み
  - 「トピック 「発言内容」」のような実況メモ（行内に曲名が引用されていても話題であって歌唱ではない）
  - 絵文字や記号だけの行、リスナーの感想（「ここ好き」「高音が綺麗」など）
  - 罫線（┗ ┣ ┏ など）でぶら下げた注釈行
- **s が false の行は l・s・ts の3つだけを返してください。**
  他のキーは書かないこと（出力量を抑えるため）。

## 逐字フィールド（nv / av）

**入力行に書かれている文字をそのままコピー**してください。翻訳・補完・表記修正・並べ替えは禁止です。
入力に存在しない文字を書いてはいけません。artist が書かれていなければ空文字列にします。

## タイムスタンプ（ts / te）

**入力行に実際に書かれている時刻文字列をそのままコピー**します（例 "01:12:42" "58:35"）。
秒数に変換してはいけません。1行に2つあれば順に ts / te へ。
s が false の行でも、時刻があれば ts は入れてください。

## 正規化フィールド（n / a / nr / ar）

- n: 演奏バージョンの表記（Acoustic Ver., Short Ver., Piano ver., アカペラ など
  **演奏方法**を示すもの）を取り除いた曲名。
  ただし Remix / Cover / Live は**別の楽曲**を指すので取り除かず残すこと。
  曲名に日本語と英語が併記されている場合（「ぼくらのレットイットビー / Bokura no Let It Be」）は日本語のみ。
  元から英語の曲名（「First Love」「KICK BACK」）は英語のまま。カタカナ化しないこと。
- a: 照合に使うアーティスト名。**行に書かれているものだけを入れること。**
  書かれていなければ空のままにする（推測で埋めない）。
  「原曲はこの人だ」と知っていても書き換えてはいけません
  （例: "涙そうそう / 夏川りみ" の a は "夏川りみ"。"BEGIN" にしない）。
  **演奏方法を表す語をアーティストにしてはいけません。**
  例えば "『愛・おぼえていますか』（アカペラ）" の「アカペラ」はアーティストではなくタグです。
- nr / ar: 平仮名の読み。不明なら空文字列。
- c: 0.0〜1.0。

## t（演奏バージョンのタグ）

**原文の行全体を見て**判定してください。括弧の有無は問いません。
"幾億光年 piano ver./ Omoinotake" のように括弧が無くてもタグを付けます。
一方 "リバベレ「ファタール」shorts公開!" の shorts は YouTube Shorts の告知であって
演奏バージョンではないため、タグを付けてはいけません。

使えるタグは次の7種のみ（他の値を返してはいけません）:
acoustic / piano / 弾き語り / acappella / short / full / medley

## 出力例（入力3行。3行目は歌唱行ではないので3キーだけ）

[
  {"l":1,"s":true,"ts":"1:46:35","te":"","nv":"愛・おぼえていますか","av":"","n":"愛・おぼえていますか","nr":"あいおぼえていますか","a":"","ar":"","t":["acappella"],"c":0.9},
  {"l":2,"s":true,"ts":"01:38:15","te":"","nv":"幾億光年","av":"Omoinotake","n":"幾億光年","nr":"いくおくこうねん","a":"Omoinotake","ar":"おもいのたけ","t":["piano"],"c":0.9},
  {"l":3,"s":false,"ts":"0:00:00"}
]`

// combinedSelection は統合プロンプトの1要素。
//
// JSON のキーを 1〜2 文字に切り詰めているのは、キー名が要素ごとに繰り返され
// 出力トークンを大きく食うため。実測では長いキー（"normalized_name_reading" 等）を
// 使うと出力が約 1.9 倍になり、出力単価は入力の 6 倍なので費用への影響が大きい。
// Go 側のフィールド名は読みやすさのため長いまま保つ。
type combinedSelection struct {
	LineIndex int      `json:"l"`
	IsSong    bool     `json:"s"`
	StartTS   string   `json:"ts"`
	EndTS     string   `json:"te"`
	NameVerb  string   `json:"nv"`
	ArtistVer string   `json:"av"`
	NormName  string   `json:"n"`
	NormRead  string   `json:"nr"`
	NormArt   string   `json:"a"`
	NormArtRd string   `json:"ar"`
	Tags      []string `json:"t"`
	Confid    float64  `json:"c"`
}

// ParseAndNormalizeWithAI は抽出と正規化を 1 回の呼び出しで行う。
//
// 返す ParsedSong には逐字フィールド（Name / OriginalArtist）と
// 正規化フィールド（NormalizedName ほか）の両方が入るため、
// 呼び出し側は後段の AI 正規化を省いて DB 照合だけを行えばよい。
func ParseAndNormalizeWithAI(aiClient ai.Chatter, comments []string) ([]ParsedSong, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}

	lines := extractTimestampLines(comments)
	if len(lines) == 0 {
		return nil, ErrNoTimestampLines
	}

	var sb strings.Builder
	fmt.Fprintf(&sb,
		"以下のコメント行をすべて解析してください。入力は %d 行です。各行について is_song を判定し、**%d 要素**を含む純粋なJSON配列のみを返してください（行の省略禁止）。\n\n",
		len(lines), len(lines),
	)
	for i, line := range lines {
		fmt.Fprintf(&sb, "%d) %s\n", i+1, line)
	}
	userMessage := sb.String()

	logger.Debugf("AI combined input userMessage (len=%d): %s", len(userMessage), userMessage)
	response, err := aiClient.SimpleChat(combinedAISystemPrompt, userMessage)
	if err != nil {
		return nil, err
	}
	logger.Debugf("AI combined raw response (len=%d): %s", len(response), response)

	selections, err := parseCombinedSelections(response)
	if err != nil {
		return nil, err
	}
	if len(selections) != len(lines) {
		logger.Warnf("AI combined: expected %d selections, got %d", len(lines), len(selections))
	}

	return buildSongsFromCombined(selections, lines), nil
}

func parseCombinedSelections(response string) ([]combinedSelection, error) {
	response = ai.CleanJSONResponse(response)
	decoder := json.NewDecoder(strings.NewReader(response))
	var items []combinedSelection
	if err := decoder.Decode(&items); err != nil {
		preview := response
		if len(preview) > 800 {
			preview = preview[:400] + " ... [truncated] ... " + preview[len(preview)-300:]
		}
		return nil, fmt.Errorf("unmarshal AI combined response: %w, response_preview: %s", err, preview)
	}
	return items, nil
}

// buildSongsFromCombined は AI の選択を ParsedSong へ落とす。
//
// 逐字フィールドは 2 段階経路と同じ検証（原文に逐語で現れること）を通す。
// 正規化フィールドは「意図的に原文と違う」ので逐語検証にかけないが、
// 空なら逐字の値へ、タグは既知の語彙へ丸めることで、AI の逸脱が
// そのまま DB へ流れ込まないようにしている。
func buildSongsFromCombined(selections []combinedSelection, lines []string) []ParsedSong {
	result := make([]ParsedSong, 0, len(selections))

	for _, sel := range selections {
		if !sel.IsSong {
			continue
		}
		if sel.LineIndex < 1 || sel.LineIndex > len(lines) {
			continue
		}
		originalLine := lines[sel.LineIndex-1]

		parsed := ParseComment(originalLine)
		if parsed == nil {
			parsed = &ParsedSong{OriginalComment: originalLine, IsEndTimeEstimated: true}
		}

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

		// 逐字フィールド：原文に現れる場合のみ採用（幻覺による書き換えを防ぐ）
		if name := firstSlashField(sel.NameVerb); name != "" && isVerbatim(name, originalLine) {
			parsed.Name = name
		}
		if artist := firstSlashField(sel.ArtistVer); artist != "" && isVerbatim(artist, originalLine) {
			if collabMarkerRe.MatchString(artist) {
				artist = ""
			}
			parsed.OriginalArtist = artist
		}

		// 正規化フィールド：空なら逐字側にフォールバックする
		parsed.NormalizedName = strings.TrimSpace(sel.NormName)
		if parsed.NormalizedName == "" {
			parsed.NormalizedName = parsed.Name
		}
		parsed.NormalizedArtist = strings.TrimSpace(sel.NormArt)
		parsed.NormalizedNameReading = strings.TrimSpace(sel.NormRead)
		parsed.NormalizedArtistReading = strings.TrimSpace(sel.NormArtRd)
		parsed.Tags = perftag.Normalize(sel.Tags, parsed.Name)
		parsed.Confidence = sel.Confid

		result = append(result, *parsed)
	}

	return result
}

// filterAllowedTags は既知のタグ語彙だけを残す。
// AI が "piano ver." や "ピアノ" のような表記で返しても DB のタグ ID とは一致しないため、
// 語彙外は落とす（誤ったタグを作るより、付かない方が害が小さい）。
