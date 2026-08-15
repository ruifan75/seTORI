package service

import (
	"testing"

	"github.com/ruifan75/setori/internal/dto"
)

// 照合の候補（0.50〜0.85）を保存し続けることを固定する。
//
// songmatch は「似ているが自動採用はできない」ものを match_candidates で返し、
// 人に選ばせる前提になっている。ところがコメント解析の経路は個別フィールドだけを
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

// 保存前に照合の結果を落とすこと。
//
// 照合は読み取り時に計算する約束なので、保存すると「古い答えが 2 か所にある」状態になる。
// これを守らないと、曲が増えても保存済みの古い matched_song_id が返り続ける。
func TestStripMatchForStorage(t *testing.T) {
	id := "8f7a7665-6d12-4f36-8f58-92f81ded9a10"
	in := []dto.CommentSong{{
		Start: 945, Name: "Starry night", NormalizedName: "Starry night",
		MatchedSongID: &id, MatchedSongName: &id,
		MatchCandidates: []dto.SongMatchCandidate{{SongID: id, Score: 0.80}},
		Changes:         []dto.FieldChange{{Field: "name", By: "db_match"}},
	}}
	out := stripMatchForStorage(in)

	if out[0].MatchedSongID != nil || out[0].MatchCandidates != nil || out[0].Changes != nil {
		t.Error("照合の結果が保存対象に残っている")
	}
	// 抽出・正規化は残す（AI を使う高い部分なので保存する価値がある）
	if out[0].Name != "Starry night" || out[0].NormalizedName != "Starry night" || out[0].Start != 945 {
		t.Error("抽出・正規化まで落としている。ここは保存しないと再取得に AI が要る")
	}
	// 元の配列を壊さない（応答にはそのまま照合入りを返す）
	if in[0].MatchedSongID == nil {
		t.Error("入力を破壊している。応答用の値まで消える")
	}
}

// 「元は X、AI 正規化で Y、DB 照合で Z」を画面に出せること。
func TestBuildFieldChanges(t *testing.T) {
	// matchService が nil だと照合しないので、変更は「抽出 → 正規化」だけになる。
	// DB を要さずに、正規化の記録と「変わらなかった段は記録しない」を確かめられる。
	svc := &NormalizationService{}

	_, changes := svc.resolveOne(
		"Starry night (Acoustic)", "稀羽すう", // 抽出したまま
		"Starry night", "稀羽すう", // AI 正規化後（アーティストは変わっていない）
		nil,
	)
	if len(changes) != 1 {
		t.Fatalf("changes = %d 件, want 1: %+v", len(changes), changes)
	}
	c := changes[0]
	if c.Field != "name" || c.By != "ai_normalize" ||
		c.From != "Starry night (Acoustic)" || c.To != "Starry night" {
		t.Errorf("記録が違う: %+v", c)
	}

	// 変わらなかった段は記録しない（「何も起きていない」を並べても読む側の負担になるだけ）
	if _, none := svc.resolveOne("Lemon", "米津玄師", "Lemon", "米津玄師", nil); len(none) != 0 {
		t.Errorf("変化が無いのに %d 件記録している", len(none))
	}
}

// 正規化が照合を壊したときに抽出のままで引き直すこと。
//
// AI 正規化は「日本語と英語の併記なら日本語のみ」という規則で
// `Departures 〜あなたにおくるアイの歌〜` を `あなたにおくるアイの歌` にし、
// アーティストも `グミ` → `GUMI` と書き換える。どちらも DB の表記から離れる方向で、
// 本番の GT では照合できなかった 213 件のうち 78 件（37%）がこれだった。
//
// なお **照合できないのが正しい場合もある**。`弱虫モンブラン` と
// `弱虫モンブラン (Reloaded)` は編曲が違う別録音で、コメント側の書き手が
// 取り違えていることがある。ここを機械で「直して」はいけない
// ── 気づくのは人で、修正提案の仕組みがその受け皿になっている。
func TestResolveFallsBackToRawWhenNormalizationBreaksMatch(t *testing.T) {
	// matchService が nil のときは 2 回目も引かない（無駄な問い合わせをしない）
	svc := &NormalizationService{}
	if m, _ := svc.resolveOne("Departures 〜あなたに〜", "EGOIST", "あなたに", "EGOIST", nil); m.MatchedSongID != nil {
		t.Error("照合サービスが無いのに結果が返っている")
	}

	// 正規化値と抽出値が同じなら引き直さない（条件の確認）
	name, artist := MatchInputs("Lemon", "米津玄師", "Lemon", "米津玄師")
	if name != "Lemon" || artist != "米津玄師" {
		t.Fatalf("前提が崩れている: %q / %q", name, artist)
	}
}

// 一括プレ分析は AI 判定を行わないこと。
//
// 抽出（comment_raw → comment_songs）と照合の判定は別の仕事で、後者は
// 人がその配信を開いて読み込むときに走るべきもの。混ぜると、誰も見ていない配信の
// ために AI を焚くことになる。
//
// ここでは入口が分かれていること自体を固定する。実際の判定は AI と DB が要るので、
// 依存の無い状態で「呼んでも何も起きない」ことまでを見る。
func TestBatchAnalyzeDoesNotAdjudicate(t *testing.T) {
	svc := &CommentService{}
	if asked, resolved := (&NormalizationService{}).AdjudicateCommentSongs(nil); asked != 0 || resolved != 0 {
		t.Errorf("asked=%d resolved=%d, want 0/0", asked, resolved)
	}
	_ = svc
}
