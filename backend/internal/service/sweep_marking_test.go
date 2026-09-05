package service

import (
	"strings"
	"testing"
)

// 自動で `is_processed` を立てる条件は、**間違えても静かに壊れる**種類の判断。
// 立てすぎれば「歌があるのに二度と拾われない配信」ができ、立てなければ
// 掃く意味が無くなる。条件がソースから消えていないかを見る。
//
// SQL・実行順の意味までは見られないが、**消えたことには気付ける**。
func TestSweepMarkingConditions(t *testing.T) {
	svc := readFileForTest(t, "batch_analyze_service.go")
	repo := readFileForTest(t, "../repository/stream_repository.go")

	t.Run("成功・非表示・未処理・0 曲のときだけ立てる", func(t *testing.T) {
		for _, cond := range []string{
			"outcome == batchOutcomeDone", // 劣化・見送り・失敗では立てない
			"!emptyByDesign",              // 「今コメントが無い」は「歌が無い」ではない
			"songs == 0",                  // -1（保存失敗）もここで弾かれる
			"stream.IsHidden",             // 表示中は Holodex や章節から歌単ができうる
		} {
			if !strings.Contains(svc, cond) {
				t.Errorf("標記の条件から %q が消えている", cond)
			}
		}
	})

	t.Run("0 曲のときだけ保存できたかを見る", func(t *testing.T) {
		// SaveCommentSongs の DB エラーはログだけで err にならない。
		// Saved を見ないと、キャッシュが無いまま処理済みになり
		// refresh の対象（is_processed = FALSE）から永久に外れる。
		//
		// **ただし Saved=false は障害の印ではない** ── キャッシュ命中は
		// 仕様として false を返す。曲数より先に評価すると、正常な経路を
		// 毎回警告として報告することになる。
		if !strings.Contains(svc, "if len(resp.Songs) == 0 && (resp.Stats == nil || !resp.Stats.Saved)") {
			t.Error("保存確認が 0 曲のときに限られていない")
		}
		if !strings.Contains(svc, "return batchOutcomeDone, -1") {
			t.Error("保存失敗を曲数 0 と区別していない")
		}
	})

	t.Run("前提は書き込み側でも確かめる", func(t *testing.T) {
		// 呼び出し側が持っているのは列挙時と分析時のスナップショット。
		// 処理中に編集者が表示へ戻す／同期が新しいコメントを入れてキャッシュを
		// NULL に戻す、のどちらも起きうるので UPDATE でも確かめる。
		for _, cond := range []string{
			"AND is_hidden AND NOT is_processed",
			"jsonb_typeof(comment_songs) = 'array' AND jsonb_array_length(comment_songs) = 0",
		} {
			if !strings.Contains(repo, cond) {
				t.Errorf("MarkProcessedIfHiddenAndEmpty の書き込み条件から %q が消えている", cond)
			}
		}
	})
}
