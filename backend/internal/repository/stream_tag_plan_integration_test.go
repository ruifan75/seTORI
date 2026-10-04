package repository

import (
	"encoding/json"
	"github.com/lib/pq"
	"testing"
)

// 小さい fixture の速さを一般化しない。専用 schema に 10,000 配信を作り、発行 SQL の実行計画を記録する。
func TestStreamTagFilterPlans(t *testing.T) {
	db := reviewTestDB(t)
	if _, err := db.Exec(`INSERT INTO singers(id,name,is_hidden) VALUES ('plan','計画',false);
 INSERT INTO streams(id,title,stream_date,is_hidden) SELECT 'plan_'||g,'配信'||g,'2026-01-01'::timestamptz+g*interval '1 minute',false FROM generate_series(1,10000) g;
 INSERT INTO stream_singers(stream_id,singer_id,is_owner) SELECT id,'plan',true FROM streams;
 INSERT INTO stream_stream_tags(stream_id,tag_id) SELECT id,'singing' FROM streams;
 INSERT INTO stream_stream_tags(stream_id,tag_id) SELECT id,'3d' FROM streams WHERE substring(id from 6)::int % 2=0;
 ANALYZE streams; ANALYZE stream_singers; ANALYZE singers; ANALYZE stream_stream_tags;`); err != nil {
		t.Fatal(err)
	}
	for _, tags := range [][]string{{}, {"singing"}, {"singing", "3d"}} {
		recorded, rec := newRecordingDB(t)
		repo := NewStreamRepository(recorded)
		repo.FindAll(20, 0, false, "date", "desc", tags)
		repo.CountByTagForList(tags)
		for i, query := range rec.all() {
			args := []any{pq.Array(tags)}
			if i == 1 {
				args = append(args, 20, 0)
			}
			var raw []byte
			if err := db.QueryRow("EXPLAIN (ANALYZE, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plan []struct {
				Plan struct {
					TotalCost float64 `json:"Total Cost"`
				}
				ExecutionTime float64         `json:"Execution Time"`
				JIT           json.RawMessage `json:"JIT"`
			}
			if err := json.Unmarshal(raw, &plan); err != nil {
				t.Fatal(err)
			}
			t.Logf("tags=%v query=%d cost=%.2f execution=%.2fms", tags, i, plan[0].Plan.TotalCost, plan[0].ExecutionTime)
			if plan[0].Plan.TotalCost >= 100000 || len(plan[0].JIT) > 0 {
				t.Fatalf("1 万配信で JIT 境界を超えた: %s", raw)
			}
		}
	}
}
