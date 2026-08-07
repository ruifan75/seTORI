package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
)

// 同じ曲名の楽曲が複数あるとき、それが何なのかを AI に説明させる。
//
// 「統合すべきか」は聞かない。実データを見ると、同名の組には少なくとも
// 3 通りの正解がある。
//
//	惑星ループ（Eve / ナユタン星人）    … 同じ曲。artist 欄が原唱と作曲の
//	                                     どちらを記録したかの違い → 統合したい
//	翼をください（赤い鳥 / 桜高軽音部） … 同じ作曲だが編曲が大きく違う。
//	                                     どちらを歌ったかが情報 → 分けたい
//	オレンジ（SPYAIR / 逢坂大河ほか）   … そもそも別の曲 → 分けたい
//
// 「どれだけ違えば分けるか」は編集方針であって、データからも AI からも出てこない。
// 一方「同じ作曲か」「編曲は同系統か」は公開された事実で、AI が答えられる。
//
// そこで **AI には事実の判定だけをさせ、統合するかどうかは人が決める**。
// 統合は破壊的（統合元が消え、歌唱が移る）なので、判定で自動実行はしない。
// 判定は候補行に保存し、同じ組を二度聞かない。

const songDupSystemPrompt = `あなたは楽曲の来歴に詳しいアシスタントです。

同じ曲名で登録されている2曲の組が与えられます。それぞれについて**事実の判定**をしてください。
統合するかどうかを決めるのはあなたではありません。判断材料を出すのがあなたの役割です。

## 判定する項目

- same_composition: **同じ作曲の楽曲か**（作詞作曲が同一で、カバー・アレンジ違い・
  名義違いも含めて「元をたどれば同じ曲」なら true）。
  たまたま曲名が同じだけの別の曲なら false。
- same_arrangement: **編曲が同系統か**。
  原曲とほぼ同じ編成・アレンジなら true。
  ジャンルや編成が大きく変わっていて、聴けば別物と分かるなら false。
  same_composition が false のときは false にしてください。
- role_a / role_b: それぞれの立ち位置を日本語30文字以内で。
  例「原曲（1971年・フォーク）」「アニメ版・バンドアレンジ」「作曲者による本家」「カバー」
- recommendation: 次の規則で機械的に決めてください。
  - same_composition が false → "keep_separate"
  - same_composition が true かつ same_arrangement が false → "keep_separate"
  - same_composition が true かつ same_arrangement が true → "merge"
- why: 判断の理由を日本語50文字以内。**知らない曲なら「情報が無い」と正直に書くこと。**

## 重要

推測で埋めないでください。知らない楽曲・アーティストの場合は
same_composition を null にし、why に「情報が無い」と書いてください。
**自信のある事実だけを答えてください。**間違った判定で楽曲が統合されると、
歌唱記録が別の曲に移って復旧が難しくなります。

## 出力

**"[" で始まり "]" で終わる純粋なJSON配列のみ。**説明文は不要です。

各要素:
{"i":0,"same_composition":true,"same_arrangement":false,
 "role_a":"原曲（1971年・フォーク）","role_b":"K-ON!版・バンドアレンジ",
 "recommendation":"keep_separate","why":"同じ曲だが編曲が大きく異なる"}`

type songDupVerdict struct {
	Index           int    `json:"i"`
	SameComposition *bool  `json:"same_composition"`
	SameArrangement *bool  `json:"same_arrangement"`
	RoleA           string `json:"role_a"`
	RoleB           string `json:"role_b"`
	Recommendation  string `json:"recommendation"`
	Why             string `json:"why"`
}

// AdjudicateDuplicates は未判定の統合候補について AI の見立てを取り、保存する。
// 返り値は判定できた件数。統合の実行はしない。
func (s *SongMatchService) AdjudicateDuplicates(aiClient ai.Chatter, limit int) (int, error) {
	if aiClient == nil {
		return 0, fmt.Errorf("AI プロバイダーが設定されていません")
	}
	pending, err := s.matchRepo.ListUnjudgedCandidates(limit)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString("次の組を判定してください：\n\n")
	for i, c := range pending {
		fmt.Fprintf(&sb, "[%d] 曲名「%s」\n     A: %s\n     B: %s\n",
			i, c.NewSong.Name,
			artistOrUnknown(c.NewSong.OriginalArtist),
			artistOrUnknown(c.ExistingSong.OriginalArtist))
	}

	logger.Infof("AI に同名楽曲の判定を問い合わせます: %d 組", len(pending))
	resp, err := aiClient.SimpleChat(songDupSystemPrompt, sb.String())
	if err != nil {
		return 0, fmt.Errorf("AI 判定に失敗しました: %w", err)
	}

	logger.Debugf("AI dup verdict raw response: %s", resp)

	// 配列の後ろに説明文を付けてくる応答があるので、Unmarshal ではなく
	// Decoder で先頭の配列だけを読む（正規化側と同じ扱い）。
	cleaned := ai.CleanJSONResponse(resp)
	var verdicts []songDupVerdict
	if err := json.NewDecoder(strings.NewReader(cleaned)).Decode(&verdicts); err != nil {
		preview := cleaned
		if len(preview) > 600 {
			preview = preview[:400] + " …[truncated]… " + preview[len(preview)-200:]
		}
		return 0, fmt.Errorf("AI 応答の解析に失敗しました: %w (応答: %s)", err, preview)
	}

	saved := 0
	for _, v := range verdicts {
		if v.Index < 0 || v.Index >= len(pending) {
			continue
		}
		c := pending[v.Index]
		// AI が「知らない」と答えた組は推奨を空にして人に丸投げする。
		// 適当な推奨を出すより、判断材料が無いと分かるほうが役に立つ。
		rec := v.Recommendation
		if v.SameComposition == nil {
			rec = ""
		}
		verdict := repository.MergeVerdict{
			SameComposition: v.SameComposition,
			SameArrangement: v.SameArrangement,
			Recommendation:  rec,
			RoleNew:         v.RoleA,
			RoleExisting:    v.RoleB,
			Note:            v.Why,
			Source:          "ai",
		}
		if err := s.matchRepo.SetMergeVerdict(c.ID, verdict); err != nil {
			logger.Warnf("set merge verdict failed (%s): %v", c.ID, err)
			continue
		}
		saved++
	}
	logger.Infof("同名楽曲の判定を %d 件保存しました", saved)
	return saved, nil
}

// ScanDuplicates は既存データを走査して同名の組を候補に積む。
func (s *SongMatchService) ScanDuplicates() (int, error) {
	return s.matchRepo.ScanDuplicateTitles()
}

func artistOrUnknown(a string) string {
	if strings.TrimSpace(a) == "" {
		return "（アーティスト未記入）"
	}
	return a
}
