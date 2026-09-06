package repository

import (
	"strings"
	"testing"
)

// 削除の可否は**濾さない**参照で決めること。
//
// 画面の歌唱一覧は非表示・秘匿を落とすので「0 件」に見えても
// `performances.song_id` の参照は残っていることがある（会限配信の歌唱を
// 作ったあとがまさにそれ）。濾した件数で判断すると、押した人には
// 理由の分からない失敗になる ── DB の ON DELETE RESTRICT は止めてくれるが、
// それは Postgres のエラーとして出てくるだけ。
//
// ここで見ているのは SQL の形。実際に「秘匿だけの歌唱でも止まる」ことは
// 端点を叩いて確認した（画面 0 件・DELETE → 409）。
func TestSongDeleteGuardDoesNotFilter(t *testing.T) {
	src := readSourceForTest(t, "song_repository.go")

	i := strings.Index(src, "func (r *SongRepository) HasAnyPerformance")
	if i < 0 {
		t.Fatal("HasAnyPerformance が無い")
	}
	body := src[i : i+600]

	// **濾さないことが要点。** これらが入ったら、見えない歌唱を見落とす。
	for _, forbidden := range []string{"is_hidden", "NotRestricted", "restriction_override", "members_only"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("削除の判定に %q が入っている（濾すと見えない歌唱を見落とす）", forbidden)
		}
	}
	if !strings.Contains(body, "SELECT 1 FROM performances WHERE song_id") {
		t.Error("performances を直接見ていない")
	}
}
