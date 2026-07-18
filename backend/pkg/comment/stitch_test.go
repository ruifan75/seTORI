package comment

import "testing"

// qvoNHajaMlg のセトリ留言：合唱曲は「時間戳+with ○○」と「歌名/歌手」の兩行式
const qvoNHajaMlgSetlist = `お誕生日おめでとうございます
セトリです
/
5:27    vivid / 花鋏キョウ

9:43    Cosmic Dancers / 花鋏キョウ×獅子神レオナ

17:51    with 水科葵
セブンティーン / YOASOBI

26:04    with 涼海ネモ
藍二乗 / ヨルシカ

33:54    with 稀羽すう & 魔光リサ
Ready Steady  /  Giga

41:08    with 夢川かなう
ルーマー / ポリスピカデリー

48:50    with エルセ
ODDS&ENDS / ryo(supercell)

1:05:19    mist / 花鋏キョウ`

func TestParseCommentsTwoLineEntries(t *testing.T) {
	songs := ParseComments([]string{qvoNHajaMlgSetlist})

	want := []struct {
		start  int
		name   string
		artist string
	}{
		{5*60 + 27, "vivid", "花鋏キョウ"},
		{9*60 + 43, "Cosmic Dancers", "花鋏キョウ×獅子神レオナ"},
		{17*60 + 51, "セブンティーン", "YOASOBI"},
		{26*60 + 4, "藍二乗", "ヨルシカ"},
		{33*60 + 54, "Ready Steady", "Giga"},
		{41*60 + 8, "ルーマー", "ポリスピカデリー"},
		{48*60 + 50, "ODDS&ENDS", "ryo(supercell)"},
		{3600 + 5*60 + 19, "mist", "花鋏キョウ"},
	}

	if len(songs) != len(want) {
		t.Fatalf("ParseComments 解析出 %d 首，期望 %d 首: %+v", len(songs), len(want), songs)
	}
	for i, w := range want {
		got := songs[i]
		if got.Start != w.start || got.Name != w.name || got.OriginalArtist != w.artist {
			t.Errorf("songs[%d]\n  got  start=%d name=%q artist=%q\n  want start=%d name=%q artist=%q",
				i, got.Start, got.Name, got.OriginalArtist, w.start, w.name, w.artist)
		}
	}
}

// 時間戳單獨一行、歌名在下一行的格式也要能縫合
func TestStitchTimestampOnlyLines(t *testing.T) {
	songs := ParseComments([]string{`セトリ
5:27
vivid / 花鋏キョウ
17:51
セブンティーン / YOASOBI
33:54
mist / 花鋏キョウ`})

	if len(songs) != 3 {
		t.Fatalf("解析出 %d 首，期望 3 首: %+v", len(songs), songs)
	}
	if songs[1].Name != "セブンティーン" || songs[1].OriginalArtist != "YOASOBI" || songs[1].Start != 17*60+51 {
		t.Errorf("songs[1] = %+v，期望 セブンティーン / YOASOBI @17:51", songs[1])
	}
}

// 時間戳行數不足門檻的留言（如歌詞引用感想）不可縫合出假歌曲
func TestStitchSkipsNonSetlistComments(t *testing.T) {
	songs := ParseComments([]string{`12:08 13:17
"You are between Light and Shadow
Dance between Light and Shadow"

These 2 point are same in original, But I feel like it have slightly difference here !  So good`})

	if len(songs) != 0 {
		t.Errorf("感想留言不應縫合出歌曲，got %+v", songs)
	}
}

// 一行式セトリ內、歌名恰好以 with/feat 開頭的曲目不可觸發合併
func TestStitchDoesNotMergeAcrossBlankLines(t *testing.T) {
	lines := []string{
		"5:27 vivid / 花鋏キョウ",
		"17:51 with 水科葵",
		"",
		"33:54 mist / 花鋏キョウ",
		"41:08 ルーマー / ポリスピカデリー",
	}
	out := stitchTwoLineEntries(lines)
	// 下一行是空行 → 不合併，原樣保留
	if len(out) != len(lines) {
		t.Errorf("空行後不應合併，got %d 行: %q", len(out), out)
	}
}

// AI 把縫合行的合唱資訊照抄進 artist 時，應被裁回純歌手名
func TestParseCommentsWithAICleansStitchedArtist(t *testing.T) {
	fake := fakeChatter{response: `[
  {"line":1,"is_song":true,"start_ts":"5:27","end_ts":"","name":"vivid","artist":"花鋏キョウ"},
  {"line":2,"is_song":true,"start_ts":"17:51","end_ts":"","name":"セブンティーン","artist":"YOASOBI / with 水科葵"},
  {"line":3,"is_song":true,"start_ts":"26:04","end_ts":"","name":"藍二乗","artist":"with 涼海ネモ"}
]`}
	songs, err := ParseCommentsWithAI(fake, []string{`セトリ
5:27    vivid / 花鋏キョウ

17:51    with 水科葵
セブンティーン / YOASOBI

26:04    with 涼海ネモ
藍二乗`})
	if err != nil {
		t.Fatalf("ParseCommentsWithAI error: %v", err)
	}
	if len(songs) != 3 {
		t.Fatalf("解析出 %d 首，期望 3 首: %+v", len(songs), songs)
	}
	if songs[1].OriginalArtist != "YOASOBI" {
		t.Errorf("songs[1].artist = %q，期望裁掉合唱資訊後為 YOASOBI", songs[1].OriginalArtist)
	}
	if songs[2].OriginalArtist != "" {
		t.Errorf("songs[2].artist = %q，整段是合唱資訊時應視為未知", songs[2].OriginalArtist)
	}
}

func TestExtractTimestampLinesStitches(t *testing.T) {
	lines := extractTimestampLines([]string{qvoNHajaMlgSetlist})
	found := false
	for _, l := range lines {
		if l == "17:51 セブンティーン / YOASOBI / with 水科葵" {
			found = true
		}
	}
	if !found {
		t.Errorf("AI 前置過濾應包含縫合後的行，got %q", lines)
	}
}
