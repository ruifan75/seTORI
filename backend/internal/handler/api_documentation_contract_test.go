package handler

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// 2つの背景処理の開始契約は文書から読み、実際のhandlerの応答と照合する。
// 入力/応答の全201行の意味を保証するテストではない。
func TestAPIDocumentedMaintenanceResponses(t *testing.T) {
	routes := loadAPIDocumentation(t)
	for _, tc := range []struct{ kind, pattern string }{
		{"readings_backfill", "POST /api/ai/backfill-readings"},
		{"duplicate_scan", "POST /api/songs/merge-candidates/scan"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			doc, ok := routes[tc.pattern]
			if !ok {
				t.Fatalf("route missing from docs: %s", tc.pattern)
			}
			status, keys, conflict := apiDocStartResponse(t, doc)
			r, d := newMaintenanceRouter(t, &maintenanceAI{})
			d.empty = true // AIや外部APIを使わず、開始・予約の契約だけを検査する。
			d.gate = make(chan struct{})
			d.entered = make(chan struct{})
			var release sync.Once
			t.Cleanup(func() {
				release.Do(func() { close(d.gate) })
				// 応答のキーが変わった負のコントロールでも、実行IDをDBから拾って後始末する。
				d.mu.Lock()
				var id string
				for key := range d.tasks {
					id = key
				}
				d.mu.Unlock()
				if id != "" {
					waitMaintenance(t, r, id)
				}
				d.mu.Lock()
				defer d.mu.Unlock()
				if len(d.bad) > 0 {
					t.Errorf("unexpected SQL: %v", d.bad)
				}
			})
			first := startMaintenance(t, r, tc.kind)
			if first.Code != status {
				t.Fatalf("docs/API.md:%d: start status=%d, document says %d", doc.line, first.Code, status)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			var actual []string
			for key := range body {
				actual = append(actual, key)
			}
			sort.Strings(actual)
			if strings.Join(actual, ",") != strings.Join(keys, ",") {
				t.Fatalf("docs/API.md:%d: start keys=%v, document says %v", doc.line, actual, keys)
			}
			select {
			case <-d.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("worker did not start")
			}
			if again := startMaintenance(t, r, tc.kind); again.Code != conflict {
				t.Fatalf("docs/API.md:%d: duplicate status=%d, document says %d", doc.line, again.Code, conflict)
			}
		})
	}
}

// この2行は「202 / `{task_id, message}`。二重起動は 409」の形式で記載する。
// コードやDTOから期待するコード・キーを生成しない。
func apiDocStartResponse(t *testing.T, route documentedAPIRoute) (int, []string, int) {
	t.Helper()
	start := regexp.MustCompile("^([1-5][0-9]{2}) / `\\{([a-z_]+(?:, *[a-z_]+)*)\\}`").FindStringSubmatch(route.response)
	conflict := regexp.MustCompile("二重起動は ([1-5][0-9]{2})").FindStringSubmatch(route.response)
	if len(start) != 3 || len(conflict) != 2 {
		t.Fatalf("docs/API.md:%d: expected status / JSON keys and duplicate status, got %s", route.line, route.response)
	}
	status, _ := strconv.Atoi(start[1])
	duplicate, _ := strconv.Atoi(conflict[1])
	keys := strings.Split(start[2], ",")
	for i := range keys {
		keys[i] = strings.TrimSpace(keys[i])
	}
	sort.Strings(keys)
	return status, keys, duplicate
}
