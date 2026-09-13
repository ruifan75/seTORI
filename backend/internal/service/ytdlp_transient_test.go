package service

import "testing"

// **レート制限は「動画が消えた」と同じ文字列で来る。**
//
// yt-dlp は reason と subreason を連結してから rate-limited の案内を足すので
// （`extractor/youtube/_video.py`）、先頭は `Video unavailable.` で始まり、
// 消失と区別が付かない。
//
// live chat の取得はこれを `chatNoReplay`（＝この配信にはチャット replay が無い）
// より**先に**通す必要がある。通さないと、障害の最中に解析した配信が
// 「チャットの無い配信」として確定し、hash が入って次回はキャッシュ命中で
// 拍手検出まで飛ぶ ── 拍手 end が永久に付かないまま静かに残る（CLAUDE.md §6.5）。
func TestIsTransientFailureCatchesRateLimit(t *testing.T) {
	// 消失の目印を含みつつ、実体はレート制限。
	rateLimited := "ERROR: [youtube] abc: Video unavailable. This content isn't available, try again later. " +
		"The current session has been rate-limited by YouTube for up to an hour."
	if !isTransientFailure(rateLimited) {
		t.Error("レート制限を一時的な失敗と見ていない（チャット無しとして確定してしまう）")
	}

	for _, c := range []struct {
		name   string
		stderr string
		want   bool
	}{
		{"通信障害", "ERROR: [youtube] abc: Unable to download API page: ('Unable to connect to proxy', ...)", true},
		{"429", "ERROR: unable to download video data: HTTP Error 429: Too Many Requests", true},
		{"BOT 判定", "ERROR: [youtube] abc: Sign in to confirm you're not a bot", true},
		{"timeout", "ERROR: [youtube] abc: The read operation timed out", true},
		// **本当に消えた配信は一時的ではない。** ここが true になると、
		// 消えた配信を永久に取り直し続けることになる。
		{"本当に消えた", "ERROR: [youtube] abc: Video unavailable", false},
		{"非公開", "ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video", false},
	} {
		if got := isTransientFailure(c.stderr); got != c.want {
			t.Errorf("%s: isTransientFailure = %v, want %v", c.name, got, c.want)
		}
	}
}
