package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// info.json は動画 ID を持っているので、取り違えを**機械で確かめられる**
// （live chat のファイルには ID がどこにも入っていない）。確かめられるものは
// 確かめる ── 別の配信のコメントから歌単を作ると、誤りが歌唱記録として残り、
// あとから来た人には由来が分からない。
func TestParseInfoJSONRejectsOtherStream(t *testing.T) {
	data := []byte(`{"id":"OTHER123","title":"別の配信","comments":[{"text":"0:00 曲","parent":"root"}]}`)

	_, texts, err := parseInfoJSON("WANTED123", data)
	if !errors.Is(err, ErrInfoJSONMismatch) {
		t.Fatalf("err = %v, want ErrInfoJSONMismatch", err)
	}
	if texts != nil {
		t.Errorf("取り違えなのに取り込む中身を返している: %v", texts)
	}
	// どの配信のものだったかを人に返す（「読めません」だけだと取り直しへ誘導してしまう）。
	if !strings.Contains(err.Error(), "OTHER123") {
		t.Errorf("err = %q, 相手の動画 ID を含めてほしい", err)
	}
}

func TestParseInfoJSONRejectsNonInfoJSON(t *testing.T) {
	// live_chat.json の 1 行。JSON としては正しいが info.json ではない。
	data := []byte(`{"replayChatItemAction":{"videoOffsetTimeMsec":"0","actions":[]}}`)
	if _, _, err := parseInfoJSON("abc", data); !errors.Is(err, ErrInfoJSONUnreadable) {
		t.Fatalf("err = %v, want ErrInfoJSONUnreadable", err)
	}
}

// 並びは「関連度の高い順」に寄せる。既存の取得経路は YouTube API を
// order=relevance で叩いており、抽出規則はその並びの前提で調整されている。
func TestParseInfoJSONOrdersPinnedAndLikedFirst(t *testing.T) {
	data := []byte(`{"id":"v1","comments":[
		{"text":"ふつうの感想","parent":"root","like_count":2},
		{"text":"0:00 開始\n3:21 曲名 / アーティスト","parent":"root","like_count":40},
		{"text":"固定された歌単","parent":"root","is_pinned":true,"like_count":1},
		{"text":"   ","parent":"root"},
		{"text":"返信です","parent":"UgxSomething"}
	]}`)

	res, texts, err := parseInfoJSON("v1", data)
	if err != nil {
		t.Fatalf("parseInfoJSON: %v", err)
	}
	if texts[0] != "固定された歌単" {
		t.Errorf("先頭 = %q, 固定コメントを先頭にしてほしい", texts[0])
	}
	if !strings.HasPrefix(texts[1], "0:00 開始") {
		t.Errorf("2 番目 = %q, いいねの多い順にしてほしい", texts[1])
	}
	// 本文が空のものは落とす。返信は**落とさない** ── 他に取りようが無い配信の
	// ための口なので、取り逃すより構造フィルタに任せる。
	if res.Saved != 4 {
		t.Errorf("Saved = %d, want 4（空の 1 件だけ落とす）", res.Saved)
	}
	if res.Total != 5 || res.TopLevel != 4 {
		t.Errorf("Total/TopLevel = %d/%d, want 5/4", res.Total, res.TopLevel)
	}
	// 時刻表記の件数は、抽出そのものと同じ関数で数える。
	if res.WithTimes != 1 {
		t.Errorf("WithTimes = %d, want 1", res.WithTimes)
	}
}

// 素通しでキャッシュへ書くと、壊れたファイルが「拍手が 0 件だった」という結論として
// 確定し、しかもキャッシュなので force 分析でも回復しない（§6.5 で踏んだ穴）。
func TestImportLiveChatRejectsTruncatedFile(t *testing.T) {
	dir := t.TempDir()
	svc := NewChatEndService(nil, "", dir)

	good := `{"replayChatItemAction":{"videoOffsetTimeMsec":"1000","actions":[{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":{"message":{"runs":[{"text":"888"}]}}}}}]}}`
	// ダウンロードの中断は「正常な行のあとで切れる」形になる。
	truncated := good + "\n" + `{"replayChatItemAction":{"videoOffsetTi`

	if _, err := svc.ImportLiveChat("aaaaaaaaaaa", strings.NewReader(truncated)); !errors.Is(err, ErrLiveChatUnreadable) {
		t.Fatalf("err = %v, want ErrLiveChatUnreadable", err)
	}
	// **弾いたものを置いていかない。** 中途半端なファイルが残ると、それが
	// そのままキャッシュとして読まれる。
	if _, err := os.Stat(filepath.Join(dir, "aaaaaaaaaaa.live_chat.json")); !os.IsNotExist(err) {
		t.Error("弾いたのにファイルが置かれている")
	}
}

func TestImportLiveChatAcceptsAndSummarizes(t *testing.T) {
	dir := t.TempDir()
	svc := NewChatEndService(nil, "", dir)

	lines := []string{
		chatLine("1000", "888"),    // 拍手
		chatLine("2000", "888888"), // 拍手
		chatLine("3000", "かわいい"),   // 拍手ではない
		chatLine("600000", "👏"),    // 拍手
	}
	res, err := svc.ImportLiveChat("bbbbbbbbbbb", strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("ImportLiveChat: %v", err)
	}
	if res.Messages != 4 || res.Applause != 3 {
		t.Errorf("Messages/Applause = %d/%d, want 4/3", res.Messages, res.Applause)
	}
	if res.FirstAtSec != 1 || res.LastAtSec != 600 {
		t.Errorf("範囲 = %.0f〜%.0f, want 1〜600", res.FirstAtSec, res.LastAtSec)
	}

	// 置いたものは読み直せる（画面が取り違えに気付けるようにするため）。
	got, ok := svc.CachedLiveChat("bbbbbbbbbbb")
	if !ok || got.Applause != 3 {
		t.Errorf("CachedLiveChat = %+v, ok=%v", got, ok)
	}

	// **消せることが手動取り込みを許す条件。** ファイルがあると yt-dlp は
	// 呼ばれず force 分析でも読み直さない。
	if err := svc.DeleteCachedLiveChat("bbbbbbbbbbb"); err != nil {
		t.Fatalf("DeleteCachedLiveChat: %v", err)
	}
	if _, ok := svc.CachedLiveChat("bbbbbbbbbbb"); ok {
		t.Error("消したのに残っている")
	}
}

func chatLine(offsetMs, text string) string {
	return `{"replayChatItemAction":{"videoOffsetTimeMsec":"` + offsetMs +
		`","actions":[{"addChatItemAction":{"item":{"liveChatTextMessageRenderer":{"message":{"runs":[{"text":"` +
		text + `"}]}}}}}]}}`
}

// 中身をメモリへ載せずに一時ファイルへ流すようにしたので、**流している途中で
// 失敗する**経路ができた（上限に当たる・接続が切れる）。そのとき書きかけの
// ファイルが残ると、次にそれがキャッシュとして読まれる。
func TestImportLiveChatLeavesNothingWhenStreamFails(t *testing.T) {
	dir := t.TempDir()
	svc := NewChatEndService(nil, "", dir)

	src := io.MultiReader(
		strings.NewReader(chatLine("1000", "888")+"\n"),
		errReader{errors.New("connection reset")},
	)
	if _, err := svc.ImportLiveChat("ccccccccccc", src); err == nil {
		t.Fatal("途中で切れたのに成功している")
	}
	for _, name := range []string{"ccccccccccc.live_chat.json", "ccccccccccc.live_chat.json.tmp"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s が残っている", name)
		}
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

// ID はパスの一部として届くので、`..%2F..%2F` のような値が入りうる。
// `filepath.Join` は `../` を正規化するので、検証しないとキャッシュディレクトリの
// 外の `.live_chat.json` に手が届く。
//
// **認可を通ったことは、値が安全であることを意味しない。**
func TestLiveChatPathsRejectNonVideoIDs(t *testing.T) {
	dir := t.TempDir()
	svc := NewChatEndService(nil, "", dir)

	// キャッシュディレクトリの外に「本物らしい」ファイルを置いておく。
	outside := filepath.Join(filepath.Dir(dir), "secret.live_chat.json")
	if err := os.WriteFile(outside, []byte(chatLine("1000", "888")), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	for _, id := range []string{
		"../secret",     // 親ディレクトリへ
		"./aaaaaaaaaaa", // 正規化すると本物の ID になる
		"",              //
		"aaaaaaaaaa",    // 10 文字
		"aaaaaaaaaaaa",  // 12 文字
		"aaaaaaaaaa/",   //
		"aaaaaaaaa.a",   // 記号
	} {
		t.Run(id, func(t *testing.T) {
			if _, err := svc.ImportLiveChat(id, strings.NewReader(chatLine("1000", "888"))); !errors.Is(err, ErrInvalidVideoID) {
				t.Errorf("ImportLiveChat: err = %v, want ErrInvalidVideoID", err)
			}
			if _, ok := svc.CachedLiveChat(id); ok {
				t.Error("CachedLiveChat が外のファイルを読んでいる")
			}
			if err := svc.DeleteCachedLiveChat(id); !errors.Is(err, ErrInvalidVideoID) {
				t.Errorf("DeleteCachedLiveChat: err = %v, want ErrInvalidVideoID", err)
			}
		})
	}

	// 外のファイルは消えていない（DeleteCachedLiveChat が届いていない）。
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("ディレクトリ外のファイルが消えている: %v", err)
	}
}

// **空の取得結果で既にある入力を消さない。**
//
// 会限配信では YouTube が 403 を返し、Holodex は空配列を**正常応答**で返すので、
// 取り直しは「成功・0 件」になる。そのまま保存すると、編集者が手元から
// 持ち込んだコメントを定期実行が消す ── 救うはずの入力を自動処理が奪う。
func TestStoredCommentCountTreatsEmptyAsNoInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"未取得", "", 0},
		{"JSON の null", "null", 0},
		{"空配列", "[]", 0},
		{"壊れた値", "{oops", 0},
		{"中身あり", `["0:00 開始","3:21 曲名"]`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := storedCommentCount([]byte(tc.raw))
			if got != tc.want {
				t.Errorf("storedCommentCount(%q) = %d, want %d", tc.raw, got, tc.want)
			}
			// 取得 0 件のときだけ見送る。**取れたなら上書きしてよい**
			// （取り直しの本来の役目）。
			if got := keepStoredComments(0, []byte(tc.raw)); got != (tc.want > 0) {
				t.Errorf("keepStoredComments(0, %q) = %v, want %v", tc.raw, got, tc.want > 0)
			}
			if keepStoredComments(3, []byte(tc.raw)) {
				t.Errorf("取得できているのに見送っている (%q)", tc.raw)
			}
		})
	}
}
