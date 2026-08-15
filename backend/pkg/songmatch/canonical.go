// Package songmatch は楽曲の照合キーを作る。
//
// 背景：楽曲の同一性を (name, original_artist) の文字列完全一致で判定していたため、
// コメントから抽出した表記ゆれがことごとく外れていた。
//
//	コメント側の表記                          DB の表記
//	少女レイ / みきとP feat. 初音ミク           少女レイ / みきとP
//	ZIGG-ZAGG / Junky(1Chorus)                ZIGG-ZAGG (feat. 初音ミク) / Junky
//	私の彼はパイロット / ランカ・リー(中島愛)     私の彼はパイロット / ランカ・リー=中島愛
//
// 設計の要点は 2 つ。
//
// 1. 「曲名が強いキー、アーティストは検証用」。
// 実測（820曲）で曲名キーの衝突はわずか 13 組。一方 original_artist は 12% が
// feat./CV/連名などのクレジット文字列（例: "スペシャルウィーク (CV. 和氣あず未),
// サイレンススズカ (CV. 高野麻里佳) & ..."）で、文字列の完全一致を要求できる品質ではない。
// よってアーティストは単一文字列ではなく**トークン集合**として扱い、
// 一致条件ではなく絞り込み条件に降格させる。
//
// 2. 「迷ったら残す」。
// 括弧の中身を消しすぎると『モザイクロール (Reloaded)』と『モザイクロール』のような
// **別録音**を同一視してしまう（実測の衝突 13 組のうち 9 組がこの型）。
// 取りこぼしは下位ティアに落ちて人が拾えるが、誤一致は黙ってデータを壊す。
// そこで括弧の中身は**既知の語だけ**を消し、判断がつかないものは残す。
package songmatch

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/ruifan75/setori/pkg/util"
)

// RulesVersion は正規化ルールの版。
// このファイルの規則を変えたら必ず上げること。保存済みの照合キー
// （song_match_keys）は起動時にこの値と突き合わせて再構築される。
const RulesVersion = 1

// ---------- 削ってよい語 / 削ってはいけない語 ----------

// styleWords は「同じ楽曲の演奏違い」を表す語。曲名から削ってよい。
// 判定は括弧の中身を畳んだ（英数字だけにした）文字列との完全一致で行う。
// 末尾の ver / version / size は落としてから照合する。
var styleWords = map[string]bool{
	"acoustic":  true,
	"アコースティック":  true,
	"piano":     true,
	"ピアノ":       true,
	"弾き語り":      true,
	"ギター弾き語り":   true,
	"acappella": true,
	"アカペラ":      true,
	"short":     true,
	"tvsize":    true,
	"tv":        true,
	"opsize":    true,
	"full":      true,
	"フル":        true,
	"1chorus":   true,
	"ワンコーラス":    true,
	"1コーラス":     true,
	"1番":        true,
	"サビ":        true,
}

// あえて styleWords に入れていない語（＝残す語）:
//
//	remix / instrumental / inst / off vocal / reloaded / cover / live / edit / mix
//	独唱 / 〜version〜 のような楽曲名の一部
//
// これらは別の音源を指すか、曲名そのものの一部なので、消すと別曲が混ざる。

var (
	// (feat. X) (ft.X) (featuring X) (with X) (CV:X) (cv.X) — 括弧つきのクレジット
	creditBracketRe = regexp.MustCompile(`(?i)[(（\[【〔]\s*(?:feat|ft|featuring|with|cv|vo|vocal)[.．:：]?\s*[^)）\]】〕]*[)）\]】〕]`)
	// 末尾の裸クレジット: "少女レイ feat. 初音ミク" の feat. 以降
	creditTrailRe = regexp.MustCompile(`(?i)[\s　]+(?:feat|ft|featuring)[.．:：]?[\s　]+.*$`)
	// 中身を見てから判断する括弧
	anyBracketRe = regexp.MustCompile(`[(（\[【〔][^)）\]】〕]*[)）\]】〕]`)
	// 末尾の裸の演奏方法: "幾億光年 piano ver." の piano ver.
	styleTrailRe = regexp.MustCompile(`(?i)[\s　]*[-–—―]?[\s　]*(acoustic|piano|ピアノ|弾き語り|アカペラ|acappella|short|full|フル|tv)[\s　]*(ver\.?|version|size|サイズ)?[\s　]*$`)
	// アーティストの区切り。"・" は名前の内側で使われる（ランカ・リー）ので入れない。
	artistSepRe = regexp.MustCompile(`(?i)(?:[\s　]+(?:feat|ft|featuring|with)[.．:：]?[\s　]+)|[&＆×✕,、/／=＝+＋]|(?:[\s　]+(?:vs|and)[\s　]+)`)
	// トークン先頭の CV: / cv. / Vo. 表記
	cvPrefixRe = regexp.MustCompile(`(?i)^\s*(?:cv|vo|vocal|voice)[.．:：]?\s*`)
)

// ---------- 曲名 ----------

// TitleKey は曲名の照合キーを返す。
//
//	"ZIGG-ZAGG (feat. 初音ミク)" → "ziggzagg"
//	"ZIGG-ZAGG"                 → "ziggzagg"   （一致する）
//	"モザイクロール (Reloaded)"    → "モザイクロールreloaded"
//	"モザイクロール"               → "モザイクロール"  （一致しない＝正しい）
func TitleKey(name string) string {
	s := util.NormalizeUnicode(name)
	s = creditBracketRe.ReplaceAllString(s, " ")
	s = creditTrailRe.ReplaceAllString(s, " ")
	s = stripStyleBrackets(s)
	s = styleTrailRe.ReplaceAllString(s, " ")
	key := foldKey(s)
	if key == "" {
		// 曲名そのものが演奏方法と同じ語だった（"Piano" という曲など）。
		// 削った結果が空になったら削らなかったことにする。
		return foldKey(util.NormalizeUnicode(name))
	}
	return key
}

// stripStyleBrackets は括弧の中身が既知の演奏方法のときだけ括弧ごと消す。
// 判断がつかない中身（Remix, instrumental, 独唱 …）はそのまま残す。
func stripStyleBrackets(s string) string {
	return anyBracketRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := foldKey(strings.Trim(m, "(（[【〔)）]】〕"))
		inner = strings.TrimSuffix(inner, "version")
		inner = strings.TrimSuffix(inner, "ver")
		inner = strings.TrimSuffix(inner, "size")
		inner = strings.TrimSuffix(inner, "サイズ")
		if styleWords[inner] {
			return " "
		}
		return m
	})
}

// ---------- アーティスト ----------

// ArtistKey はアーティスト表記を照合しやすい形に分解したもの。
type ArtistKey struct {
	// Primary は主体。"みきとP feat. 初音ミク" なら "みきとp"。
	// 括弧・区切りより前の最初のトークンで、いちばん信頼できる 1 つ。
	Primary string
	// Tokens は登場する名前すべて（括弧の中も含む）。重複なし・ソート済み。
	// "ランカ・リー=中島愛" も "ランカ・リー(中島愛)" も {ランカリー, 中島愛} になる。
	Tokens []string
}

// String は照合キーとして保存・索引できる 1 本の文字列にする。
// トークンは畳み済み（英数字と仮名漢字のみ）なので区切りに "|" を使っても衝突しない。
// "ランカ・リー(中島愛)" も "ランカ・リー=中島愛" も同じ文字列になる。
func (k ArtistKey) String() string { return strings.Join(k.Tokens, "|") }

// ParseArtist はアーティスト表記を分解する。
func ParseArtist(artist string) ArtistKey {
	s := util.NormalizeUnicode(artist)
	outside, inner := splitBrackets(s)

	var raw []string
	// 主体は括弧の外の先頭トークンから採る
	outsideTokens := splitArtistTokens(outside)
	raw = append(raw, outsideTokens...)
	for _, seg := range inner {
		raw = append(raw, splitArtistTokens(seg)...)
	}

	key := ArtistKey{}
	seen := map[string]bool{}
	for _, t := range raw {
		t = cvPrefixRe.ReplaceAllString(t, "")
		folded := foldKey(t)
		if folded == "" || styleWords[folded] {
			// "Junky(1Chorus)" の 1Chorus はアーティストではない
			continue
		}
		if !seen[folded] {
			seen[folded] = true
			key.Tokens = append(key.Tokens, folded)
		}
	}
	if len(outsideTokens) > 0 {
		key.Primary = foldKey(cvPrefixRe.ReplaceAllString(outsideTokens[0], ""))
	}
	if key.Primary == "" && len(key.Tokens) > 0 {
		key.Primary = key.Tokens[0]
	}
	sortStrings(key.Tokens)
	return key
}

func splitArtistTokens(s string) []string {
	parts := artistSepRe.Split(s, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitBrackets は括弧の外の文字列と、括弧の中身（入れ子も個別に）を返す。
// "桜高軽音部 [平沢唯・秋山澪(CV:豊崎愛生)]" →
//
//	outside="桜高軽音部 ", inner=["CV:豊崎愛生", "平沢唯・秋山澪"]
func splitBrackets(s string) (string, []string) {
	var stack []*strings.Builder
	root := &strings.Builder{}
	stack = append(stack, root)
	var inner []string

	for _, r := range s {
		switch r {
		case '(', '（', '[', '［', '【', '〔', '〈', '<':
			stack = append(stack, &strings.Builder{})
		case ')', '）', ']', '］', '】', '〕', '〉', '>':
			if len(stack) > 1 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if t := strings.TrimSpace(top.String()); t != "" {
					inner = append(inner, t)
				}
				continue
			}
			// 対応する開き括弧が無い閉じ括弧は捨てる
		default:
			stack[len(stack)-1].WriteRune(r)
		}
	}
	// 閉じられなかった括弧の中身も拾う
	for i := len(stack) - 1; i >= 1; i-- {
		if t := strings.TrimSpace(stack[i].String()); t != "" {
			inner = append(inner, t)
		}
	}
	return root.String(), inner
}

// ---------- アーティストの関係 ----------

// ArtistRelation は 2 つのアーティスト表記の近さ。数値が大きいほど強い証拠。
type ArtistRelation int

const (
	// ArtistNone は共通するトークンが無い。別人の可能性が高い（別名の場合もある）。
	ArtistNone ArtistRelation = iota
	// ArtistUnknown は片方が空。否定の証拠にはならない。
	ArtistUnknown
	// ArtistOverlap は名前がひとつ以上共通する（連名の一部が一致など）。
	ArtistOverlap
	// ArtistPrimary は主体が一致する。"みきとP feat. 初音ミク" と "みきとP"。
	ArtistPrimary
	// ArtistSame はトークン集合が完全に一致する。
	ArtistSame
)

// CompareArtists は 2 つのアーティスト表記の関係を返す。
func CompareArtists(a, b ArtistKey) ArtistRelation {
	if len(a.Tokens) == 0 || len(b.Tokens) == 0 {
		return ArtistUnknown
	}
	if equalStrings(a.Tokens, b.Tokens) {
		return ArtistSame
	}
	if a.Primary != "" && a.Primary == b.Primary {
		return ArtistPrimary
	}
	set := make(map[string]bool, len(a.Tokens))
	for _, t := range a.Tokens {
		set[t] = true
	}
	for _, t := range b.Tokens {
		if set[t] {
			return ArtistOverlap
		}
	}
	return ArtistNone
}

// ---------- 共通 ----------

// foldKey は比較用に文字を畳む。小文字化し、文字と数字だけを残す。
// 記号・空白・中黒・ハイフンはすべて落ちるので "ZIGG-ZAGG" と "ZIGG ZAGG" は同じになる。
func foldKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(util.NormalizeUnicode(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// IdentityPairKey は「この表記」と「この曲」の組を一意に表すキー。
// 判定の重複問い合わせを避けるために使う（repository 側の列と同じ組み立て方）。
func IdentityPairKey(nameKey, artistKey, songID string) string {
	return nameKey + "\x1f" + artistKey + "\x1f" + songID
}
