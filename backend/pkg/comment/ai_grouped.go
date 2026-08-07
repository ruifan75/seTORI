package comment

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/pkg/ai"
)

// このファイルは実験的な経路：AI に「抽出＋正規化＋重複排除」まで一度にやらせる。
//
// 動機：1 つの配信に複数の視聴者がセトリを投稿することが多い（実測 59.3%）。
// 現状は全員ぶんの行を平坦に並べて AI に渡し、AI の出力を後段の DeduplicateSongs
// （開始 ±30 秒 + 曲名類似度 0.8）で機械的にまとめている。送信行の 26.1% は重複。
//
// AI は「別々のコメントが同じ曲を指している」ことを文脈で判断できるはずで、
// 類似度の閾値より賢く統合できる可能性がある。また片方に歌手名、もう片方に
// 終了時刻という補完関係も見える。ここではその精度を測れるようにする。
//
// ⚠️ 安全性の設計が他の経路と異なる。
// 抽出・統合の経路は「出力要素数 = 入力行数」を不変条件にして、行番号のずれや
// 取りこぼしを検出していた。重複排除をさせると要素数が減るためこれが使えない。
// 代わりに **AI にはどの行をまとめたか（src）を申告させ、実際の統合は Go 側で
// mergeParsedSong により確定的に行う**。AI は「グループ分けの提案」だけを担当し、
// 値の合成には関与しない。

// commentLine は元コメントの区別を保ったままの候補行。
type commentLine struct {
	CommentIndex int    // 何番目のコメント由来か（1 始まり）
	Text         string // 行そのもの
}

// extractTimestampLinesGrouped は extractTimestampLines と同じ抽出をしつつ、
// どのコメント由来かを保持する。AI に「別コメントの同じ曲」を認識させるために要る。
func extractTimestampLinesGrouped(comments []string) []commentLine {
	var lines []commentLine
	for ci, c := range comments {
		for _, line := range stitchTwoLineEntries(strings.Split(c, "\n")) {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if aiTimestampRe.MatchString(trimmed) {
				lines = append(lines, commentLine{CommentIndex: ci + 1, Text: trimmed})
			}
		}
	}
	return lines
}

const groupedAISystemPrompt = `あなたはYouTubeのコメントから歌枠のセットリストを抽出し、正規化し、重複をまとめるアシスタントです。

**出力は必ず "[" で始まり "]" で終わる純粋なJSON配列のみ。**説明文・コードブロック・前後の一切のテキストを付けてはいけません。

入力は「同じ配信に投稿された複数のコメント」を、コメントごとに区切って番号付きで並べたものです。
複数の視聴者が同じセットリストを投稿していることがよくあります。

## やること

1. 各行が実際の歌唱かどうかを判定する
2. **同じ曲を指す行は 1 つの要素にまとめる**（別のコメント由来でも、同じコメント内でも）
3. まとめた元の行番号を src にすべて列挙する
4. 曲名を正規化し、読みとタグを付ける

**歌唱行だけを返してください。**雑談・開演・終了・告知・スパチャ読み・実況メモ・
リスナーの感想・絵文字だけの行は、要素として返さないでください。

## 出力の形式

各要素のキーは短縮形です。必ずこの通りのキー名を使ってください。

| キー | 意味 |
|----|----|
| src | まとめた元の行番号の配列（1始まり）。1行だけなら要素1つ |
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

## まとめる / まとめない の判断

- 開始時刻がほぼ同じ（数十秒以内）で同じ曲名を指していれば、表記が違ってもまとめる
  例: "9:26 花泥棒さん" と "09:26 さよなら、花泥棒さん / ナノウ" は同じ曲
- **同じ曲名でも開始時刻が大きく離れていれば別の歌唱**（1つの配信で2回歌うことがある）。
  まとめないこと
- メドレーで曲が連続する場合、それぞれ別の曲として扱う

## 逐字フィールド（nv / av）

**src に挙げたいずれかの行に書かれている文字をそのままコピー**してください。
翻訳・補完・表記修正は禁止です。複数行をまとめた場合、最も情報量の多い行から採ってかまいません。
アーティストがどの行にも書かれていなければ空文字列にします。

## タイムスタンプ（ts / te）

**原文に書かれている時刻文字列をそのままコピー**します（例 "01:12:42" "58:35"）。
秒数に変換してはいけません。まとめた行の中に終了時刻を書いているものがあれば te に入れてください。

## 正規化フィールド（n / a / nr / ar）

- n: 演奏バージョンの表記（Acoustic Ver., Short Ver., Piano ver., アカペラ など
  **演奏方法**を示すもの）を取り除いた曲名。
  ただし Remix / Cover / Live は**別の楽曲**を指すので取り除かず残すこと。
  曲名に日本語と英語が併記されている場合は日本語のみ。
  元から英語の曲名（「First Love」「KICK BACK」）は英語のまま。カタカナ化しないこと。
- a: アーティスト名。**演奏方法を表す語をアーティストにしてはいけません。**
  "『愛・おぼえていますか』（アカペラ）" の「アカペラ」はアーティストではなくタグです。
- nr / ar: 平仮名の読み。不明なら空文字列。

## t（演奏バージョンのタグ）

**原文の行全体を見て**判定してください。括弧の有無は問いません。
"幾億光年 piano ver./ Omoinotake" のように括弧が無くてもタグを付けます。
一方 "リバベレ「ファタール」shorts公開!" の shorts は YouTube Shorts の告知であって
演奏バージョンではないため、タグを付けてはいけません。

使えるタグは次の7種のみ（他の値を返してはいけません）:
acoustic / piano / 弾き語り / acappella / short / full / medley

## 出力例

[
  {"src":[1,7],"ts":"9:26","te":"","nv":"さよなら、花泥棒さん","av":"ナノウ","n":"さよなら、花泥棒さん","nr":"さよならはなどろぼうさん","a":"ナノウ","ar":"なのう","t":[],"c":0.9},
  {"src":[3],"ts":"1:46:35","te":"","nv":"愛・おぼえていますか","av":"","n":"愛・おぼえていますか","nr":"あいおぼえていますか","a":"","ar":"","t":["acappella"],"c":0.9}
]`

// groupedSelection は重複排除ありプロンプトの1要素。
type groupedSelection struct {
	Src       []int    `json:"src"`
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

// ParseNormalizeAndDedupWithAI は抽出・正規化・重複排除を 1 回の呼び出しで行う（実験的）。
//
// 返り値は既に重複排除済みなので、呼び出し側で DeduplicateSongs を通す必要はない
// （通しても冪等なので害は無いが、二重に統合される可能性はある）。
func ParseNormalizeAndDedupWithAI(aiClient ai.Chatter, comments []string) ([]ParsedSong, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}

	lines := extractTimestampLinesGrouped(comments)
	if len(lines) == 0 {
		return nil, ErrNoTimestampLines
	}

	userMessage := buildGroupedMessage(lines)
	logger.Debugf("AI grouped input userMessage (len=%d): %s", len(userMessage), userMessage)

	response, err := aiClient.SimpleChat(groupedAISystemPrompt, userMessage)
	if err != nil {
		return nil, err
	}
	logger.Debugf("AI grouped raw response (len=%d): %s", len(response), response)

	var selections []groupedSelection
	cleaned := ai.CleanJSONResponse(response)
	if err := json.NewDecoder(strings.NewReader(cleaned)).Decode(&selections); err != nil {
		preview := cleaned
		if len(preview) > 800 {
			preview = preview[:400] + " ... [truncated] ... " + preview[len(preview)-300:]
		}
		return nil, fmt.Errorf("unmarshal AI grouped response: %w, response_preview: %s", err, preview)
	}

	return buildSongsFromGrouped(selections, lines), nil
}

// buildGroupedMessage はコメントの区切りを見せたまま行番号を振る。
// 区切りが無いと AI は「別の人が投稿した同じセトリ」を認識しづらい。
func buildGroupedMessage(lines []commentLine) string {
	var sb strings.Builder
	commentCount := 0
	for _, l := range lines {
		if l.CommentIndex > commentCount {
			commentCount = l.CommentIndex
		}
	}

	fmt.Fprintf(&sb,
		"以下は同じ配信に投稿された %d 件のコメントから抽出した %d 行です。同じ曲を指す行はまとめ、まとめた行番号を src に列挙してください。歌唱行のみを返してください。\n",
		commentCount, len(lines),
	)

	prev := -1
	for i, l := range lines {
		if l.CommentIndex != prev {
			fmt.Fprintf(&sb, "\n--- コメント %d ---\n", l.CommentIndex)
			prev = l.CommentIndex
		}
		fmt.Fprintf(&sb, "%d) %s\n", i+1, l.Text)
	}
	return sb.String()
}

// buildSongsFromGrouped は AI のグループ提案を ParsedSong へ落とす。
//
// 統合そのものは Go 側で行う：src に挙がった各行を確定的にパースして
// mergeParsedSong で畳み込み、その上に AI の指摘（時刻・逐字・正規化）を重ねる。
// AI が値を合成することはないので、幻覚が数値や名称に混ざる余地が無い。
func buildSongsFromGrouped(selections []groupedSelection, lines []commentLine) []ParsedSong {
	result := make([]ParsedSong, 0, len(selections))

	for _, sel := range selections {
		srcLines := validSrcLines(sel.Src, lines)
		if len(srcLines) == 0 {
			logger.Warnf("AI grouped: dropping element with no valid src %v", sel.Src)
			continue
		}

		// src の各行を確定的にパースして畳み込む
		var merged *ParsedSong
		for _, text := range srcLines {
			p := ParseComment(text)
			if p == nil {
				p = &ParsedSong{OriginalComment: text, IsEndTimeEstimated: true}
			}
			if merged == nil {
				merged = p
				continue
			}
			m := mergeParsedSong(*merged, *p)
			merged = &m
		}

		if sel.StartTS != "" {
			if ts := parseTimestampString(sel.StartTS); ts > 0 {
				merged.Start = ts
			}
		}
		if sel.EndTS != "" {
			if ts := parseTimestampString(sel.EndTS); ts > 0 {
				merged.End = ts
				merged.IsEndTimeEstimated = false
			}
		}

		// 逐字検証は src に挙がった行すべてを対象にする
		// （まとめた結果、歌手名は別の行から来ることがあるため）
		joined := strings.Join(srcLines, "\n")
		if name := firstSlashField(sel.NameVerb); name != "" && isVerbatim(name, joined) {
			merged.Name = name
		}
		if artist := firstSlashField(sel.ArtistVer); artist != "" && isVerbatim(artist, joined) {
			if collabMarkerRe.MatchString(artist) {
				artist = ""
			}
			merged.OriginalArtist = artist
		}

		merged.NormalizedName = strings.TrimSpace(sel.NormName)
		if merged.NormalizedName == "" {
			merged.NormalizedName = merged.Name
		}
		merged.NormalizedArtist = strings.TrimSpace(sel.NormArt)
		merged.NormalizedNameReading = strings.TrimSpace(sel.NormRead)
		merged.NormalizedArtistReading = strings.TrimSpace(sel.NormArtRd)
		merged.Tags = filterAllowedTags(sel.Tags)
		merged.Confidence = sel.Confid

		result = append(result, *merged)
	}

	return result
}

// validSrcLines は AI が申告した行番号のうち実在するものだけを取り出す。
// 重複した番号は 1 回だけ数える。
func validSrcLines(src []int, lines []commentLine) []string {
	seen := make(map[int]bool, len(src))
	var out []string
	for _, idx := range src {
		if idx < 1 || idx > len(lines) || seen[idx] {
			continue
		}
		seen[idx] = true
		out = append(out, lines[idx-1].Text)
	}
	return out
}
