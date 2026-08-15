package service

import (
	"testing"

	"github.com/ruifan75/setori/internal/dto"
)

func TestChaptersAsText(t *testing.T) {
	got := chaptersAsText([]Chapter{
		{Start: 0, End: 102, Title: "開始"},
		{Start: 127, End: 719, Title: "ヴィーナスとジーザス / やくしまるえつこ"},
		{Start: 3723, End: 3900, Title: "ギラギラ / Ado"},
	})
	want := "0:00 開始\n2:07 ヴィーナスとジーザス / やくしまるえつこ\n1:02:03 ギラギラ / Ado\n"
	if got != want {
		t.Fatalf("chaptersAsText:\n got %q\nwant %q", got, want)
	}
}

// 章節の end は次の章節の開始なので、必ず推定値として入る。
// ここが「確かな end」になると、拍手検出が上書きしてくれなくなる。
func TestApplyChapterEndsMarksEstimated(t *testing.T) {
	chapters := []Chapter{
		{Start: 127, End: 719, Title: "ヴィーナスとジーザス / やくしまるえつこ"},
		{Start: 719, End: 1220, Title: "プラチナ / 坂本真綾"},
	}
	songs := []dto.CommentSong{
		{Start: 127, Name: "ヴィーナスとジーザス"},
		{Start: 719, Name: "プラチナ"},
	}
	applyChapterEnds(songs, chapters)

	for i, want := range []int{719, 1220} {
		if songs[i].End != want {
			t.Fatalf("songs[%d].End = %d, want %d", i, songs[i].End, want)
		}
		if !songs[i].IsEndTimeEstimated {
			t.Fatalf("songs[%d].IsEndTimeEstimated = false, want true", i)
		}
	}
}

// 抽出側が既に確かな end を持っていれば章節で上書きしない。
func TestApplyChapterEndsKeepsExplicitEnd(t *testing.T) {
	songs := []dto.CommentSong{{Start: 127, End: 600, IsEndTimeEstimated: false}}
	applyChapterEnds(songs, []Chapter{{Start: 127, End: 719}})
	if songs[0].End != 600 {
		t.Fatalf("End = %d, want 600 (明示された end を保つ)", songs[0].End)
	}
}

func TestEndSourceForChapter(t *testing.T) {
	cases := []struct {
		name string
		song dto.CommentSong
		want string
	}{
		{"end が無い", dto.CommentSong{Start: 10}, "unknown"},
		// 章節の end は次の章節の開始でしかないので、確度は next_start 止まり
		{"章節の end", dto.CommentSong{Start: 10, End: 300, IsEndTimeEstimated: true}, "next_start"},
		{"拍手で埋まった", dto.CommentSong{Start: 10, End: 280, ChatEnd: 280}, "chat"},
	}
	for _, c := range cases {
		if got := endSourceForChapter(c.song); got != c.want {
			t.Errorf("%s: endSourceForChapter = %q, want %q", c.name, got, c.want)
		}
	}
	// next_start は「入力元が終了時間を持っていた」に数えない＝審査へ回る
	if reliableEndSources[endSourceForChapter(dto.CommentSong{Start: 10, End: 300, IsEndTimeEstimated: true})] {
		t.Error("章節の end が確度のある end として扱われている（審査を素通りする）")
	}
}

// 「まだ調べていない」と「調べたが章節が無い」を取り違えないこと。
func TestDecodeChapters(t *testing.T) {
	if _, ok := decodeChapters(nil); ok {
		t.Error("NULL は未取得として扱うこと")
	}
	if chapters, ok := decodeChapters([]byte(`[]`)); !ok || len(chapters) != 0 {
		t.Error("空配列は「調べたが章節が無い」として扱うこと")
	}
	chapters, ok := decodeChapters([]byte(`[{"start":10,"end":20,"title":"a"}]`))
	if !ok || len(chapters) != 1 || chapters[0].Title != "a" {
		t.Errorf("decodeChapters = %v, %v", chapters, ok)
	}
}
