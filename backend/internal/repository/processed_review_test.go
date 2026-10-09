package repository

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"reflect"
	"testing"
	"time"
)

func processedDB(t *testing.T, steps []visibilityStep) (*ProcessedReviewRepository, *visibilityDriver) {
	v, d := visibilityDB(t, steps)
	return NewProcessedReviewRepository(v.db), d
}

func TestProcessedPreviewWritesOnlyAudit(t *testing.T) {
	for _, n := range []int64{2, 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			end := "commit"
			if n != 2 {
				end = "rollback"
			}
			r, d := processedDB(t, []visibilityStep{
				{kind: "begin"},
				{kind: "exec", query: `INSERT INTO processed_review_runs (id, status, item_count, after_processed, started_by) VALUES ($1, 'preview', $2, $3, $4)`, affected: 1},
				{kind: "exec", query: `INSERT INTO processed_review_items (run_id, stream_id, stream_title, before_processed, after_processed, before_updated_at)
 SELECT $1, s.id, s.title, s.is_processed, $2, s.updated_at FROM streams s WHERE s.is_processed <> $2 AND s.id = ANY($3)`, affected: n},
				{kind: end},
			})
			id, err := r.Preview([]string{"one", "two"}, true, &visibilityID)
			if n == 2 {
				if err != nil || id == uuid.Nil {
					t.Fatal(id, err)
				}
			} else if !errors.Is(err, ErrProcessedConflict) {
				t.Fatal("一部だけの候補を受け付けた", err)
			}
			// 可変のrun ID以外の引数も固定する。
			generated := d.calls[1][0]
			if _, err := uuid.Parse(generated.(string)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.calls[1][1:], []driver.Value{int64(2), true, visibilityID.String()}) {
				t.Fatal("runの対象数/操作者", d.calls[1])
			}
			if !reflect.DeepEqual(d.calls[2], []driver.Value{generated, true, `{"one","two"}`}) {
				t.Fatal("退避対象の引数", d.calls[2])
			}
		})
	}
}

const processedWantLock = `SELECT status, item_count FROM processed_review_runs WHERE id = $1 FOR UPDATE`
const processedWantApplyLock = `SELECT s.id FROM streams s JOIN processed_review_items i ON i.stream_id = s.id
 WHERE i.run_id = $1 AND s.is_processed = i.before_processed
 ORDER BY s.id ASC FOR UPDATE OF s`
const processedWantApply = `WITH changed AS (
 UPDATE streams s SET is_processed = i.after_processed, updated_at = NOW()
 FROM processed_review_items i WHERE i.run_id = $1 AND i.stream_id = s.id
 AND s.is_processed = i.before_processed
 RETURNING s.id, s.updated_at)
 UPDATE processed_review_items i SET after_updated_at = c.updated_at
 FROM changed c WHERE i.run_id = $1 AND i.stream_id = c.id`
const processedWantRevert = `WITH restored AS (
 UPDATE streams s SET is_processed = i.before_processed, updated_at = NOW()
 FROM processed_review_items i WHERE i.run_id = $1 AND i.stream_id = s.id
 AND s.is_processed = i.after_processed
 AND i.reverted_at IS NULL
 RETURNING s.id)
 UPDATE processed_review_items i SET reverted_at = NOW()
 FROM restored s WHERE i.run_id = $1 AND i.stream_id = s.id`

func TestProcessedApplySQLAndAtomicConflict(t *testing.T) {
	for _, scenario := range []string{"success", "missing", "write-count", "replay"} {
		t.Run(scenario, func(t *testing.T) {
			status := "preview"
			if scenario == "replay" {
				status = "applied"
			}
			steps := []visibilityStep{{kind: "begin"}, {kind: "query", query: processedWantLock, args: visibilityIDArgs, rows: [][]driver.Value{{status, int64(2)}}}}
			if scenario != "replay" {
				rows := [][]driver.Value{{"one"}, {"two"}}
				if scenario == "missing" {
					rows = rows[:1]
				}
				steps = append(steps, visibilityStep{kind: "query", query: processedWantApplyLock, args: visibilityIDArgs, rows: rows})
				if scenario != "missing" {
					n := int64(2)
					if scenario == "write-count" {
						n = 1
					}
					steps = append(steps, visibilityStep{kind: "exec", query: processedWantApply, args: visibilityIDArgs, affected: n})
				}
			}
			if scenario == "success" {
				steps = append(steps, visibilityStep{kind: "exec", query: `UPDATE processed_review_runs SET status = 'applied', applied_at = NOW() WHERE id = $1`, args: visibilityIDArgs, affected: 1}, visibilityStep{kind: "commit"})
			} else {
				steps = append(steps, visibilityStep{kind: "rollback"})
			}
			r, _ := processedDB(t, steps)
			n, err := r.Apply(visibilityID)
			if scenario == "success" {
				if err != nil || n != 2 {
					t.Fatal(n, err)
				}
			} else if !errors.Is(err, ErrProcessedConflict) {
				t.Fatal("競合時に一部の変更を確定した", err)
			}
		})
	}
}
func TestProcessedRevertSQLPreservesLaterChanges(t *testing.T) {
	for _, status := range []string{"applied", "preview", "reverted"} {
		t.Run(status, func(t *testing.T) {
			steps := []visibilityStep{{kind: "begin"}, {kind: "query", query: processedWantLock, args: visibilityIDArgs, rows: [][]driver.Value{{status, int64(2)}}}}
			if status == "applied" {
				steps = append(steps,
					visibilityStep{kind: "exec", query: processedWantRevert, args: visibilityIDArgs, affected: 1},
					visibilityStep{kind: "exec", query: `UPDATE processed_review_runs SET status = 'reverted', reverted_count = $2, reverted_at = NOW() WHERE id = $1`, args: []driver.Value{visibilityID.String(), int64(1)}, affected: 1},
					visibilityStep{kind: "commit"})
			} else {
				steps = append(steps, visibilityStep{kind: "rollback"})
			}
			r, _ := processedDB(t, steps)
			n, skipped, err := r.Revert(visibilityID)
			if status == "applied" {
				if err != nil || n != 1 || skipped != 1 {
					t.Fatal(n, skipped, err)
				}
			} else if !errors.Is(err, ErrProcessedConflict) {
				t.Fatal("再撤回/未実行を戻した", err)
			}
		})
	}
}
func TestProcessedRejectsBadSelectionBeforeDB(t *testing.T) {
	for _, ids := range [][]string{nil, {""}, {"same", "same"}, make([]string, 501)} {
		r, _ := processedDB(t, nil)
		if _, err := r.Preview(ids, false, nil); !errors.Is(err, ErrProcessedSelection) {
			t.Fatal("invalid selection accepted", err)
		}
	}
}

func TestProcessedRunsIssuedSQL(t *testing.T) {
	now := time.Unix(1, 0)
	for _, after := range []bool{false, true} {
		r, _ := processedDB(t, []visibilityStep{{kind: "query", query: `SELECT id, status, item_count, after_processed, reverted_count, created_at, applied_at, reverted_at FROM processed_review_runs ORDER BY created_at DESC LIMIT 20`, rows: [][]driver.Value{{visibilityID.String(), "applied", int64(2), after, int64(0), now, now, nil}}}})
		rows, err := r.Runs()
		if err != nil || len(rows) != 1 || rows[0].AfterProcessed != after || rows[0].Count != 2 || rows[0].AppliedAt == nil || rows[0].RevertedAt != nil {
			t.Fatal(rows, err)
		}
	}
}
