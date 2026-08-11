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

// 全件走査の system prompt。
//
// 曲名キーで束ねる走査は**同じキーの組しか見つけられない**。
// `ホール・ニュー・ワールド` と `A Whole New World` はキーが完全に別なので、
// どれだけ走査しても同じ組に入らず、判定の AI にも届かなかった。
// 文字列の側で寄せ方を工夫するより、一覧を丸ごと渡して選ばせるほうが確実で単純。
//
// 入力は大きいが（本番 819 曲で約 13k トークン）出力は数組しかない。
// 入力は出力より一桁安く、この走査は滅多に回さないので割に合う。
const dupScanSystemPrompt = `あなたは楽曲データベースの重複検出を助けるアシスタントです。
番号つきの登録曲一覧を渡します。**同じ楽曲が別々に登録されている組**を挙げてください。

同じ楽曲とみなすもの:
- 表記の違いだけ（翻訳・ローマ字・副題・記号・送り仮名・誤字）
  例: "ルーマー" と "Rumor"、"創聖のアクエリオン" と "創生のアクエリオン"
- アーティスト欄が作曲者と原唱のどちらを書いたかの違い
- カバー・歌ってみたでも編曲が原曲と同じもの

**別の楽曲**とみなすもの（挙げてはいけない）:
- 編曲・録音が違う（instrumental / Remix / Reloaded / アコースティック / 和風アレンジ）
- 曲名が似ているだけの別の曲（"ダーリン" と "ダーリンダンス"）
- 同名だが別の曲（"オレンジ" の SPYAIR 版と 逢坂大河 版）

確信が持てない組は挙げないでください。挙げても統合は実行されず、人のレビューに回ります。

JSON配列のみ。説明文を付けないこと。
各要素: {"a":番号,"b":番号,"why":"30字以内の理由"}`

type dupScanPair struct {
	A   int    `json:"a"`
	B   int    `json:"b"`
	Why string `json:"why"`
}

// ScanDuplicatesWithAI は登録曲を丸ごと AI に見せ、重複している組を候補に積む。
//
// 曲名キーの走査（ScanDuplicates）と役割が違う。あちらは同じキーの組を確実に拾うが、
// キーが違う組（邦題と原題、誤字、ローマ字）は原理的に見つけられない。
// ここはその穴を埋める。両方を回すのが前提。
//
// 統合は実行しない。候補として積み、AI の理由を verdict として添えるだけ。
// 同名の組には「統合すべき」「編曲違いで分けるべき」「そもそも別の曲」が混在し、
// その線引きは編集方針なので人が決める。
func (s *SongMatchService) ScanDuplicatesWithAI(aiClient ai.Chatter) (int, error) {
	if aiClient == nil {
		return 0, fmt.Errorf("AI プロバイダーが設定されていません")
	}
	songs, err := s.matchRepo.ListAllForScan()
	if err != nil {
		return 0, err
	}
	if len(songs) < 2 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString("## 登録曲一覧\n\n")
	for i, sg := range songs {
		fmt.Fprintf(&sb, "%d\t%s / %s\n", i, sg.Name, artistOrUnknown(sg.OriginalArtist))
	}
	logger.Infof("[dup] AI に全件走査を依頼します: %d 曲", len(songs))

	resp, err := aiClient.SimpleChat(dupScanSystemPrompt, sb.String())
	if err != nil {
		return 0, fmt.Errorf("AI 呼び出しに失敗しました: %w", err)
	}
	var pairs []dupScanPair
	if err := json.Unmarshal([]byte(ai.CleanJSONResponse(resp)), &pairs); err != nil {
		logger.Warnf("[dup] 全件走査の応答を解析できません: %v (resp=%.200s)", err, resp)
		return 0, fmt.Errorf("AI の応答を解析できませんでした")
	}

	added := 0
	for _, p := range pairs {
		if p.A < 0 || p.A >= len(songs) || p.B < 0 || p.B >= len(songs) || p.A == p.B {
			continue
		}
		a, b := songs[p.A], songs[p.B]
		ok, err := s.matchRepo.RecordScanCandidate(a.ID, b.ID, 0.0, "ai_scan")
		if err != nil {
			logger.Warnf("[dup] 候補の記録に失敗: %v", err)
			continue
		}
		if !ok {
			continue // 既にある組（却下済みを含む）は蒸し返さない
		}
		added++
		logger.Infof("[dup] AI が重複を検出: %q / %q ↔ %q / %q（%s）",
			a.Name, a.OriginalArtist, b.Name, b.OriginalArtist, p.Why)
	}
	logger.Infof("[dup] 全件走査で %d 組を追加しました", added)
	return added, nil
}
