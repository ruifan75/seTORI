package service

import (
	"testing"

	"github.com/ruifan75/setori/internal/dto"
)

// 照合の候補（0.50〜0.85）を保存し続けることを固定する。
//
// songmatch は「似ているが自動採用はできない」ものを match_candidates で返し、
// 人に選ばせる前提になっている。ところが留言解析の経路は個別フィールドだけを
// コピーしていて候補を写していなかったため、本番の comment_songs は
// match_candidates が全件 0 件だった（未照合 2220 件のうち 203 件は候補を持っていた）。
// 「決められないので人に見せる」ぶんが、保存の段階で毎回捨てられていた。

func TestMatchInputsFallsBackToExtraction(t *testing.T) {
	// AI が空の曲名を返した行（a0973fb 以前のデータ）。抽出名に落ちること。
	name, artist := matchInputs(dto.CommentSong{Name: "Starry night", OriginalArtist: "稀羽すう"})
	if name != "Starry night" || artist != "稀羽すう" {
		t.Errorf("matchInputs = (%q, %q), want 抽出値へのフォールバック", name, artist)
	}

	// 正規化済みならそちらが優先される
	name, artist = matchInputs(dto.CommentSong{
		Name: "Starry night (Acoustic Ver.)", OriginalArtist: "きうすう",
		NormalizedName: "Starry night", NormalizedArtist: "稀羽すう",
	})
	if name != "Starry night" || artist != "稀羽すう" {
		t.Errorf("matchInputs = (%q, %q), want 正規化値", name, artist)
	}

	// 片方だけ空でも、空いている側だけが落ちる
	name, artist = matchInputs(dto.CommentSong{
		Name: "生の曲名", OriginalArtist: "生のアーティスト", NormalizedName: "正規化した曲名",
	})
	if name != "正規化した曲名" || artist != "生のアーティスト" {
		t.Errorf("matchInputs = (%q, %q), want 曲名のみ正規化値", name, artist)
	}
}

// 候補だけが変わったときも「変化あり」と判定すること。
// ここを見落とすと、候補を書き戻すようにしても保存されず DB は空のまま残る。
func TestReresolveMatchesDetectsCandidateChange(t *testing.T) {
	// matchService が nil の NormalizationService は候補を返さない。
	// 保存済みの候補が消える方向の変化を再現できる。
	svc := &CommentService{normalizationService: &NormalizationService{}}

	songs := []dto.CommentSong{{
		Name: "Starry night",
		MatchCandidates: []dto.SongMatchCandidate{
			{SongID: "8f7a7665-6d12-4f36-8f58-92f81ded9a10", Name: "Starry night", Score: 0.80, Reason: ReasonTitleOnly},
		},
	}}

	if !svc.reresolveMatches(songs) {
		t.Error("候補が消えたのに changed=false。保存されず古い候補が残る")
	}
	if len(songs[0].MatchCandidates) != 0 {
		t.Errorf("MatchCandidates = %d 件, want 0（再解決の結果で置き換わること）", len(songs[0].MatchCandidates))
	}

	// 2 回目は既に一致しているので変化なし＝無駄な書き込みをしない
	if svc.reresolveMatches(songs) {
		t.Error("同じ結果なのに changed=true。毎回 UPDATE が走る")
	}
}

func TestCandidateSigDistinguishesScoreChange(t *testing.T) {
	id := "8f7a7665-6d12-4f36-8f58-92f81ded9a10"
	low := []dto.SongMatchCandidate{{SongID: id, Score: 0.80}}
	high := []dto.SongMatchCandidate{{SongID: id, Score: 0.95}}
	if candidateSig(low) == candidateSig(high) {
		t.Error("点数が動いても同じ signature になっている。別名義の登録で確信度が上がった変化を取り逃す")
	}
	if candidateSig(nil) != "" {
		t.Error("候補なしは空文字であるべき（omitempty で消える状態と一致させる）")
	}
	if candidateSig(low) != candidateSig([]dto.SongMatchCandidate{{SongID: id, Score: 0.80}}) {
		t.Error("同じ内容が別の signature になっている。毎回 changed 扱いで書き込みが走る")
	}
}
