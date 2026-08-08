package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/songmatch"
)

// アーティストの別名義を AI に判定させる。
//
// 「曲名は一意に一致しているのにアーティストだけ違う」（照合理由 title_mismatch）は、
// 文字列の比較では原理的に決着しない。松任谷由実 / 荒井由実 のような同一人物の
// 改名・別名義か、たまたま同名の別の曲かのどちらかで、区別には世界知識が要る。
//
// 設計上の要点は 3 つ。
//
//  1. **聞くのは一度だけ。** 判定は肯定・否定とも artist_alias_checks に残す。
//     否定を残さないと、当たらない組に対して毎回 AI を呼び続けることになり、
//     費用も遅延も収束しない。学習層の価値は「二度目が速い」ことにある。
//  2. **前提が強いときにしか聞かない。** 曲名キーが一意に一致している場合に限る。
//     曲名という強い証拠があるうえでアーティストだけが食い違う状況なら、
//     別名義である事前確率が十分に高い。
//  3. **失敗は素通し。** AI が落ちても照合は素の結果のままで、
//     その組は統合候補として人に回る（030 の受け皿）。

const artistAliasSystemPrompt = `あなたは音楽アーティストの名義に詳しいアシスタントです。

与えられた名前の組それぞれについて、**同一の人物・グループが使った別名義かどうか**を判定してください。

## 同一と判定してよい例
- 改名・旧名義（荒井由実 と 松任谷由実、Kalafina の梶浦由記 など）
- 本名とアーティスト名、別プロジェクト名義（米津玄師 と ハチ）
- キャラクター名と声優名（ランカ・リー と 中島愛、涼宮ハルヒ と 平野綾）
- 表記ゆれ（ローマ字と日本語、大文字小文字、記号の有無）

## 同一と判定してはいけない例
- 別のアーティストによるカバー（原曲が YOASOBI、歌ったのが 家入レオ）
- 同じグループの別メンバー
- たまたま似た名前の別人
- **少しでも確信が持てない場合**

判定に迷ったら必ず false にしてください。
false は「別の曲として登録する」だけですが、true を誤ると全楽曲の照合が狂います。

## 出力

**"[" で始まり "]" で終わる純粋なJSON配列のみ**を返してください。説明文は一切不要です。

各要素:
- i: 入力の番号 (number)
- same: 同一人物・同一グループの別名義なら true (boolean)
- why: 理由を日本語で20文字以内 (string)

例:
[{"i":0,"same":true,"why":"荒井由実の結婚後の名義"},{"i":1,"same":false,"why":"別アーティストのカバー"}]`

// artistPair は判定したい 1 組。
type artistPair struct {
	DisplayA string // 元の表記（AI に見せる）
	DisplayB string
	KeyA     string // 畳んだ照合キー（保存に使う）
	KeyB     string
}

type artistAliasVerdict struct {
	Index int    `json:"i"`
	Same  bool   `json:"same"`
	Why   string `json:"why"`
}

// adjudicateArtistAliases は未判定の組を AI に聞き、結果を永続化する。
// 返り値は「同一人物」と判定されて別名義に登録された組の数。
//
// 呼び出し側はこの戻り値が 0 より大きいときだけ照合をやり直せばよい。
func (s *NormalizationService) adjudicateArtistAliases(pairs []artistPair) int {
	if s.matchService == nil || s.aiClient == nil || len(pairs) == 0 {
		return 0
	}

	pending, err := s.filterUncheckedPairs(pairs)
	if err != nil {
		logger.Warnf("artist alias check lookup failed: %v", err)
		return 0
	}
	if len(pending) == 0 {
		return 0
	}

	var sb strings.Builder
	sb.WriteString("次の名前の組を判定してください：\n\n")
	for i, p := range pending {
		fmt.Fprintf(&sb, "[%d] %s ／ %s\n", i, p.DisplayA, p.DisplayB)
	}

	logger.Infof("AI にアーティストの別名義を問い合わせます: %d 組", len(pending))
	resp, err := s.aiClient.SimpleChat(artistAliasSystemPrompt, sb.String())
	if err != nil {
		// 判定できなくても照合は素の結果で続く。統合候補として人に回るだけ。
		logger.Warnf("artist alias adjudication failed: %v", err)
		return 0
	}

	var verdicts []artistAliasVerdict
	if err := json.Unmarshal([]byte(ai.CleanJSONResponse(resp)), &verdicts); err != nil {
		logger.Warnf("artist alias response parse failed: %v (resp=%.200s)", err, resp)
		return 0
	}

	linked := 0
	for _, v := range verdicts {
		if v.Index < 0 || v.Index >= len(pending) {
			continue
		}
		p := pending[v.Index]
		if !v.Same {
			// 否定こそ残す価値がある。次からこの組は AI に聞かない。
			if err := s.matchService.RecordArtistAliasVerdict(p.KeyA, p.KeyB, false, "ai", v.Why); err != nil {
				logger.Warnf("record alias verdict failed: %v", err)
			}
			continue
		}
		if err := s.matchService.LinkArtistAliases(p.DisplayA, p.DisplayB, "ai", v.Why); err != nil {
			logger.Warnf("link artist aliases failed (%s / %s): %v", p.DisplayA, p.DisplayB, err)
			continue
		}
		linked++
	}
	logger.Infof("アーティストの別名義: %d 組を判定し、%d 組を同一人物として登録しました", len(verdicts), linked)
	return linked
}

// filterUncheckedPairs は「まだ聞いていない組」だけを残す。
// 同じ組が複数の曲から挙がることがあるので重複も潰す。
func (s *NormalizationService) filterUncheckedPairs(pairs []artistPair) ([]artistPair, error) {
	unique := make(map[string]artistPair, len(pairs))
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p.KeyA == "" || p.KeyB == "" || p.KeyA == p.KeyB {
			continue
		}
		k := repository.AliasPairKey(p.KeyA, p.KeyB)
		if _, ok := unique[k]; !ok {
			unique[k] = p
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}

	checked, err := s.matchService.CheckedArtistPairs(keys)
	if err != nil {
		return nil, err
	}
	out := make([]artistPair, 0, len(keys))
	for _, k := range keys {
		if _, done := checked[k]; done {
			continue
		}
		out = append(out, unique[k])
	}
	return out, nil
}

// collectArtistAliasPairs は照合結果から「聞くべき組」を拾う。
// title_mismatch（曲名は一意に一致・アーティストだけ違う）だけが対象。
func collectArtistAliasPairs(queryArtist string, cands []MatchCandidate) []artistPair {
	var out []artistPair
	qk := songmatch.ParseArtist(queryArtist)
	for _, c := range cands {
		if c.Reason != ReasonTitleMismatch {
			continue
		}
		dk := songmatch.ParseArtist(c.Song.OriginalArtist)
		// 連名クレジットは「どの名前とどの名前を比べるのか」が定まらないので聞かない。
		// 単独名義どうしに限れば問いが曖昧にならず、誤った別名義も作りにくい。
		if len(qk.Tokens) != 1 || len(dk.Tokens) != 1 {
			continue
		}
		out = append(out, artistPair{
			DisplayA: queryArtist,
			DisplayB: c.Song.OriginalArtist,
			KeyA:     qk.Tokens[0],
			KeyB:     dk.Tokens[0],
		})
	}
	return out
}
