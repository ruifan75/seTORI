package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/songmatch"
)

// 未照合の行を AI に回す仕組み。ここで固定するのは、AI を呼ぶ前後の約束ごと。

// 依存が無いときは黙って何もしない。
// 照合は規則の結果のまま残り、決着しない行は人に回る。
func TestAdjudicate_NoDepsIsNoop(t *testing.T) {
	svc := &NormalizationService{}
	songs := []dto.CommentSong{{Name: "深昏睡"}}
	if asked, resolved := svc.AdjudicateCommentSongs(songs); asked != 0 || resolved != 0 {
		t.Errorf("asked=%d resolved=%d, want 0/0（AI も照合サービスも無い）", asked, resolved)
	}
	sugg := []dto.SongSuggestion{{Name: "深昏睡"}}
	if asked, resolved := svc.AdjudicateSuggestions(sugg); asked != 0 || resolved != 0 {
		t.Errorf("asked=%d resolved=%d, want 0/0", asked, resolved)
	}
}

// 既に照合できている行は聞かない（AI 呼び出しは決着しなかった分だけ）。
func TestAdjudicate_SkipsMatched(t *testing.T) {
	id := uuid.New().String()
	svc := &NormalizationService{}
	songs := []dto.CommentSong{{Name: "深昏睡", MatchedSongID: &id}}
	if asked, _ := svc.AdjudicateCommentSongs(songs); asked != 0 {
		t.Errorf("照合済みの行を %d 件聞こうとしている", asked)
	}
}

// 応答の行番号は**そのバッチの中でだけ**意味を持つこと。
//
// 通し番号で解釈すると、モデルがバッチ内で 0 から振り直したときに
// 別のバッチの答えを上書きする。実測でこれが起き、8 行すべて正解していた
// バッチが丸ごと誤答として記録された ── 応答は正常な形をしているので、
// 番号がずれても誰も気づけない。範囲外は捨てるのが唯一の防波堤になる。
func TestApplyVerdicts_IgnoresOutOfRangeIndex(t *testing.T) {
	svc := &NormalizationService{}
	batch := []*aiMatchRow{{Name: "深昏睡"}, {Name: "革命道中"}}
	id := uuid.New()
	ids := map[string]uuid.UUID{"s1": id}

	resolved := svc.applyVerdicts(batch, []aiMatchVerdict{
		{Index: 7, ID: strPtr("s1"), Confidence: 0.9},  // 範囲外
		{Index: -1, ID: strPtr("s1"), Confidence: 0.9}, // 範囲外
	}, ids)

	if resolved != 0 {
		t.Errorf("resolved=%d, want 0（範囲外の番号は捨てる）", resolved)
	}
	for i, r := range batch {
		if r.SongID != nil {
			t.Errorf("batch[%d] に範囲外の答えが書き込まれている", i)
		}
	}
}

// 知らない曲 id は捨てる。
// 一覧に無い id を書き込むと、存在しない楽曲を指す歌唱ができてしまう。
func TestApplyVerdicts_IgnoresUnknownSongID(t *testing.T) {
	svc := &NormalizationService{}
	batch := []*aiMatchRow{{Name: "深昏睡"}}
	resolved := svc.applyVerdicts(batch, []aiMatchVerdict{
		{Index: 0, ID: strPtr("s999"), Confidence: 0.9},
	}, map[string]uuid.UUID{"s1": uuid.New()})

	if resolved != 0 || batch[0].SongID != nil {
		t.Error("一覧に無い id を採用している")
	}
}

// バッチの大きさは控えめに保つ。
// 失敗が無音（番号ずれでも応答は正常に見える）なので、欲張ると検知できない誤りが増える。
func TestBatchSizeIsConservative(t *testing.T) {
	if aiMatchBatchSize <= 0 || aiMatchBatchSize > 10 {
		t.Errorf("aiMatchBatchSize = %d。0 だと進まず、大きすぎると番号ずれの被害が広がる", aiMatchBatchSize)
	}
}

// 否定を保存するキーと、照合で引くキーが同じであること。
// ここがずれると「別の曲だと判定したのに次も聞く」状態になり、AI を無限に呼び続ける。
func TestRejectionPairKeyMatchesLookupKey(t *testing.T) {
	songID := uuid.New()
	nameKey := songmatch.TitleKey("深昏睡")
	artistKey := songmatch.ParseArtist("春野").String()

	fromPkg := songmatch.IdentityPairKey(nameKey, artistKey, songID.String())
	fromRepo := repository.SongIdentityPairKey(nameKey, artistKey, songID)
	if fromPkg != fromRepo {
		t.Errorf("キーの作り方が食い違っている:\n  service 側 %q\n  repo 側    %q", fromPkg, fromRepo)
	}
}

// 短い曲名を文字数で足切りしないこと。
//
// 実装当初 4 文字未満を弾いていて「深昏睡」(3 文字) が漏れ、2 文字に下げても
// 「唱」(Ado) のような 1 文字の曲名が漏れた。実測では 819 曲中 1 文字が 14 件、
// 2 文字が 29 件あり、短い曲名は珍しくない。
func TestRecallHasNoLengthCutoff(t *testing.T) {
	for _, name := range []string{"唱", "空", "燈", "怪獣", "深昏睡"} {
		if key := songmatch.TitleKey(name); key == "" {
			t.Errorf("%q の照合キーが空。これでは候補を抽出できない", name)
		}
	}
	if identityPrefixLimit <= 0 || identityPrefixLimit > 5 {
		t.Errorf("identityPrefixLimit = %d。0 だと候補抽出が機能せず、大きすぎるとプロンプトが膨らむ", identityPrefixLimit)
	}
}

func strPtr(s string) *string { return &s }

// 連名クレジットのときは別名義を提案しないこと。
//
// 「May'n & 中島愛」と「ランカ・リー=中島愛」では、どの名前とどの名前が
// 同じ人なのかが定まらない。別名義はその人の全楽曲に効くので、
// 誤った 1 件の影響が広い。曲の照合自体は採用してよい。
func TestArtistAliasProposal_SkipsJointCredits(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"May'n & 中島愛", "ランカ・リー=中島愛"},
		{"中島愛", "シェリル・ノーム & ランカ・リー"},
	} {
		single := len(songmatch.ParseArtist(tc.a).Tokens) <= 1 &&
			len(songmatch.ParseArtist(tc.b).Tokens) <= 1
		if single {
			t.Errorf("%q / %q を単独名義とみなしている。別名義を提案してしまう", tc.a, tc.b)
		}
	}
	// 単独名義どうしは提案の対象
	if !(len(songmatch.ParseArtist("松任谷由実").Tokens) <= 1 &&
		len(songmatch.ParseArtist("荒井由実").Tokens) <= 1) {
		t.Error("単独名義どうしが対象から外れている")
	}
}
