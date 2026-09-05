package service

import (
	"testing"
)

// 設定の既定は**無効**であること。
// 外部 API と AI を自動で叩く仕組みなので、入れただけで動き出してよいものではない。
// （`singers.auto_fill_enabled` と同じ考え方。CLAUDE.md §3 の既定を取り違えた件）
func TestAutoFillDefaultsToDisabled(t *testing.T) {
	// 既定値だけを見る。**GetSettings に nil の repo を渡さない** ──
	// 「repo が無ければ既定」を許すと、DI の配線漏れが
	// 「自動処理が静かに動かない」形で隠れてしまう。
	got := defaultAutoFillSettings()
	if got.Enabled {
		t.Error("既定で有効になっている。設定しない限り動いてはいけない")
	}
	if got.IntervalHours <= 0 || got.RefreshDays <= 0 {
		t.Errorf("既定値が実行に使えない: interval=%d refresh=%d", got.IntervalHours, got.RefreshDays)
	}
}

// 二重起動しないこと。**判定と旗立てを同じロックで行う**必要がある
// （分けると 2 つの tick が同時に通り抜ける）。
func TestAutoFillRefusesConcurrentRun(t *testing.T) {
	s := &AutoFillService{}
	s.running = true

	if _, err := s.RunOnce(); err == nil {
		t.Fatal("実行中なのに 2 つ目が通ってしまった")
	}

	// 弾かれた側が旗を落とさないこと（落とすと本物の実行が野放しになる）
	if !s.running {
		t.Error("弾かれた呼び出しが running を落としている")
	}
}
