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
