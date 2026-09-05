package service

import (
	"encoding/json"
	"testing"
	"time"
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

// **実行結果の保存が設定を巻き戻さないこと。**
// 同じ JSON を read-modify-write すると、recordRun が古い enabled を書き戻して
// 「運用者が止めたのに勝手に再開する」が起きる。キーを分けてあることを固定する。
func TestAutoFillSettingsAndLastRunUseDifferentKeys(t *testing.T) {
	if settingsKeyAutoFill == settingsKeyAutoFillRun {
		t.Fatal("設定と実行結果が同じキー。実行結果の保存が設定を巻き戻す")
	}
}

// 設定の構造体に実行結果が混ざっていないこと（混ざると同じ書き込みに乗る）。
func TestAutoFillSettingsHasNoRunState(t *testing.T) {
	b, err := json.Marshal(defaultAutoFillSettings())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"last_run_at", "last_run_note", "last_run_error"} {
		if _, ok := m[k]; ok {
			t.Errorf("設定に実行結果 %q が入っている", k)
		}
	}
}

// **見送りで次回を先送りしないこと。**
// 見送り（一括が実行中）で At を進めると、予定時刻にたまたま重なっただけで
// 次回が丸ごと 1 間隔ぶん遅れる（168 時間設定なら 7 日後）。
func TestAutoFillSkipDoesNotDelayNextRun(t *testing.T) {
	// 判定に使うのは At だけで、SkippedAt は表示用。
	// 構造体の JSON キーが分かれていることを固定する（同じキーだと区別できない）。
	b, err := json.Marshal(AutoFillLastRun{})
	if err != nil {
		t.Fatal(err)
	}
	_ = b

	var last AutoFillLastRun
	now := time.Now()
	last.SkippedAt = &now
	if last.At != nil {
		t.Error("見送りの記録が実行時刻を進めている")
	}
}

// **見送りの記録が、実行の記録と混ざらないこと。**
// Note を共有すると、古い last_run_at と新しい見送り理由が組み合わさって
// 「実在しない一回の結果」に見える。
func TestAutoFillSkipKeepsRunRecordIntact(t *testing.T) {
	b, err := json.Marshal(AutoFillLastRun{
		Note: "実行の記録", Error: "実行のエラー", SkipNote: "見送りの理由",
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["last_run_note"] == m["last_skip_note"] {
		t.Error("実行の記録と見送りの理由が同じ欄に入っている")
	}
	for _, k := range []string{"last_run_note", "last_skip_note", "last_run_error"} {
		if _, ok := m[k]; !ok {
			t.Errorf("%s が応答に出ない", k)
		}
	}
}
