package comment

import (
	"fmt"
	"testing"
)

// stubChatter は決まった応答を返す ai.Chatter。ネットワークも API キーも使わない。
type stubChatter struct {
	response string
	err      error
	gotSys   string
	gotUser  string
}

func (s *stubChatter) SimpleChat(systemPrompt, userMessage string) (string, error) {
	s.gotSys, s.gotUser = systemPrompt, userMessage
	return s.response, s.err
}

// 実データに基づく入力。1行目は括弧なしのタグ（実データの 51.8% がこの形）、
// 2行目は括弧つき、3行目は "shorts公開" という紛らわしい非タグ、4行目は非歌唱行。
var combinedTestLines = []string{
	"01:38:15 幾億光年 piano ver./ Omoinotake",
	"01:46:35 『愛・おぼえていますか』（アカペラ）",
	"01:31:37 リバベレ「ファタール」shorts公開!",
	"00:00:00 開演",
}

func combinedResponse() string {
	return `[
  {"line":1,"is_song":true,"start_ts":"01:38:15","end_ts":"","name_verbatim":"幾億光年","artist_verbatim":"Omoinotake","normalized_name":"幾億光年","normalized_name_reading":"いくおくこうねん","normalized_artist":"Omoinotake","normalized_artist_reading":"おもいのたけ","tags":["piano"],"confidence":0.9},
  {"line":2,"is_song":true,"start_ts":"01:46:35","end_ts":"","name_verbatim":"愛・おぼえていますか","artist_verbatim":"","normalized_name":"愛・おぼえていますか","normalized_name_reading":"あいおぼえていますか","normalized_artist":"","normalized_artist_reading":"","tags":["acappella"],"confidence":0.9},
  {"line":3,"is_song":true,"start_ts":"01:31:37","end_ts":"","name_verbatim":"ファタール","artist_verbatim":"","normalized_name":"ファタール","normalized_name_reading":"ふぁたーる","normalized_artist":"","normalized_artist_reading":"","tags":[],"confidence":0.8},
  {"line":4,"is_song":false,"start_ts":"00:00:00","end_ts":"","name_verbatim":"","artist_verbatim":"","normalized_name":"","normalized_name_reading":"","normalized_artist":"","normalized_artist_reading":"","tags":[],"confidence":0.0}
]`
}

func TestParseAndNormalizeWithAI(t *testing.T) {
	stub := &stubChatter{response: combinedResponse()}
	songs, err := ParseAndNormalizeWithAI(stub, combinedTestLines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(songs) != 3 {
		t.Fatalf("is_song=true の 3 件だけが返るはず。got %d 件", len(songs))
	}

	t.Run("括弧なしのタグを拾える", func(t *testing.T) {
		// これが 2 段階経路で落ちていたケース。正規化には "幾億光年" しか渡らず
		// "piano ver." が見えないため piano タグが付かなかった。
		got := songs[0]
		if got.NormalizedName != "幾億光年" {
			t.Errorf("NormalizedName = %q, want 幾億光年", got.NormalizedName)
		}
		if len(got.Tags) != 1 || got.Tags[0] != "piano" {
			t.Errorf("Tags = %v, want [piano]", got.Tags)
		}
		if got.NormalizedNameReading != "いくおくこうねん" {
			t.Errorf("読みが入っていない: %q", got.NormalizedNameReading)
		}
	})

	t.Run("括弧内のタグがアーティストにならない", func(t *testing.T) {
		// 2 段階経路では ParseComment の baseline が（アカペラ）を
		// アーティストと解釈し、AI の空文字列では上書きされず残っていた。
		got := songs[1]
		if got.NormalizedArtist == "アカペラ" {
			t.Errorf("アーティストにタグが入っている: %q", got.NormalizedArtist)
		}
		if len(got.Tags) != 1 || got.Tags[0] != "acappella" {
			t.Errorf("Tags = %v, want [acappella]", got.Tags)
		}
	})

	t.Run("shorts公開 はタグにしない", func(t *testing.T) {
		// 正規表現でタグを判定すると偽陽性になるケース。
		if len(songs[2].Tags) != 0 {
			t.Errorf("Tags = %v, want 空（YouTube Shorts の告知であって版種ではない）", songs[2].Tags)
		}
	})

	t.Run("タイムスタンプが秒に変換される", func(t *testing.T) {
		if songs[0].Start != 1*3600+38*60+15 {
			t.Errorf("Start = %d, want %d", songs[0].Start, 1*3600+38*60+15)
		}
	})

	t.Run("元のコメント行が保持される", func(t *testing.T) {
		if songs[0].OriginalComment != combinedTestLines[0] {
			t.Errorf("OriginalComment = %q", songs[0].OriginalComment)
		}
	})
}

func TestParseAndNormalizeWithAI_逐字検証(t *testing.T) {
	// AI が原文に存在しない名前を返した場合、逐字フィールドは採用されない。
	// 正規化フィールドは意図的に原文と異なるため、この検証の対象外。
	resp := `[
  {"line":1,"is_song":true,"start_ts":"01:38:15","end_ts":"","name_verbatim":"存在しない曲名","artist_verbatim":"","normalized_name":"幾億光年","normalized_name_reading":"","normalized_artist":"","normalized_artist_reading":"","tags":[],"confidence":0.9}
]`
	stub := &stubChatter{response: resp}
	songs, err := ParseAndNormalizeWithAI(stub, combinedTestLines[:1])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(songs) != 1 {
		t.Fatalf("got %d songs", len(songs))
	}
	if songs[0].Name == "存在しない曲名" {
		t.Errorf("原文に無い名前が採用された: %q", songs[0].Name)
	}
}

func TestFilterAllowedTags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want int
	}{
		{"既知のタグは通す", []string{"piano", "short"}, 2},
		{"語彙外は落とす", []string{"piano", "Piano ver.", "ピアノ", "instrumental"}, 1},
		{"重複を除く", []string{"piano", "piano"}, 1},
		{"空は落とす", []string{"", "  "}, 0},
		{"nil は nil", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := filterAllowedTags(c.in); len(got) != c.want {
				t.Errorf("filterAllowedTags(%v) = %v, want %d 件", c.in, got, c.want)
			}
		})
	}
}

func TestParseAndNormalizeWithAI_エラー処理(t *testing.T) {
	t.Run("AI 呼び出しの失敗を伝える", func(t *testing.T) {
		stub := &stubChatter{err: fmt.Errorf("boom")}
		if _, err := ParseAndNormalizeWithAI(stub, combinedTestLines); err == nil {
			t.Error("エラーが返らなかった")
		}
	})

	t.Run("壊れた JSON はエラーにする", func(t *testing.T) {
		stub := &stubChatter{response: "これはJSONではありません"}
		if _, err := ParseAndNormalizeWithAI(stub, combinedTestLines); err == nil {
			t.Error("エラーが返らなかった")
		}
	})

	t.Run("タイムスタンプ行が無ければエラー", func(t *testing.T) {
		stub := &stubChatter{response: "[]"}
		if _, err := ParseAndNormalizeWithAI(stub, []string{"時刻のない雑談"}); err == nil {
			t.Error("エラーが返らなかった")
		}
	})

	t.Run("client が nil ならエラー", func(t *testing.T) {
		if _, err := ParseAndNormalizeWithAI(nil, combinedTestLines); err == nil {
			t.Error("エラーが返らなかった")
		}
	})
}
