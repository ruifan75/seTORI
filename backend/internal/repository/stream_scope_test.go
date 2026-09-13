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
//
// **分岐ごとに見る。** `FindAll` は includeHidden で 2 通りに分かれるので、
// まとめて見ると「通常表示から外して includeHidden 側へ移す」改変を見逃す
// ── そのとき件数は濾していて一覧だけ素通りになる。
func TestStreamListsFilterCountAndRowsTogether(t *testing.T) {
	src := readSourceFile(t, "stream_repository.go")

	t.Run("FindAll は通常表示の件数と一覧だけを濾す", func(t *testing.T) {
		body := funcBody(t, src, "func (r *StreamRepository) FindAll")
		// includeHidden の分岐は 2 つある。1 つ目が件数、2 つ目が一覧。
		countIf, countElse := ifElseBlocks(t, body, "includeHidden", 0)
		listIf, listElse := ifElseBlocks(t, body, "includeHidden", 1)
		if !strings.Contains(listElse, "streamListQuery(") {
			t.Fatalf("2 つ目の分岐が一覧ではない: %q", listElse)
		}

		for name, part := range map[string]string{"件数": countElse, "一覧": listElse} {
			assertAndedVisibleChannel(t, "通常表示の"+name, part)
		}
		for name, part := range map[string]string{"件数": countIf, "一覧": listIf} {
			if strings.Contains(part, "VisibleChannelExpr(") {
				t.Errorf("includeHidden 側の%sまで濾している（全部見せる分岐のはず）: %q", name, part)
			}
		}
	})

	t.Run("FindByTagID は件数と一覧の両方を濾す", func(t *testing.T) {
		body := funcBody(t, src, "func (r *StreamRepository) FindByTagID")
		countPart, listPart := splitAtStreamList(t, body, "FindByTagID")
		assertAndedVisibleChannel(t, "件数", countPart)
		assertAndedVisibleChannel(t, "一覧", listPart)
	})
}
