package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/ruifan75/setori/internal/dto"
)

// AI が使えないとき、照合がどこまで生きているかを固定する。
//
// 照合の本体（曲名キー・アーティストのトークン照合・学習済みの別表記）は
// 規則ベースで AI に依存しない。AI が担うのは「新しい別名義を見つける」ことだけで、
// これが止まっても既に登録済みの別名義は効き続ける。
//
// ここが崩れると、プロバイダーが全滅した日にすべての曲が新規登録され、
// 近似重複が一気に増える。

// countingAI は呼ばれた回数を数える Chatter。
type countingAI struct {
	calls    int
	response string
	err      error
}

func (c *countingAI) SimpleChat(_, _ string) (string, error) {
	c.calls++
	if c.err != nil {
		return "", c.err
	}
	return c.response, nil
}

// AI 呼び出しが失敗しても、正規化は元データで結果を返し、警告を添える。
// このとき matchService は nil でも落ちないこと（照合できないだけ）。
func TestBatchAINormalization_AIDownStillReturnsItems(t *testing.T) {
	ai := &countingAI{err: errors.New("all providers cooled down")}
	svc := &NormalizationService{aiClient: ai}

	resp, err := svc.BatchAINormalization([]dto.AINormalizationItem{
		{Name: "少女レイ", OriginalArtist: "みきとP"},
		{Name: "ひこうき雲", OriginalArtist: "松任谷由実"},
	})
	if err != nil {
		t.Fatalf("AI が落ちても正規化はエラーにしない: %v", err)
	}
	if len(resp.Suggestions) != 2 {
		t.Fatalf("got %d suggestions, want 2（入力は素通しで返す）", len(resp.Suggestions))
	}
	if resp.Warning == "" {
		t.Error("AI 失敗が warning に出ていない。無言で劣化させてはいけない")
	}
	// 素通し＝入力がそのまま返る
	if resp.Suggestions[0].NormalizedName != "少女レイ" {
		t.Errorf("NormalizedName = %q, want 素通しの %q", resp.Suggestions[0].NormalizedName, "少女レイ")
	}
}

// 正規化の AI が失敗しているのに、別名義の判定でもう一度 AI を叩かないこと。
// 同じ理由で必ず失敗するので、待ち時間が延びるだけになる。
func TestBatchAINormalization_NoSecondAICallWhenAIIsDown(t *testing.T) {
	ai := &countingAI{err: errors.New("all providers cooled down")}
	svc := &NormalizationService{aiClient: ai}

	if _, err := svc.BatchAINormalization([]dto.AINormalizationItem{
		{Name: "ひこうき雲", OriginalArtist: "松任谷由実"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ai.calls != 1 {
		t.Errorf("AI 呼び出し %d 回。失敗が分かっている以上 1 回で止めるべき", ai.calls)
	}
}

// AI の応答が壊れていても同じ（解析失敗は呼び出し失敗と同じ扱い）。
func TestBatchAINormalization_BrokenResponseDoesNotRetryAI(t *testing.T) {
	ai := &countingAI{response: "これはJSONではありません"}
	svc := &NormalizationService{aiClient: ai}

	resp, err := svc.BatchAINormalization([]dto.AINormalizationItem{
		{Name: "ひこうき雲", OriginalArtist: "松任谷由実"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Warning == "" {
		t.Error("解析失敗が warning に出ていない")
	}
	if ai.calls != 1 {
		t.Errorf("AI 呼び出し %d 回、want 1", ai.calls)
	}
}

// 照合サービスが無い状態でも落ちないこと（AI とは別軸の耐性）。
func TestMatchAndPopulateSong_NilMatchServiceIsSafe(t *testing.T) {
	svc := &NormalizationService{}
	var res dto.AISuggestionResult
	item := dto.AINormalizationItem{Name: "少女レイ", OriginalArtist: "みきとP"}

	svc.matchAndPopulateSong(&res, &item, item.Name, item.OriginalArtist)

	if res.MatchedSongID != nil {
		t.Error("照合サービスが無いのにマッチしている")
	}
	if len(res.MatchCandidates) != 0 {
		t.Error("照合サービスが無いのに候補が入っている")
	}
}

// AI が別名義を 1 件も確定できなかったら、照合はやり直さない（元の結果のまま）。
func TestResolveAliases_NoLinkKeepsOriginalMatches(t *testing.T) {
	ai := &countingAI{response: `[{"i":0,"same":false,"why":"別人"}]`}
	svc := &NormalizationService{aiClient: ai}

	// matchService が nil なので resolveAliasesAndRematch は即 return する。
	// ここで落ちないことと、AI が余計に呼ばれないことを確認する。
	sugs := []dto.AISuggestionResult{{
		Index:          0,
		OriginalArtist: "松任谷由実",
		MatchCandidates: []dto.SongMatchCandidate{
			{Artist: "荒井由実", Reason: ReasonTitleMismatch, Score: 0.6},
		},
	}}
	svc.resolveAliasesAndRematch([]dto.AINormalizationItem{{Name: "ひこうき雲"}}, sugs)

	if ai.calls != 0 {
		t.Errorf("照合サービスが無いのに AI を %d 回呼んでいる", ai.calls)
	}
	if len(sugs[0].MatchCandidates) != 1 {
		t.Error("元の候補が消えている")
	}
}

// 別名義の問い合わせ対象を集める段階は AI に触れない。
// AI が死んでいても、規則で拾えるものは拾える形になっているかの確認。
func TestCollectPairs_IsPureAndAIFree(t *testing.T) {
	sug := dto.AISuggestionResult{
		OriginalArtist: "松任谷由実",
		MatchCandidates: []dto.SongMatchCandidate{
			{Artist: "荒井由実", Reason: ReasonTitleMismatch},
			{Artist: "YOASOBI", Reason: ReasonTitleAmbiguous},
		},
	}
	pairs := collectArtistAliasPairsFromDTO(sug)
	if len(pairs) != 1 {
		t.Fatalf("got %d pairs, want 1（title_mismatch だけが対象）", len(pairs))
	}
	if !strings.Contains(pairs[0].DisplayB, "荒井由実") {
		t.Errorf("pair = %+v", pairs[0])
	}
}
