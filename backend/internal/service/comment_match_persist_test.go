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

// 統合経路（grouped）からも別名義の AI 判定が呼べることを固定する。
//
// 2026-08-07 に既定を統合経路へ切り替え、翌 08-08 に別名義の AI 判定を
// BatchAINormalization の中へ追加した。統合経路はそこを通らないので、
// **判定は追加された時点から一度も実行されていなかった**
// （本番の artist_alias_checks が 0 件だった理由）。
//
// ここで見るのは「候補から問い合わせ対象の組を拾えるか」。実際の AI 呼び出しは
// aiClient / matchService が nil なら何もせず false を返す ── その素通りも一緒に固定する。
func TestAdjudicateAliasesForCommentSongs(t *testing.T) {
	songs := []dto.CommentSong{{
		Name: "Catch You Catch Me", OriginalArtist: "GUMI",
		MatchCandidates: []dto.SongMatchCandidate{
			{SongID: "s1", Name: "Catch You Catch Me", Artist: "グミ", Score: 0.60, Reason: ReasonTitleMismatch},
		},
	}}

	// 依存が無ければ黙って false（照合は規則ベースのまま残る）
	if (&NormalizationService{}).AdjudicateAliasesForCommentSongs(songs) {
		t.Error("aiClient も matchService も無いのに true。AI を呼ばない経路で呼びに行っている")
	}

	// 問い合わせ対象の組を候補から拾えること
	pairs := collectArtistAliasPairsFromCandidates("GUMI", songs[0].MatchCandidates)
	if len(pairs) != 1 {
		t.Fatalf("pairs = %d, want 1（title_mismatch の単独名義どうしは問い合わせ対象）", len(pairs))
	}
	if pairs[0].DisplayA != "GUMI" || pairs[0].DisplayB != "グミ" {
		t.Errorf("pair = %q / %q, want GUMI / グミ", pairs[0].DisplayA, pairs[0].DisplayB)
	}

	// 既に自動採用できている行は問い合わせない
	matched := "s1"
	done := []dto.CommentSong{{
		Name: "Catch You Catch Me", OriginalArtist: "GUMI", MatchedSongID: &matched,
		MatchCandidates: songs[0].MatchCandidates,
	}}
	if (&NormalizationService{}).AdjudicateAliasesForCommentSongs(done) {
		t.Error("照合済みの行まで問い合わせ対象にしている")
	}

	// 連名は「どの名前とどの名前を比べるのか」が定まらないので聞かない
	multi := []dto.SongMatchCandidate{
		{SongID: "s2", Artist: "逢坂大河, 櫛枝実乃梨 & 川嶋亜美", Score: 0.60, Reason: ReasonTitleMismatch},
	}
	if p := collectArtistAliasPairsFromCandidates("釘宮理恵・堀江由衣・喜多村英梨", multi); len(p) != 0 {
		t.Errorf("連名の組を %d 件拾っている。問いが曖昧になるので対象外のはず", len(p))
	}
}
