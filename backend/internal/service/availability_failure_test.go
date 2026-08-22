package service

import "testing"

// レート制限は "Video unavailable" で始まるので、消失と区別できることを固定する。
func TestTransientBeatsGone(t *testing.T) {
	rateLimited := "ERROR: [youtube] abc: Video unavailable. This content isn't available, try again later. " +
		"The current session has been rate-limited by YouTube for up to an hour."
	if !isTransientFailure(rateLimited) {
		t.Fatal("レート制限を一時障害として拾えていない")
	}
	if !isVideoGone(rateLimited) {
		t.Fatal("前提が変わった：この文字列は isVideoGone にも当たるはず（だから順序が要る）")
	}

	gone := "ERROR: [youtube] hVfDBfreYNI: Video unavailable. This video is not available"
	if isTransientFailure(gone) {
		t.Fatal("消失を一時障害と誤判定した")
	}
	if !isVideoGone(gone) {
		t.Fatal("消失を拾えていない")
	}

	network := "ERROR: [youtube] abc: Unable to download API page: ('Unable to connect to proxy', ...)"
	if !isTransientFailure(network) {
		t.Fatal("通信障害を一時障害として拾えていない")
	}
}
