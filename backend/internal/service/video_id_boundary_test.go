package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nil repo の早退は使わず、ファイル・実コマンドの境界を直接検査する。
func TestVideoIDBeforeFilesAndCommands(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "invocations")
	script := filepath.Join(root, "fake-ytdlp")
	// マーカーのパスは引数・環境ではなくテスト生成スクリプトの定数。外部通信はしない。
	body := "#!/bin/sh\nprintf 'called\\n' >> '" + strings.ReplaceAll(marker, "'", "'\\''") + "'\nprintf '%s\\n' '[{\"start_time\":1,\"end_time\":2,\"title\":\"chapter\"}]'\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(root, "cache")
	if err := os.Mkdir(cacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "victim.live_chat.json")
	content := []byte(strings.Repeat("invalid cache\n", 64))
	if err := os.WriteFile(victim, content, 0600); err != nil {
		t.Fatal(err)
	}
	chat := NewChatEndService(nil, script, cacheDir)
	chapter := NewChapterService(nil, nil, nil, chat)
	for _, id := range []string{"../victim", "../12345678", "hVfDBfreYNI/../victim", "hVfDBfreYNI?list=x", "hVfDBfreYNI\n", "", strings.Repeat("a", 12)} {
		if _, _, err := chat.loadChat(id); !errors.Is(err, ErrInvalidVideoID) {
			t.Errorf("chat %q err=%v", id, err)
		}
		if _, err := chapter.fetchChapters(id); !errors.Is(err, ErrInvalidVideoID) {
			t.Errorf("chapter %q err=%v", id, err)
		}
	}
	got, err := os.ReadFile(victim)
	if err != nil || string(got) != string(content) {
		t.Fatalf("outside-cache file changed: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("invalid ID reached command: %v", err)
	}
	// 陽性対照。正常 ID は実際に同じコマンドを起動して章節を返す。
	chapters, err := chapter.fetchChapters("hVfDBfreYNI")
	if err != nil || len(chapters) != 1 || chapters[0].Title != "chapter" {
		t.Fatalf("valid chapters=%+v err=%v", chapters, err)
	}
	_, outcome, err := chat.fetchLiveChat("hVfDBfreYNI")
	if outcome != chatNoReplay || err == nil {
		t.Fatalf("valid fetch outcome=%v err=%v", outcome, err)
	}
	calls, err := os.ReadFile(marker)
	if err != nil || string(calls) != "called\ncalled\n" {
		t.Fatalf("valid command invocations=%q err=%v", calls, err)
	}
}
