package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/songmatch"
)

// 規則で照合できなかった行を AI に回す。二段構えで、どちらを使うかは候補の数で決まる。
//
// 本番の 255 行（GT つき）で両方を実測した結果：
//
//	              候補あり 212 行        候補ゼロ 43 行     input tokens
//	候補を見せる    正解 209 (98.6%)      解けない (0%)      25k
//	楽曲カタログ全体 正解 211 (99.5%)      正解 33 (76.7%)    441k
//
// 誤答はどちらも 0。分からなければ null を返し、無理に近いものを選ばない。
//
// つまり候補があるうちは候補で足りる（18 倍の入力で +0.9% にしかならない）。
// 候補がゼロのときだけ楽曲カタログ全体を見せる ── そこは判断ではなく再現率の問題で、
// 候補の中に答えが無い以上、何を聞いても当たらないため。楽曲カタログでしか解けない例：
//
//	Rumor / Police Piccadilly → ルーマー / ポリスピカデリー
//	Plastic Love / 竹内まりや → プラスティック・ラブ
//	深昏睡 / 春野             → 深昏睡 (Deep coma)
//
// いずれも曲名キーも trigram も一致しないので、候補には決して現れない。
//
// 歌手が違う場合の「同一人物か」も同じ呼び出しで答えさせる。AI は曲を選ぶ時点で
// 両方の歌手名を見ているので、分けて聞くのは往復が 1 回増えるだけになる。

const matchAISystemPrompt = `あなたは楽曲データベースの照合を担当します。
配信のコメントから抽出された曲名・アーティストが、登録済みのどの楽曲を指すかを判定してください。

## 同じ曲とするもの

- 表記の違いだけ（翻訳・ローマ字・副題・記号・クレジットの書き方）
  例: 深昏睡 と 深昏睡 (Deep coma)、Plastic Love と プラスティック・ラブ
- **カバー・歌ってみたでも、編曲が原曲と同じもの**
  誰が歌ったかは別に記録するので、楽曲としては原曲を指す
- アーティスト欄が作曲者と原曲歌手のどちらを書いたかの違い
  例: 惑星ループ / ナユタン星人（作曲者）と 惑星ループ / Eve（原曲歌手）

## 別の曲とするもの

- **編曲・録音が違うもの**（最も重要な軸）
  instrumental / Remix / Reloaded / アコースティック / 大きく編曲し直したカバー
  例: Starry night と Starry night (instrumental)
  例: 翼をください / 赤い鳥 と 翼をください / 桜高軽音部
- 曲名が似ているだけの別の曲
  例: ダーリン と ダーリンダンス、オレンジ / SPYAIR と オレンジ / 逢坂大河

**少しでも確信が持てなければ id を null にしてください。**
null は「新しい曲として登録し、人が確認する」だけで済みます。
誤って結び付けると、歌唱が別の曲にぶら下がったまま誰も気づきません。

## アーティストの同一人物判定

曲が同じで、かつアーティスト名が違うときだけ answer に same_artist を入れてください。

- 同一人物の別名義なら true（例: 松任谷由実 と 荒井由実、中島愛 と ランカ・リー）
- 別人なら false（例: 作曲者 ryo (supercell) と原曲歌手の初音ミク、カバーした VTuber と原曲歌手）
- 判断できなければ省略

## 出力

JSON 配列のみ。説明文を付けないこと。
各要素: {"i":番号,"id":"曲のid" または null,"conf":0.0〜1.0,"why":"30字以内の理由","same_artist":true/false}`

// aiMatchRow は AI に回す 1 行。Answer は判定後に埋まる。
type aiMatchRow struct {
	Name       string // 照合に使った曲名（正規化後、無ければ抽出のまま）
	Artist     string
	Candidates []MatchCandidate

	SongID      *uuid.UUID // AI が選んだ楽曲
	Confidence  float64
	Why         string
	SameArtist  *bool
	AliasTarget string // same_artist=true のとき、DB 側の歌手名（別名義の本体）
}

// aiMatchVerdict は AI の応答 1 件。
type aiMatchVerdict struct {
	Index      int     `json:"i"`
	ID         *string `json:"id"`
	Confidence float64 `json:"conf"`
	Why        string  `json:"why"`
	SameArtist *bool   `json:"same_artist"`
}

// aiMatchBatchSize は 1 回の問い合わせに載せる行数。
//
// 8 行で実測 0 誤答。大きくしてもよいが、失敗は**無音**なので欲張らない
// ── 行番号がずれても応答は正常に見えるため、間違いに気づく手立てが無い。
const aiMatchBatchSize = 8

// AdjudicateMatches は未照合の行を AI に判定させる。
// 戻り値は (実際に AI へ送った行数, 照合できた行数)。
//
// 呼ぶのは「入力元を編集フォームへ読み込む」ときだけ。配信を開いただけの読み取りや
// 一括プレ分析からは呼ばない（誰も見ていない配信のために AI を焚かない）。
func (s *NormalizationService) AdjudicateMatches(rows []*aiMatchRow) (asked, resolved int) {
	if s.matchService == nil || s.aiClient == nil || len(rows) == 0 {
		return 0, 0
	}

	pending := s.dropRejected(rows)
	if len(pending) == 0 {
		return 0, 0
	}

	// 候補の有無で二手に分ける。楽曲カタログを見せるのは候補ゼロの行だけ。
	var withCands, noCands []*aiMatchRow
	for _, r := range pending {
		if len(r.Candidates) > 0 {
			withCands = append(withCands, r)
		} else {
			noCands = append(noCands, r)
		}
	}

	asked = len(withCands)
	resolved += s.askInBatches(withCands, s.buildCandidatePrompt)
	if len(noCands) > 0 {
		catalog, err := s.matchService.CatalogForAI()
		if err != nil {
			logger.Warnf("load catalog for AI failed: %v", err)
		} else if catalog.Len() > 0 {
			asked += len(noCands)
			resolved += s.askInBatches(noCands, func(batch []*aiMatchRow) (string, map[string]uuid.UUID) {
				return catalog.Prompt(batch), catalog.IDs
			})
		}
	}
	return asked, resolved
}

// askInBatches は行を小分けにして問い合わせる。
//
// **行番号は呼び出しごとに 0 から振り直し、その呼び出しの中でだけ解釈する。**
// 通し番号を使うと、モデルがバッチ内で振り直したときに別のバッチの答えを
// 上書きしてしまう。実測でこれが起き、8 行すべて正解していたバッチが
// 丸ごと誤答として記録された。しかも応答自体は正常な形をしているので気づけない。
func (s *NormalizationService) askInBatches(rows []*aiMatchRow, build func([]*aiMatchRow) (string, map[string]uuid.UUID)) int {
	resolved := 0
	for off := 0; off < len(rows); off += aiMatchBatchSize {
		end := off + aiMatchBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[off:end]

		prompt, ids := build(batch)
		resp, err := s.aiClient.SimpleChat(matchAISystemPrompt, prompt)
		if err != nil {
			// 判定できなくても照合は規則の結果のまま進む。人に回るだけ。
			logger.Warnf("AI match adjudication failed: %v", err)
			continue
		}

		var verdicts []aiMatchVerdict
		if err := json.Unmarshal([]byte(ai.CleanJSONResponse(resp)), &verdicts); err != nil {
			logger.Warnf("AI match response parse failed: %v (resp=%.200s)", err, resp)
			continue
		}
		resolved += s.applyVerdicts(batch, verdicts, ids)
	}
	return resolved
}

// applyVerdicts は応答を行へ書き戻す。範囲外の番号は捨てる。
func (s *NormalizationService) applyVerdicts(batch []*aiMatchRow, verdicts []aiMatchVerdict, ids map[string]uuid.UUID) int {
	resolved := 0
	for _, v := range verdicts {
		if v.Index < 0 || v.Index >= len(batch) {
			logger.Warnf("AI match: 範囲外の行番号 %d（バッチ %d 行）を無視します", v.Index, len(batch))
			continue
		}
		row := batch[v.Index]
		if v.ID == nil || *v.ID == "" {
			// 「この曲ではない」は残す。残さないと、当たらない組に毎回 AI を呼ぶ。
			s.recordSongRejections(row, "ai", v.Why)
			continue
		}
		songID, ok := ids[*v.ID]
		if !ok {
			logger.Warnf("AI match: 未知の曲 id %q を無視します", *v.ID)
			continue
		}
		row.SongID = &songID
		row.Confidence = v.Confidence
		row.Why = v.Why
		row.SameArtist = v.SameArtist
		resolved++

		if v.SameArtist != nil && !*v.SameArtist {
			// 「別人」も残す。次の読み込みで同じ提案を繰り返さないため。
			s.recordArtistRejection(row, songID, v.Why)
		}
	}
	return resolved
}

// buildCandidatePrompt は候補だけを見せる問いを作る。
func (s *NormalizationService) buildCandidatePrompt(batch []*aiMatchRow) (string, map[string]uuid.UUID) {
	ids := map[string]uuid.UUID{}
	var sb strings.Builder
	sb.WriteString("次の各行について、候補の中に同じ曲があればその id を、無ければ null を返してください。\n\n")
	for i, r := range batch {
		fmt.Fprintf(&sb, "%d. 曲名「%s」 アーティスト「%s」\n", i, r.Name, orDash(r.Artist))
		for _, c := range r.Candidates {
			key := c.Song.ID.String()
			ids[key] = c.Song.ID
			fmt.Fprintf(&sb, "   - %s|%s|%s\n", key, c.Song.Name, c.Song.OriginalArtist)
		}
	}
	return sb.String(), ids
}

// dropRejected は「別の曲」と記録済みの候補を落とし、
// 候補がすべて消えた行は楽曲カタログ側へ回す（候補ゼロとして扱われる）。
func (s *NormalizationService) dropRejected(rows []*aiMatchRow) []*aiMatchRow {
	var keys []string
	for _, r := range rows {
		nameKey, artistKey := songmatch.TitleKey(r.Name), songmatch.ParseArtist(r.Artist).String()
		for _, c := range r.Candidates {
			keys = append(keys, repository.SongIdentityPairKey(nameKey, artistKey, c.Song.ID))
		}
	}
	rejected, err := s.matchService.RejectedSongPairs(keys)
	if err != nil {
		logger.Warnf("song rejection lookup failed: %v", err)
		return rows
	}
	if len(rejected) == 0 {
		return rows
	}
	for _, r := range rows {
		nameKey, artistKey := songmatch.TitleKey(r.Name), songmatch.ParseArtist(r.Artist).String()
		var keep []MatchCandidate
		for _, c := range r.Candidates {
			if !rejected[repository.SongIdentityPairKey(nameKey, artistKey, c.Song.ID)] {
				keep = append(keep, c)
			}
		}
		r.Candidates = keep
	}
	return rows
}

func (s *NormalizationService) recordSongRejections(row *aiMatchRow, source, note string) {
	nameKey, artistKey := songmatch.TitleKey(row.Name), songmatch.ParseArtist(row.Artist).String()
	for _, c := range row.Candidates {
		if err := s.matchService.RecordSongRejection(nameKey, artistKey, c.Song.ID, source, note); err != nil {
			logger.Warnf("record song rejection failed: %v", err)
		}
	}
}

func (s *NormalizationService) recordArtistRejection(row *aiMatchRow, songID uuid.UUID, note string) {
	song, err := s.matchService.FindSong(songID)
	if err != nil || song == nil {
		return
	}
	keyA := songmatch.ParseArtist(row.Artist).Primary
	keyB := songmatch.ParseArtist(song.OriginalArtist).Primary
	if keyA == "" || keyB == "" || keyA == keyB {
		return
	}
	if err := s.matchService.RecordArtistRejection(keyA, keyB, "ai", note); err != nil {
		logger.Warnf("record artist rejection failed: %v", err)
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（記載なし）"
	}
	return s
}
