package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/pkg/songmatch"
)

// 楽曲の同一性判定は「規則では決まらない組」を AI に回す仕組み。
// ここで固定するのは、AI を呼ぶ前後の約束ごと（呼ぶ相手の選び方と、二度聞かないこと）。

// 依存が無いときは黙って何もしない。
// 照合は規則の結果のまま残り、決着しない組は統合候補として人に回る。
func TestAdjudicateSongIdentity_NoDepsIsNoop(t *testing.T) {
	svc := &NormalizationService{}
	songs := []dto.CommentSong{{Name: "深昏睡"}}
	if asked, linked := svc.AdjudicateSongIdentityForCommentSongs(songs); asked != 0 || linked != 0 {
		t.Errorf("asked=%d linked=%d, want 0/0（AI も照合サービスも無い）", asked, linked)
	}
	sugg := []dto.SongSuggestion{{Name: "深昏睡"}}
	if asked, linked := svc.AdjudicateSongIdentityForSuggestions(sugg); asked != 0 || linked != 0 {
		t.Errorf("asked=%d linked=%d, want 0/0", asked, linked)
	}
}

// 既に照合できている行は聞かない（AI 呼び出しは決着しなかった分だけ）。
func TestAdjudicateSongIdentity_SkipsMatched(t *testing.T) {
	id := uuid.New().String()
	svc := &NormalizationService{}
	songs := []dto.CommentSong{{Name: "深昏睡", MatchedSongID: &id}}
	if asked, _ := svc.AdjudicateSongIdentityForCommentSongs(songs); asked != 0 {
		t.Errorf("照合済みの行を %d 件聞こうとしている", asked)
	}
}

// 判定を保存するキーと、照合で引くキーが同じであること。
// ここがずれると「判定したのに次も当たらない」状態になり、AI を無限に呼び続ける。
func TestIdentityPairKeyMatchesLookupKey(t *testing.T) {
	songID := uuid.New()
	name, artist := "深昏睡", "春野"
	nameKey := songmatch.TitleKey(name)
	artistKey := songmatch.ParseArtist(artist).String()

	fromPkg := songmatch.IdentityPairKey(nameKey, artistKey, songID.String())
	fromRepo := repoIdentityPairKey(nameKey, artistKey, songID)
	if fromPkg != fromRepo {
		t.Errorf("キーの作り方が食い違っている:\n  service 側 %q\n  repo 側    %q", fromPkg, fromRepo)
	}
}

// 短い曲名を文字数で足切りしないこと。
//
// 実装当初 4 文字未満を弾いていて「深昏睡」(3 文字) が漏れ、2 文字に下げても
// 「唱」(Ado) のような 1 文字の曲名が漏れた。実測では 819 曲中 1 文字が 14 件、
// 2 文字が 29 件あり、短い曲名は珍しくない。
// 当たり過ぎるかどうかを決めるのはヒット数であって字数ではない
// （「怪獣」は 2 文字でも 1 件、「恋」は 1 文字で 10 件）。
func TestIdentityRecallHasNoLengthCutoff(t *testing.T) {
	for _, name := range []string{"唱", "空", "燈", "怪獣", "深昏睡"} {
		if key := songmatch.TitleKey(name); key == "" {
			t.Errorf("%q の照合キーが空。これでは召回も学習もできない", name)
		}
	}
	// 上限は件数側で持つ（AI の入力を膨らませないため）
	if identityPrefixLimit <= 0 || identityPrefixLimit > 5 {
		t.Errorf("identityPrefixLimit = %d。0 だと召回が死に、大きすぎるとプロンプトが膨らむ", identityPrefixLimit)
	}
}
