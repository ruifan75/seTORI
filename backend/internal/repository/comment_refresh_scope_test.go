package repository

import (
	"strings"
	"testing"
)

// **取り直しの対象を、所有者だけか参加者までかで切り替える**（issue #60）。
//
// 実際に呼んで、発行された SQL の該当部分を**完全一致**で見る。
// `is_owner` を含むかだけを見る検査では、条件を `OR` で繋ぐ・別の列に替える
// 改変を通してしまう。
func TestCommentRefreshScopeFollowsIncludeCollabs(t *testing.T) {
	for _, tc := range []struct {
		name           string
		includeCollabs bool
		want           string
	}{
		{"所有者だけ（既定）", false,
			"AND EXISTS (SELECT 1 FROM stream_channels ss WHERE ss.stream_id = s.id AND ss.is_owner AND ss.channel_id = ANY($2))"},
		{"参加者まで", true,
			"AND EXISTS (SELECT 1 FROM stream_channels ss WHERE ss.stream_id = s.id AND ss.channel_id = ANY($2))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, rec := newRecordingDB(t)
			NewStreamRepository(db).FindStreamsNeedingCommentRefresh([]string{"UC1"}, 30, nil, tc.includeCollabs)
			issued := rec.all()
			if len(issued) != 1 {
				t.Fatalf("1 本のはずが %d 本: %q", len(issued), issued)
			}
			norm := strings.Join(strings.Fields(issued[0]), " ")
			if !strings.Contains(norm, tc.want+" ORDER BY") {
				t.Errorf("チャンネルの絞り方が期待と違う\n got: %s\nwant 含む: %s ORDER BY", norm, tc.want)
			}
			if strings.Count(norm, "stream_channels ss") != 1 {
				t.Errorf("チャンネルの条件が 1 つではない: %s", norm)
			}
		})
	}
}

// チャンネルを指定しないときは絞らない（両方の値で同じ）。
func TestCommentRefreshWithoutChannelsHasNoChannelClause(t *testing.T) {
	for _, collabs := range []bool{false, true} {
		db, rec := newRecordingDB(t)
		NewStreamRepository(db).FindStreamsNeedingCommentRefresh(nil, 30, nil, collabs)
		if q := rec.all()[0]; strings.Contains(q, "stream_channels ss") {
			t.Errorf("チャンネル未指定なのに絞っている（collabs=%v）: %s", collabs, q)
		}
	}
}
