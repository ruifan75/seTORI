package chatend

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestIsPureApplause(t *testing.T) {
	pure := []string{"888888", "ぱちぱち", "拍手", ":clapping_hands::clapping_hands:", "👏👏👏", "8888 :clap:"}
	notPure := []string{"次の曲なに？", "かわいい", "わしょーい:clapping_hands:", "神曲", "こんばんは"}
	for _, s := range pure {
		if !IsPureApplause(s) {
			t.Errorf("expected pure applause: %q", s)
		}
	}
	for _, s := range notPure {
		if IsPureApplause(s) {
			t.Errorf("expected NOT pure applause: %q", s)
		}
	}
}

func TestDetectEnds(t *testing.T) {
	var ev []Event
	for i := 0; i < 20; i++ { // 1 曲目の曲末拍手は 96 秒
		ev = append(ev, Event{T: 96.0, Text: "888888"})
	}
	for i := 0; i < 15; i++ { // 2 曲目の曲末拍手は 291 秒
		ev = append(ev, Event{T: 291.0, Text: ":clapping_hands:"})
	}
	res := DetectEnds([]float64{0, 200}, ev, 320, DefaultOptions())
	if len(res) != 2 {
		t.Fatalf("got %d results", len(res))
	}
	// onset 96 − lag 2 = 94
	if res[0].End == nil || *res[0].End < 90 || *res[0].End > 96 {
		t.Errorf("song1 end = %v, want ~94", res[0].End)
	}
	if res[1].End == nil || *res[1].End < 285 || *res[1].End > 292 {
		t.Errorf("song2 end = %v, want ~289", res[1].End)
	}
}

func TestNoApplauseReturnsNil(t *testing.T) {
	ev := []Event{{T: 50, Text: "こんばんは"}, {T: 120, Text: "次の曲は？"}}
	res := DetectEnds([]float64{0}, ev, 300, DefaultOptions())
	if res[0].End != nil {
		t.Errorf("expected nil end, got %v", *res[0].End)
	}
}

// TestAgainstRealFile：CHATEND_SAMPLE=/abs/path/to/xxx.live_chat.json を設定した場合のみ実行、
// Python 版の出力と照合するために使用する（出力された ends を目視で比較する）。
func TestAgainstRealFile(t *testing.T) {
	path := os.Getenv("CHATEND_SAMPLE")
	if path == "" {
		t.Skip("set CHATEND_SAMPLE to a real live_chat.json to verify against Python")
	}
	ev, recognized, err := ParseLiveChatFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !recognized {
		t.Fatal("実ファイルなのに live chat replay として認識できていない")
	}
	pure := 0
	for _, e := range ev {
		if IsPureApplause(e.Text) {
			pure++
		}
	}
	t.Logf("parsed %d events, %d pure-applause", len(ev), pure)
	starts := []float64{}
	for _, s := range os.Getenv("CHATEND_STARTS") {
		_ = s
	}
	// starts は環境変数から受け取る（秒数をカンマ区切り）。未指定なら event/applause の統計だけを表示する
	if ss := os.Getenv("CHATEND_STARTS"); ss != "" {
		for _, tok := range splitComma(ss) {
			var f float64
			fmt.Sscanf(tok, "%f", &f)
			starts = append(starts, f)
		}
		se := ev[len(ev)-1].T
		for i, r := range DetectEnds(starts, ev, se, DefaultOptions()) {
			if r.End != nil {
				t.Logf("song %d start=%.0f -> end=%.1f (conf %.2f)", i, r.Start, *r.End, r.Confidence)
			} else {
				t.Logf("song %d start=%.0f -> NO APPLAUSE", i, r.Start)
			}
		}
	}
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// 壊れた／別物のファイルを「0 件・エラー無し」で返すと、呼び出し側は
// 「拍手が無かった」という確かな結論と区別できない。**サイズでは判別できない**
// （十分に長くても中身が別物なら同じ）ので、認識できたかを返す。
func TestParseLiveChatRecognizesReplay(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantRecognized bool
	}{
		{name: "空", body: "", wantRecognized: false},
		{name: "途中で切れている", body: "{", wantRecognized: false},
		{
			// レビューで指摘された形：長いが中身が replay ではない。
			// サイズ判定だけではこれを弾けない。
			name:           "十分に長いが別物",
			body:           strings.Repeat(`{"a":1}`+"\n", 64),
			wantRecognized: false,
		},
		{
			name: "replay の記録がある",
			body: `{"replayChatItemAction":{"videoOffsetTimeMsec":"1000","actions":[` +
				`{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":` +
				`{"message":{"runs":[{"text":"888"}]}}}}}]}}` + "\n",
			wantRecognized: true,
		},
		{
			// 記録はあるが描画できるテキストが無い → Event は 0 件でも
			// 「replay のファイルである」ことは確か
			name:           "記録はあるがテキストが無い",
			body:           `{"replayChatItemAction":{"videoOffsetTimeMsec":"1000","actions":[]}}` + "\n",
			wantRecognized: true,
		},
		{
			// **ダウンロード中断の典型**：正常な行が並んだあとで切れる。
			// 記録の有無だけで見ると通ってしまう。
			name: "正常な行のあとで切れている",
			body: `{"replayChatItemAction":{"videoOffsetTimeMsec":"1000","actions":[` +
				`{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":` +
				`{"message":{"runs":[{"text":"888"}]}}}}}]}}` + "\n" + "{",
			wantRecognized: false,
		},
		{
			// offset が数値でない行も壊れている。ここで recognized を
			// 立ててしまうと、壊れた行が「認識できた」に化ける。
			name:           "offset が数値でない",
			body:           `{"replayChatItemAction":{"videoOffsetTimeMsec":"broken","actions":[]}}` + "\n",
			wantRecognized: false,
		},
		{
			// replay ではない行が混ざっているのは正常（JSON としては正しい）
			name: "replay 以外の行が混ざる",
			body: `{"somethingElse":{}}` + "\n" + `{"replayChatItemAction":{"videoOffsetTimeMsec":"1000","actions":[` +
				`{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":` +
				`{"message":{"runs":[{"text":"888"}]}}}}}]}}` + "\n",
			wantRecognized: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, recognized, err := ParseLiveChat(strings.NewReader(tt.body))
			if err != nil {
				t.Fatalf("解析でエラー: %v", err)
			}
			if recognized != tt.wantRecognized {
				t.Errorf("recognized = %v, want %v", recognized, tt.wantRecognized)
			}
		})
	}
}
