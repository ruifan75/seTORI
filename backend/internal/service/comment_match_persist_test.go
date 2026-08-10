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
	name, artist := "Starry night", "稀羽すう"
	s := dto.CommentSong{
		// 曲名: 抽出 → 正規化でタグが落ちる。照合先とは同じ文字列なので db_match は記録しない
		Name: "Starry night (Acoustic)", NormalizedName: "Starry night",
		// アーティスト: AI は埋めなかったので抽出のまま照合へ行き、そこで表記が直る
		OriginalArtist: "きうすう",
		MatchedSongID:  &name, MatchedSongName: &name, MatchedSongArtist: &artist,
	}
	got := buildFieldChanges(s, ReasonExact, 1.0)

	var byStep []string
	for _, c := range got {
		byStep = append(byStep, c.Field+":"+c.By)
	}
	want := map[string]bool{"name:ai_normalize": true, "artist:db_match": true}
	for _, k := range byStep {
		if !want[k] {
			t.Errorf("余計な変更を記録している: %s", k)
		}
		delete(want, k)
	}
	for k := range want {
		t.Errorf("記録されていない変更がある: %s", k)
	}

	// 留言に歌手が書かれておらず、照合で埋まった場合も記録すること。
	// ここを落とすと、画面に理由なく歌手名が現れたように見える。
	empty := dto.CommentSong{
		Name: "Starry night", OriginalArtist: "",
		MatchedSongID: &name, MatchedSongName: &name, MatchedSongArtist: &artist,
	}
	var found bool
	for _, c := range buildFieldChanges(empty, ReasonTitleOnly, 0.8) {
		if c.Field == "artist" && c.By == "db_match" && c.From == "" && c.To == artist {
			found = true
		}
	}
	if !found {
		t.Error("未記入 → 照合で補完 が記録されていない")
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
	if asked, linked := (&NormalizationService{}).AdjudicateAliasesForCommentSongs(songs); asked != 0 || linked != 0 {
		t.Errorf("aiClient も matchService も無いのに asked=%d linked=%d。AI を呼ばない経路で呼びに行っている", asked, linked)
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
	if asked, _ := (&NormalizationService{}).AdjudicateAliasesForCommentSongs(done); asked != 0 {
		t.Errorf("照合済みの行まで問い合わせ対象にしている（asked=%d）", asked)
	}

	// 連名は「どの名前とどの名前を比べるのか」が定まらないので聞かない
	multi := []dto.SongMatchCandidate{
		{SongID: "s2", Artist: "逢坂大河, 櫛枝実乃梨 & 川嶋亜美", Score: 0.60, Reason: ReasonTitleMismatch},
	}
	if p := collectArtistAliasPairsFromCandidates("釘宮理恵・堀江由衣・喜多村英梨", multi); len(p) != 0 {
		t.Errorf("連名の組を %d 件拾っている。問いが曖昧になるので対象外のはず", len(p))
	}
}
