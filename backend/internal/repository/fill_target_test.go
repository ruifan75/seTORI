package repository

import (
	"strings"
	"testing"
)

// 一括の対象条件は SQL 文字列で組み立てるので、**壊れてもコンパイルは通る**。
// 実 DB を使うテスト基盤が無いので、せめてモードごとの生成結果を固定する
// （JSONB の意味そのものは PostgreSQL を使わないと固定できない）。
func TestFillTargetWhere(t *testing.T) {
	unprocessed := fillTargetWhere("unprocessed")
	force := fillTargetWhere("force")

	t.Run("comment_raw は非空の配列で絞る", func(t *testing.T) {
		// `IS NOT NULL AND != 'null'` では `[]` が通り、「保存済みの入力を処理し直す」
		// つもりの実行が遠隔からの再取得に化ける（CLAUDE.md §6.1）。
		// 実測：これで通っていた 9 本はすべて会限で comment_raw = []。
		for _, w := range []string{unprocessed, force} {
			if !strings.Contains(w, "jsonb_array_length(s.comment_raw) > 0") {
				t.Error("comment_raw の空配列を弾いていない")
			}
			if strings.Contains(w, "s.comment_raw != 'null'") {
				t.Error("空配列が通る古い条件が残っている")
			}
		}
	})

	t.Run("入力源は OR で保つ", func(t *testing.T) {
		// comment_raw が空でも、Holodex の曲・抽出済み・章節があれば対象に残る。
		// AND にすると、入力源が 1 つしか無い配信を全部落とす。
		for _, src := range []string{
			"s.holodex_data->'songs'", "s.comment_songs", "s.comment_raw", "s.chapter_raw",
		} {
			if !strings.Contains(unprocessed, src) {
				t.Errorf("入力源 %s が条件から消えている", src)
			}
		}
		if strings.Count(unprocessed, "OR (") != 3 {
			t.Errorf("入力源の OR が 3 つでない（AND に変わっていないか）: %s", unprocessed)
		}
	})

	t.Run("処理済みを除くのは unprocessed だけ", func(t *testing.T) {
		// ここでの unprocessed は「歌唱記録が無い」で、is_processed 列とは別物。
		// 見ないと、人が「この配信に歌は無い」と確認しても毎回やり直す。
		if !strings.Contains(unprocessed, "NOT s.is_processed") {
			t.Error("unprocessed が処理済みを除外していない")
		}
		// force は「全部もう一度考え直す」明示的な口なので、人の裁定で絞らない。
		if strings.Contains(force, "NOT s.is_processed") {
			t.Error("force が処理済みを除外している（明示的な再検討ができない）")
		}
		if strings.Contains(force, "NOT EXISTS (SELECT 1 FROM performances") {
			t.Error("force が歌唱ありを除外している")
		}
	})

	t.Run("非表示は常に除く", func(t *testing.T) {
		for _, w := range []string{unprocessed, force} {
			if !strings.Contains(w, "s.is_hidden = FALSE") {
				t.Error("非表示を除外していない")
			}
		}
	})
}
