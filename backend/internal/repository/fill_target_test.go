package repository

import (
	"strings"
	"testing"
)

// 一括の対象条件は SQL 文字列で組み立てるので、**壊れてもコンパイルは通る**。
// 実測で問題になった 2 点を固定する。
func TestFillTargetConditions(t *testing.T) {
	// FindStreamsForFill が組み立てる where と同じ形を取り出せないので、
	// 実装のソースを読んで条件が残っているかを見る。SQL の意味までは見られないが、
	// **消えたことには気付ける**（消えると本番で毎時間 403 を叩く状態へ戻る）。
	src := readSource(t, "stream_repository.go")
	fill := section(t, src, "func (r *StreamRepository) FindStreamsForFill", "func (r *StreamRepository)")

	t.Run("comment_raw は非空の配列で絞る", func(t *testing.T) {
		// `IS NOT NULL AND != 'null'` では `[]` が通り、「保存済みの入力を処理し直す」
		// つもりの実行が遠隔からの再取得に化ける（CLAUDE.md §6.1）。
		// 実測：これで通っていた 9 本はすべて会限で comment_raw = []。
		// コメントは API key では読めないので毎時間 403 になっていた。
		if !strings.Contains(fill, "jsonb_array_length(s.comment_raw) > 0") {
			t.Error("comment_raw の空配列を弾いていない")
		}
		if strings.Contains(fill, "s.comment_raw != 'null'") {
			t.Error("空配列が通る古い条件が残っている")
		}
	})

	t.Run("処理済みは触らない", func(t *testing.T) {
		// ここでの unprocessed は「歌唱記録が無い」で、is_processed 列とは別物。
		// 見ないと、人が「この配信に歌は無い」と確認しても毎回やり直す。
		if !strings.Contains(fill, "NOT s.is_processed") {
			t.Error("人が処理済みにした配信を除外していない")
		}
	})
}
