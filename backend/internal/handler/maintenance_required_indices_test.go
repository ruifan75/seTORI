package handler

import (
	"fmt"
	"testing"

	"github.com/ruifan75/setori/internal/repository"
)

// 欠落/null を番号 0 として採用しない。handler と固定 SQL driver を通す。
func TestReadingTaskRequiresExplicitIndex(t *testing.T) {
	for _, tc := range []struct {
		name, reply, reason string
	}{
		{"missing", `[{"reading":"こう","confidence":0.95},{"index":2,"reading":"へい","confidence":0.9}]`, "AI の読み候補に index がありません"},
		{"null", `[{"index":null,"reading":"こう","confidence":0.95},{"index":2,"reading":"へい","confidence":0.9}]`, "AI の読み候補に index がありません"},
		{"null-item", `[null,{"index":2,"reading":"へい","confidence":0.9}]`, "AI の読み候補に index がありません"},
		{"null-array", `null`, "AI の読み候補が配列ではありません"},
		{"negative", `[{"index":-1,"reading":"こう","confidence":0.95}]`, "AI の読み候補の index が不正です: -1"},
		{"out-of-range", `[{"index":3,"reading":"こう","confidence":0.95}]`, "AI の読み候補の index が不正です: 3"},
		{"duplicate", `[{"index":0,"reading":"こう","confidence":0.95},{"index":0,"reading":"こう","confidence":0.95}]`, "AI の読み候補の index が不正です: 0"},
		{"zero-is-valid", `[{"index":0,"reading":"こう","confidence":0.95}]`, ""},
		{"empty-array-is-valid", `[]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &maintenanceAI{replies: []string{tc.reply, maintenanceSongReply}, errors: make([]error, 2)}
			r, d := newMaintenanceRouter(t, a)
			task := waitMaintenance(t, r, maintenanceTaskID(t, startMaintenance(t, r, "readings_backfill")))
			status, counts, failures := "done", [5]int{5, 5, 3, 2, 0}, []repository.TaskFailure{}
			saved := []string{maintenanceIDs[0], maintenanceIDs[3], maintenanceIDs[4]}
			if tc.name == "empty-array-is-valid" {
				counts = [5]int{5, 5, 2, 3, 0}
				saved = []string{maintenanceIDs[3], maintenanceIDs[4]}
			}
			if tc.reason != "" {
				status, counts = "failed", [5]int{5, 5, 2, 0, 3}
				saved = []string{maintenanceIDs[3], maintenanceIDs[4]}
				for _, id := range maintenanceIDs[:3] {
					failures = append(failures, repository.TaskFailure{Target: "artist:" + id, Reason: "アーティスト名の AI 補完に失敗: " + tc.reason})
				}
			}
			assertMaintenance(t, d, task, status, counts, failures)
			d.mu.Lock()
			got := fmt.Sprint(d.saved)
			d.mu.Unlock()
			if got != fmt.Sprint(saved) {
				t.Fatalf("saved=%s want=%v", got, saved)
			}
		})
	}
}

func TestDuplicateScanRequiresBothIndices(t *testing.T) {
	for _, tc := range []struct{ name, invalid string }{
		{"missing-a", `{"b":2}`}, {"missing-b", `{"a":2}`},
		{"null-a", `{"a":null,"b":2}`}, {"null-b", `{"a":2,"b":null}`}, {"null-pair", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &maintenanceAI{replies: []string{`[{"a":1,"b":2},` + tc.invalid + `]`}, errors: make([]error, 1)}
			r, d := newMaintenanceRouter(t, a)
			task := waitMaintenance(t, r, maintenanceTaskID(t, startMaintenance(t, r, "duplicate_scan")))
			assertMaintenance(t, d, task, "failed", [5]int{3, 3, 2, 0, 1}, []repository.TaskFailure{{Target: "AI 走査", Reason: "候補に曲番号がありません"}})
			d.mu.Lock()
			got := fmt.Sprint(d.saved)
			d.mu.Unlock()
			want := []string{maintenanceIDs[1] + "/" + maintenanceIDs[0], maintenanceIDs[1] + "/" + maintenanceIDs[2]}
			if got != fmt.Sprint(want) {
				t.Fatalf("saved=%s want=%v", got, want)
			}
		})
	}
	// 有効な 0 を拒否する修正も弾く。
	t.Run("zero-is-valid", func(t *testing.T) {
		a := &maintenanceAI{replies: []string{`[{"a":0,"b":2}]`}, errors: make([]error, 1)}
		r, d := newMaintenanceRouter(t, a)
		task := waitMaintenance(t, r, maintenanceTaskID(t, startMaintenance(t, r, "duplicate_scan")))
		assertMaintenance(t, d, task, "done", [5]int{2, 2, 2, 0, 0}, []repository.TaskFailure{})
		d.mu.Lock()
		got := fmt.Sprint(d.saved)
		d.mu.Unlock()
		want := []string{maintenanceIDs[1] + "/" + maintenanceIDs[0], maintenanceIDs[0] + "/" + maintenanceIDs[2]}
		if got != fmt.Sprint(want) {
			t.Fatalf("saved=%s want=%v", got, want)
		}
	})
}

// null は「候補なし」の [] と区別して失敗にする。曲名キーの保存結果は残す。
func TestDuplicateScanRequiresArray(t *testing.T) {
	a := &maintenanceAI{replies: []string{"null"}, errors: make([]error, 1)}
	r, d := newMaintenanceRouter(t, a)
	task := waitMaintenance(t, r, maintenanceTaskID(t, startMaintenance(t, r, "duplicate_scan")))
	assertMaintenance(t, d, task, "failed", [5]int{2, 2, 1, 0, 1}, []repository.TaskFailure{{Target: "AI 走査", Reason: "AI の候補が配列ではありません"}})
	d.mu.Lock()
	got := fmt.Sprint(d.saved)
	d.mu.Unlock()
	want := []string{maintenanceIDs[1] + "/" + maintenanceIDs[0]}
	if got != fmt.Sprint(want) {
		t.Fatalf("saved=%s want=%v", got, want)
	}
}
