package service

import (
	"github.com/ruifan75/setori/internal/dto"
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

	t.Run("保存失敗は0曲と区別しキャッシュ命中は通す", func(t *testing.T) {
		// ソースの字面ではなく、実際の一括処理の結果を確かめる。
		// Saved=falseでもcacheなら正常、抽出結果を保存できなかった回は -1。
		for _, tc := range []struct {
			name, path string
			songs      []dto.CommentSong
			want       int
		}{
			{"0曲を保存できなかった", "grouped", []dto.CommentSong{}, -1},
			{"非空を保存できなかった", "grouped", []dto.CommentSong{{}}, -1},
			{"キャッシュ命中", "cache", []dto.CommentSong{{}}, 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				b := &BatchAnalyzeService{commentService: analyzedComments{&dto.AnalyzeCommentsResponse{Songs: tc.songs, Stats: &dto.AnalyzeStats{Path: tc.path, Saved: false}}}}
				outcome, songs := b.processOne("video", false)
				wantOutcome := batchOutcomeDone
				if tc.want < 0 {
					wantOutcome = batchOutcomeFailed
				}
				if outcome != wantOutcome || songs != tc.want {
					t.Fatalf("outcome=%v songs=%d want%d", outcome, songs, tc.want)
				}
			})
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
