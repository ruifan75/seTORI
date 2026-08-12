package service

import (
	"errors"
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

// 照合サービスが無ければ AI を呼ばない。
//
// 未照合の行を AI に回す入口は、依存が欠けているときに黙って素通りすること。
// ここが落ちると、AI が使えない環境で読み込みごと失敗する。
func TestAdjudicate_NoMatchServiceIsAIFree(t *testing.T) {
	ai := &countingAI{response: `[{"i":0,"id":null}]`}
	svc := &NormalizationService{aiClient: ai}

	songs := []dto.CommentSong{{Name: "ひこうき雲", OriginalArtist: "松任谷由実"}}
	if asked, resolved := svc.AdjudicateCommentSongs(songs); asked == 0 && resolved != 0 {
		t.Errorf("resolved=%d, want 0", resolved)
	}
	if ai.calls != 0 {
		t.Errorf("照合サービスが無いのに AI を %d 回呼んでいる", ai.calls)
	}
	if songs[0].MatchedSongID != nil {
		t.Error("照合できていないのに結果が入っている")
	}
}

// AI が項目を返しても曲名が空のことがある。そのまま信じると空文字で照合しにいき、
// 必ず外れる ── 本番の実測で、照合できなかった 2262 件のうち 1876 件（82.9%）が
// この状態だった（照合できた 4910 件では 0 件）。空なら抽出時の名前へ落とすこと。
func TestBatchAINormalization_EmptyNormalizedNameFallsBackToRaw(t *testing.T) {
	// AI は項目を返すが normalized_name が空。artist も空にして両方を確かめる。
	ai := &countingAI{response: `{"suggestions":[{"index":0,"normalized_name":"","original_artist":"","confidence":0.95}]}`}
	svc := &NormalizationService{aiClient: ai}

	resp, err := svc.BatchAINormalization([]dto.AINormalizationItem{
		{Name: "A Whole New World", OriginalArtist: "アラジン"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Suggestions) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(resp.Suggestions))
	}

	got := resp.Suggestions[0]
	if got.NormalizedName != "A Whole New World" {
		t.Errorf("NormalizedName = %q, want 抽出時の %q（空を素通しすると照合が必ず外れる）",
			got.NormalizedName, "A Whole New World")
	}
	if got.OriginalArtist != "アラジン" {
		t.Errorf("OriginalArtist = %q, want 抽出時の %q", got.OriginalArtist, "アラジン")
	}
}

// AI が正しく正規化した場合は、その結果を尊重する（上の fallback で潰さないこと）。
func TestBatchAINormalization_KeepsAINormalizedName(t *testing.T) {
	ai := &countingAI{response: `{"suggestions":[{"index":0,"normalized_name":"ひこうき雲","original_artist":"松任谷由実","confidence":0.9}]}`}
	svc := &NormalizationService{aiClient: ai}

	resp, err := svc.BatchAINormalization([]dto.AINormalizationItem{
		{Name: "ひこうき雲(cover)", OriginalArtist: "荒井由実"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Suggestions[0].NormalizedName != "ひこうき雲" {
		t.Errorf("NormalizedName = %q, want AI の結果 %q", resp.Suggestions[0].NormalizedName, "ひこうき雲")
	}
	if resp.Suggestions[0].OriginalArtist != "松任谷由実" {
		t.Errorf("OriginalArtist = %q, want AI の結果 %q", resp.Suggestions[0].OriginalArtist, "松任谷由実")
	}
}
