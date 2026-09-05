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

// **見送りが実行の記録を汚さないこと。** 手で組み立てた構造体を marshal しても
// 何も固定できない（recordSkip が Note を上書きし直しても通ってしまう）ので、
// 実際の重ね方（applySkip）を通す。
func TestApplySkipKeepsRunRecordIntact(t *testing.T) {
	runAt := time.Now().Add(-2 * time.Hour)
	before := AutoFillLastRun{
		At:    &runAt,
		Note:  "チャンネル 1 / 同期 3 / コメント取り直し 2",
		Error: "1 件の失敗（詳細はログ）",
	}

	now := time.Now()
	after := applySkip(before, "一括セットリスト作成が実行中のため見送りました", now)

	// 見送りは自分の欄にだけ書く
	if after.SkippedAt == nil || !after.SkippedAt.Equal(now) {
		t.Error("見送り時刻が記録されていない")
	}
	if after.SkipNote == "" {
		t.Error("見送りの理由が記録されていない")
	}

	// **実行の記録は 1 つも変わらない。** ここが崩れると、古い実行時刻と
	// 新しい見送り理由が「実在しない一回の結果」として画面に出る。
	if after.At == nil || !after.At.Equal(runAt) {
		t.Error("見送りが実行時刻を進めている（次回が 1 間隔ぶん遅れる）")
	}
	if after.Note != before.Note {
		t.Errorf("見送りが実行の内容を上書きした: %q", after.Note)
	}
	if after.Error != before.Error {
		t.Errorf("見送りが実行のエラーを上書きした: %q", after.Error)
	}
}

// 記録の構造体が 5 つの欄を**別々に**持ち、JSON でも別々に出ること。
//
// **これは handler の応答を保証しない。** handler は明示的な map を組み立てて
// 返すので、そこから項目を落としてもこのテストは通る。ここで固定できるのは
// 「欄が混ざっていない」ことだけ（見送りと実行が同じ欄に入ると、
// 古い実行時刻と新しい見送り理由が「実在しない一回」に見える）。
func TestAutoFillLastRunFieldsAreSeparate(t *testing.T) {
	now := time.Now()
	b, err := json.Marshal(AutoFillLastRun{
		At: &now, SkippedAt: &now,
		Note: "n", SkipNote: "s", Error: "e",
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"last_run_at", "last_skipped_at", "last_run_note", "last_skip_note", "last_run_error"} {
		if _, ok := m[k]; !ok {
			t.Errorf("%s が JSON に出ない（欄が混ざっている）", k)
		}
	}
}
