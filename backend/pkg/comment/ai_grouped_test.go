package comment

import (
	"strings"
	"testing"
)

// 同じ配信に 2 人がセトリを投稿した状況。実データで 59.3% の配信がこの形。
// コメント1 は曲名のみ、コメント2 は歌手名つきで終了時刻もある、という補完関係にしてある。
var groupedTestComments = []string{
	"9:26 さよなら、花泥棒さん\n20:42 群青\n開演までしばらくお待ちください",
	"09:26 さよなら、花泥棒さん / ナノウ\n20:42 - 24:10 群青 / YOASOBI",
}

func TestParseNormalizeAndDedupWithAI(t *testing.T) {
	// AI が 2 コメントの同じ曲をまとめ、src に両方の行番号を挙げた想定。
	resp := `[
  {"src":[1,3],"ts":"9:26","te":"","nv":"さよなら、花泥棒さん","av":"ナノウ","n":"さよなら、花泥棒さん","nr":"さよならはなどろぼうさん","a":"ナノウ","ar":"なのう","t":[],"c":0.9},
  {"src":[2,4],"ts":"20:42","te":"24:10","nv":"群青","av":"YOASOBI","n":"群青","nr":"ぐんじょう","a":"YOASOBI","ar":"よあそび","t":[],"c":0.9}
]`
	stub := &stubChatter{response: resp}
	songs, err := ParseNormalizeAndDedupWithAI(stub, groupedTestComments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(songs) != 2 {
		t.Fatalf("重複がまとまって 2 件になるはず。got %d", len(songs))
	}

	t.Run("コメント境界がプロンプトに現れる", func(t *testing.T) {
		// 境界が無いと AI は「別の人が投稿した同じセトリ」を認識しづらい
		if !strings.Contains(stub.gotUser, "--- コメント 1 ---") ||
			!strings.Contains(stub.gotUser, "--- コメント 2 ---") {
			t.Errorf("コメント区切りが入っていない:\n%s", stub.gotUser)
		}
	})

	t.Run("別コメントから歌手名を取り込める", func(t *testing.T) {
		// 1行目のコメントには歌手名が無い。まとめた相手の行から来る。
		if songs[0].OriginalArtist != "ナノウ" {
			t.Errorf("OriginalArtist = %q, want ナノウ", songs[0].OriginalArtist)
		}
	})

	t.Run("別コメントから終了時刻を取り込める", func(t *testing.T) {
		want := 24*60 + 10
		if songs[1].End != want {
			t.Errorf("End = %d, want %d", songs[1].End, want)
		}
		if songs[1].IsEndTimeEstimated {
			t.Error("明示された終了時刻なのに推定扱いになっている")
		}
	})

	t.Run("開始時刻が秒に変換される", func(t *testing.T) {
		if songs[0].Start != 9*60+26 {
			t.Errorf("Start = %d, want %d", songs[0].Start, 9*60+26)
		}
	})
}

func TestBuildSongsFromGrouped_不正なsrc(t *testing.T) {
	lines := extractTimestampLinesGrouped(groupedTestComments)

	t.Run("範囲外の行番号は無視する", func(t *testing.T) {
		sels := []groupedSelection{{Src: []int{1, 999, -3}, NameVerb: "さよなら、花泥棒さん"}}
		songs := buildSongsFromGrouped(sels, lines)
		if len(songs) != 1 {
			t.Fatalf("有効な行が1つあるので1件返るはず。got %d", len(songs))
		}
	})

	t.Run("有効な行が1つも無ければ要素を捨てる", func(t *testing.T) {
		sels := []groupedSelection{{Src: []int{999}, NameVerb: "存在しない"}}
		if songs := buildSongsFromGrouped(sels, lines); len(songs) != 0 {
			t.Errorf("捨てられていない: %+v", songs)
		}
	})

	t.Run("src が空でも落ちない", func(t *testing.T) {
		if songs := buildSongsFromGrouped([]groupedSelection{{Src: nil}}, lines); len(songs) != 0 {
			t.Errorf("捨てられていない: %+v", songs)
		}
	})

	t.Run("重複した行番号は1回だけ数える", func(t *testing.T) {
		got := validSrcLines([]int{1, 1, 1}, lines)
		if len(got) != 1 {
			t.Errorf("validSrcLines = %v, want 1 件", got)
		}
	})
}

func TestParseNormalizeAndDedupWithAI_逐字検証はsrc全体が対象(t *testing.T) {
	// まとめた結果、歌手名は別の行から来ることがあるので、
	// 逐字チェックは src に挙がった行の集合に対して行う必要がある。
	resp := `[
  {"src":[1,3],"ts":"9:26","te":"","nv":"さよなら、花泥棒さん","av":"ナノウ","n":"さよなら、花泥棒さん","nr":"","a":"ナノウ","ar":"","t":[],"c":0.9}
]`
	stub := &stubChatter{response: resp}
	songs, err := ParseNormalizeAndDedupWithAI(stub, groupedTestComments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "ナノウ" は 3 行目にしか無いが、src に 3 が入っているので採用される
	if songs[0].OriginalArtist != "ナノウ" {
		t.Errorf("src 内の別行にある値が採用されなかった: %q", songs[0].OriginalArtist)
	}

	// 一方、src のどの行にも無い値は採用しない
	resp2 := `[
  {"src":[1],"ts":"9:26","te":"","nv":"さよなら、花泥棒さん","av":"存在しない歌手","n":"さよなら、花泥棒さん","nr":"","a":"","ar":"","t":[],"c":0.9}
]`
	stub2 := &stubChatter{response: resp2}
	songs2, err := ParseNormalizeAndDedupWithAI(stub2, groupedTestComments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if songs2[0].OriginalArtist == "存在しない歌手" {
		t.Errorf("src のどの行にも無い値が採用された")
	}
}

func TestExtractTimestampLinesGrouped(t *testing.T) {
	lines := extractTimestampLinesGrouped(groupedTestComments)

	t.Run("コメント由来を保持する", func(t *testing.T) {
		if lines[0].CommentIndex != 1 {
			t.Errorf("1行目の CommentIndex = %d, want 1", lines[0].CommentIndex)
		}
		last := lines[len(lines)-1]
		if last.CommentIndex != 2 {
			t.Errorf("最終行の CommentIndex = %d, want 2", last.CommentIndex)
		}
	})

	t.Run("タイムスタンプの無い行は落とす", func(t *testing.T) {
		for _, l := range lines {
			if strings.Contains(l.Text, "開演までしばらく") {
				t.Errorf("タイムスタンプの無い行が残っている: %q", l.Text)
			}
		}
	})
}
