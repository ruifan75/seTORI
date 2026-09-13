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
//
// **期待値と完全一致で見る。** 部分一致だと `EXISTS` → `NOT EXISTS`、末尾に
// `OR TRUE` を足す、といった「呼び出しは在るが効いていない」改変が通る
// ── 実際 3 回、部分一致を継ぎ足して塞ごうとして毎回別の穴が残った。
func TestVisibleChannelExpr(t *testing.T) {
	const want = "EXISTS (SELECT 1 FROM stream_singers ss" +
		" JOIN singers si ON si.id = ss.singer_id" +
		" WHERE ss.stream_id = st.id AND si.is_hidden = FALSE)"
	if got := VisibleChannelExpr("st"); got != want {
		t.Errorf("判定式が変わっている\n got: %q\nwant: %q", got, want)
	}
}

// **件数と一覧は同じ条件から作る。** 一覧から落としても件数が合わなければ、
// 何件伏せたかが残る（秘匿の件数と同じ話）。条件を別々に書き下ろせる限り
// 必ずずれるので、`streamListFilter` に集約して**書き下ろせなくして**ある。
//
// ここも**完全一致**で見る。ソースを読んで「AND が在るか」を調べる形にしていた
// ときは、`AND … = FALSE` のように前後の形だけ合わせた改変が通っていた。
func TestStreamListFilter(t *testing.T) {
	t.Run("公開は非表示とチャンネル範囲の両方で絞る", func(t *testing.T) {
		want := "s.is_hidden = FALSE AND " + VisibleChannelExpr("s")
		if got := streamListFilter("s", false); got != want {
			t.Errorf("公開の条件が変わっている\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("編集者向けは両方とも外す", func(t *testing.T) {
		if got := streamListFilter("s", true); got != "TRUE" {
			t.Errorf("includeHidden で絞っている: %q", got)
		}
	})

	// alias を渡すのは、配信一覧（streams）とタグ別（s）で別名を使うため。
	// ここを固定しないと、片方で別のテーブルの列を見ても気付けない。
	t.Run("alias が両方の条件に効く", func(t *testing.T) {
		got := streamListFilter("zz", false)
		for _, want := range []string{"zz.is_hidden = FALSE", "ss.stream_id = zz.id"} {
			if !strings.Contains(got, want) {
				t.Errorf("alias が効いていない（%q が無い）: %q", want, got)
			}
		}
	})
}
