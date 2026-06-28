package chatend

import (
	"fmt"
	"os"
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
	for i := 0; i < 20; i++ { // 第一首曲末拍手在 96s
		ev = append(ev, Event{T: 96.0, Text: "888888"})
	}
	for i := 0; i < 15; i++ { // 第二首曲末拍手在 291s
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

// TestAgainstRealFile：設 CHATEND_SAMPLE=/abs/path/to/xxx.live_chat.json 才會跑，
// 用來和 Python 版輸出對照（人工比對印出的 ends）。
func TestAgainstRealFile(t *testing.T) {
	path := os.Getenv("CHATEND_SAMPLE")
	if path == "" {
		t.Skip("set CHATEND_SAMPLE to a real live_chat.json to verify against Python")
	}
	ev, err := ParseLiveChatFile(path)
	if err != nil {
		t.Fatal(err)
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
	// starts 由環境帶入（逗號分隔秒數）；沒帶就只印 event/applause 統計
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
