package service

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeYtdlp は「終了コード 0・ファイルを書かない・stderr に文字列を吐く」yt-dlp を作る。
//
// **`--ignore-no-formats-error` を付けているので、レート制限も一時的な不可視も
// 警告へ降格して終了コードは 0 になる。** その状態で stderr だけが手がかりになる、
// という本番の形をそのまま再現する。
func fakeYtdlp(t *testing.T, stderr string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "yt-dlp")
	script := "#!/bin/sh\ncat >&2 <<'EOF'\n" + stderr + "\nEOF\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("fake yt-dlp: %v", err)
	}
	return path
}

// **一時的な失敗を「チャットの無い配信」として確定させない。**
//
// 守りたいのは `isTransientFailure` の戻り値ではなく、`fetchLiveChat` が
// **それを `chatNoReplay` より先に見ること**。述語だけを検査するテストは、
// 呼び出し側から判定を丸ごと外しても通ってしまう（実際そう書いて指摘された）。
//
// ここで `chatNoReplay` に落ちると、呼び出し側は hash を保存して
// 「この配信にはチャットが無い」を結論として固定する。次回はキャッシュ命中で
// 拍手検出まで飛ぶので、**拍手 end が永久に付かないまま静かに残る**
// （CLAUDE.md §6.5）。
func TestFetchLiveChatClassifiesTransientBeforeNoReplay(t *testing.T) {
	// レート制限。**消失と同じ `Video unavailable.` で始まる**ので、
	// 順序が逆だと消失（＝chatNoReplay）と読める。
	rateLimited := "ERROR: [youtube] abc: Video unavailable. This content isn't available, try again later. " +
		"The current session has been rate-limited by YouTube for up to an hour."

	svc := NewChatEndService(nil, fakeYtdlp(t, rateLimited), t.TempDir())
	_, outcome, err := svc.fetchLiveChat("abc")

	if outcome != chatTransientError {
		t.Errorf("outcome = %v, want chatTransientError（%v だと「チャット無し」として確定する）", outcome, chatNoReplay)
	}
	if err == nil {
		t.Error("エラーを返していない")
	}
}

// 陽性対照：**本当に replay が無いときは chatNoReplay でなければならない。**
// これが無いと、上のテストは「常に chatTransientError を返す」実装でも通る。
func TestFetchLiveChatReturnsNoReplayWhenNothingIsWrong(t *testing.T) {
	// 警告も何も出ないまま、ファイルだけが書かれなかった状態。
	svc := NewChatEndService(nil, fakeYtdlp(t, "WARNING: [youtube] abc: no subtitles"), t.TempDir())
	_, outcome, _ := svc.fetchLiveChat("abc")

	if outcome != chatNoReplay {
		t.Errorf("outcome = %v, want chatNoReplay", outcome)
	}
}
