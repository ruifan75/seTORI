package repository

import (
	"regexp"
	"strings"
	"testing"
)

// 歌唱に付く歌手とタグは**順序を固定する**。
//
// 寄せる前の `FindBySongID` / `FindByTagID` は曲ごとに `GetSingers` / `GetTags` を
// 呼んでおり、**どちらも ORDER BY が無かった** ── Postgres は順序を約束しないので、
// 同じ歌唱でも要求ごとに並びが変わりうる。本番には歌手が 2 人以上の歌唱が
// 432 件（最大 21 人）あるので、画面上で合唱の顔ぶれが動くことになる。
//
// **並び順そのものを見る。** 最初は「ORDER BY という語があるか」だけを見ていたが、
// それでは `s.name` を `s.name DESC` に変えても、タグ側の `pt.id` を消しても通る
// （レビューで実際に確かめられた）。守りたいのは「指定があること」ではなく
// **「この順で並ぶこと」**。
func TestPerformanceAttachmentsAreOrdered(t *testing.T) {
	src := readSourceForTest(t, "performance_repository.go")

	i := strings.Index(src, "func (r *PerformanceRepository) attachTagsAndSingers")
	if i < 0 {
		t.Fatal("attachTagsAndSingers が無い")
	}
	body := src[i:]
	if j := strings.Index(body[1:], "\nfunc "); j > 0 {
		body = body[:j+1]
	}

	for _, tc := range []struct {
		name string
		want string // 並び順そのもの（末尾は行末で閉じる）
	}{
		// 歌手は名前の昇順。合唱の並びが要求ごとに変わらないように。
		{"歌手", "ORDER BY ps.performance_id, s.name`"},
		// タグは ID の昇順。件数は少ないが理由は同じ。
		{"タグ", "ORDER BY ppt.performance_id, pt.id`"},
	} {
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s の並び順が %q でない", tc.name, tc.want)
		}
	}

	// DESC などが紛れ込んでいないことも見る（上の完全一致で弾けるが、
	// 将来 ORDER BY を書き足したときに気付けるように数も確かめる）。
	if n := len(regexp.MustCompile(`ORDER BY`).FindAllString(body, -1)); n != 2 {
		t.Errorf("attachTagsAndSingers の ORDER BY が %d 個（歌手とタグの 2 つのはず）", n)
	}
}
