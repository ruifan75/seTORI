package repository

import (
	"strings"
	"testing"
)

// 一覧に出すチャンネルの判定は **VisibleChannelExpr に集約する**。
//
// 同期は description の mention からも参加者を作るので、この站が扱っていない
// チャンネルの配信が混ざる。歌手ページは `stream_singers` を JOIN しているので
// 参加者を外せば消えるのに、`/streams` とタグ別一覧は参加者を見ていなかったため
// 残り続けた ── 直したのに直っていないように見える（2026-09-13）。
func TestVisibleChannelExprUsesChannelVisibility(t *testing.T) {
	got := VisibleChannelExpr("st")
	for _, want := range []string{"stream_singers", "singers", "is_hidden = FALSE", "st.id"} {
		if !strings.Contains(got, want) {
			t.Errorf("判定に %q が無い: %q", want, got)
		}
	}
	// **チャンネル側の旗を読むこと。** 配信の is_hidden で代用すると、
	// 「チャンネルを表示にしたのに歌枠が出ない」を直すのにもう 1 か所触ることになる。
	if !strings.Contains(got, "si.is_hidden = FALSE") {
		t.Errorf("チャンネルの表示/非表示を見ていない: %q", got)
	}
}

// **件数と一覧は必ず対で濾す。** 一覧から落としても件数が合わなければ、
// 何件伏せたかが残る（秘匿の件数と同じ話）。実際、最初の実装では
// 文字列置換が件数の行に 2 回当たって**一覧のほうが素通り**していた。
func TestStreamListsFilterCountAndRowsTogether(t *testing.T) {
	src := readSourceFile(t, "stream_repository.go")

	for _, fn := range []string{"FindAll", "FindByTagID"} {
		body := funcBody(t, src, "func (r *StreamRepository) "+fn)
		if n := strings.Count(body, "VisibleChannelExpr("); n != 2 {
			t.Errorf("%s: VisibleChannelExpr の出現が %d 回（件数と一覧で 2 回のはず）", fn, n)
		}
		// 件数側（COUNT）と一覧側（streamListQuery / SELECT）の両方に入っていること。
		countPart, listPart := splitAtStreamList(t, body, fn)
		if !strings.Contains(countPart, "VisibleChannelExpr(") {
			t.Errorf("%s: 件数のクエリが濾していない", fn)
		}
		if !strings.Contains(listPart, "VisibleChannelExpr(") {
			t.Errorf("%s: 一覧のクエリが濾していない", fn)
		}
	}
}
