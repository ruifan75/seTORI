package repository

import (
	"strings"
	"testing"
)

// 歌唱に付く歌手とタグは**順序を固定する**。
//
// 寄せる前の `FindBySongID` / `FindByTagID` は `GetSingers` / `GetTags` を
// 曲ごとに呼んでおり、**どちらも ORDER BY が無かった** ── Postgres は
// 順序を約束しないので、同じ歌唱でも要求ごとに並びが変わりうる。
// 本番には歌手が 2 人以上の歌唱が 432 件（最大 21 人）あるので、
// 画面上で合唱の顔ぶれが動くことになる。
//
// 共通処理（attachTagsAndSingers）は名前順・ID 順に固定している。
// **寄せたことで順序が変わったが、変わった先のほうが正しい。**
func TestPerformanceAttachmentsAreOrdered(t *testing.T) {
	src := readSourceForTest(t, "performance_repository.go")

	i := strings.Index(src, "func (r *PerformanceRepository) attachTagsAndSingers")
	if i < 0 {
		t.Fatal("attachTagsAndSingers が無い")
	}
	body := src[i:]
	if j := strings.Index(body[1:], "\nfunc "); j > 0 {
		body = body[:j]
	}

	// 歌手は名前順（合唱の並びが要求ごとに変わらないように）。
	if !strings.Contains(body, "ORDER BY ps.performance_id, s.name") {
		t.Error("歌手の取得に名前順の ORDER BY が無い")
	}
	// タグは ID 順（表示の安定のため。件数は少ないが同じ理由）。
	if !strings.Contains(body, "ORDER BY") || strings.Count(body, "ORDER BY") < 2 {
		t.Error("タグ側にも ORDER BY が要る（順序未指定だと並びが安定しない）")
	}
}
