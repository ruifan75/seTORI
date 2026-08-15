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

func TestParseNormalizeAndDedupWithAI_原文検証はsrc全体が対象(t *testing.T) {
	// まとめた結果、歌手名は別の行から来ることがあるので、
	// 原文チェックは src に挙がった行の集合に対して行う必要がある。
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

// AI が要素を返したのに 1 つも使えない場合はエラーにする。
//
// これを成功として返すと「この配信には曲が無い」という見た目で通ってしまう。
// 実際に 2026-08-07、応答が劣化して 10 要素すべて曲名が空になり、
// 警告も出ないまま 0 曲として扱われた。エラーにすれば 2 段階経路へ退避できる。
func TestParseNormalizeAndDedupWithAI_全要素が使えなければエラー(t *testing.T) {
	// 曲名が空 & src も不正 → 1 件も組み立てられない
	resp := `[
  {"src":[999],"ts":"9:26","nv":"","n":"","t":[],"c":0.9},
  {"src":[888],"ts":"20:42","nv":"","n":"","t":[],"c":0.9}
]`
	_, err := ParseNormalizeAndDedupWithAI(&stubChatter{response: resp}, groupedTestComments)
	if err == nil {
		t.Fatal("使える要素が 0 なのにエラーが返らなかった（0 曲として通ってしまう）")
	}
}

// 空配列（＝AI が「歌唱行は無い」と判断）は正常な結果なのでエラーにしない。
func TestParseNormalizeAndDedupWithAI_空配列は正常(t *testing.T) {
	if _, err := ParseNormalizeAndDedupWithAI(&stubChatter{response: `[]`}, groupedTestComments); err != nil {
		t.Errorf("空配列は正常な結果のはず: %v", err)
	}
}

// プロンプトが「歌手を推測で埋めない」ことを指示していること。
//
// 本番の GT で数えると、コメントに歌手が書かれていない 49 行すべてで
// **その曲名は DB 内で唯一**だった。つまり AI が歌手を補っても
// 「補わなければ照合できなかった」ケースは 1 件も無く、
// 得られたのは確認を省けたこと（42 行）だけ。代わりに 7 行で
// 誤った歌手が入り、0.95 の自動採用として**人の確認を通らずに保存**された。
//
// 誤りはマイナーな曲に集中していた（Re:AcT のオリジナル曲に有名な同名曲の歌手を当てる）。
// 空のままなら title_only 0.80 に落ちて人の確認に回るので、
// 「確認が要る」ことが可視化される。
func TestGroupedPromptDoesNotInventArtist(t *testing.T) {
	must := []string{
		"行に書かれているものだけを入れる",
		"推測して埋めてはいけない",
	}
	for _, m := range must {
		if !strings.Contains(groupedAISystemPrompt, m) {
			t.Errorf("プロンプトに %q が無い。歌手の推測を許すと誤りが確認を通らず保存される", m)
		}
	}
	// 例が指示と矛盾していないこと（例は指示より強く効く）
	if strings.Contains(groupedAISystemPrompt, `"a":"YOASOBI"`) {
		t.Error("例が「歌手を補う」挙動を示している。指示と矛盾する")
	}
}
