package comment

import "testing"

// qvoNHajaMlg のセットリストコメント：コラボ曲は「タイムスタンプ+with ○○」と「曲名/歌手」の 2 行形式。
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
		t.Fatalf("ParseComments が %d 曲を解析、期待値は %d 曲: %+v", len(songs), len(want), songs)
	}
	for i, w := range want {
		got := songs[i]
		if got.Start != w.start || got.Name != w.name || got.OriginalArtist != w.artist {
			t.Errorf("songs[%d]\n  got  start=%d name=%q artist=%q\n  want start=%d name=%q artist=%q",
				i, got.Start, got.Name, got.OriginalArtist, w.start, w.name, w.artist)
		}
	}
}

// タイムスタンプだけの行に曲名の行が続く形式も結合できること。
func TestStitchTimestampOnlyLines(t *testing.T) {
	songs := ParseComments([]string{`セトリ
5:27
vivid / 花鋏キョウ
17:51
セブンティーン / YOASOBI
33:54
mist / 花鋏キョウ`})

	if len(songs) != 3 {
		t.Fatalf("%d 曲を解析、期待値は 3 曲: %+v", len(songs), songs)
	}
	if songs[1].Name != "セブンティーン" || songs[1].OriginalArtist != "YOASOBI" || songs[1].Start != 17*60+51 {
		t.Errorf("songs[1] = %+v、期待値は セブンティーン / YOASOBI @17:51", songs[1])
	}
}

// タイムスタンプ行が閾値未満のコメント（歌詞引用の感想など）から偽の楽曲を結合してはならない。
func TestStitchSkipsNonSetlistComments(t *testing.T) {
	songs := ParseComments([]string{`12:08 13:17
"You are between Light and Shadow
Dance between Light and Shadow"

These 2 point are same in original, But I feel like it have slightly difference here !  So good`})

	if len(songs) != 0 {
		t.Errorf("感想コメントから楽曲を結合すべきではない, got %+v", songs)
	}
}

// 1 行形式のセットリストで曲名が with/feat から始まっていても、結合を発動してはならない。
func TestStitchDoesNotMergeAcrossBlankLines(t *testing.T) {
	lines := []string{
		"5:27 vivid / 花鋏キョウ",
		"17:51 with 水科葵",
		"",
		"33:54 mist / 花鋏キョウ",
		"41:08 ルーマー / ポリスピカデリー",
	}
	out := stitchTwoLineEntries(lines)
	// 次の行が空行なら結合せず、そのまま残す
	if len(out) != len(lines) {
		t.Errorf("空行の後を結合すべきではない, got %d 行: %q", len(out), out)
	}
}

// AI が結合行のコラボ情報まで artist にコピーした場合は、歌手名だけに切り戻す。
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
		t.Fatalf("%d 曲を解析、期待値は 3 曲: %+v", len(songs), songs)
	}
	if songs[1].OriginalArtist != "YOASOBI" {
		t.Errorf("songs[1].artist = %q, コラボ情報を除いた YOASOBI を期待", songs[1].OriginalArtist)
	}
	if songs[2].OriginalArtist != "" {
		t.Errorf("songs[2].artist = %q, 全体がコラボ情報なら不明として扱うことを期待", songs[2].OriginalArtist)
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
		t.Errorf("AI の前処理には結合後の行を含めること, got %q", lines)
	}
}
