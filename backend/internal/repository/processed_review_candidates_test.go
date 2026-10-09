package repository

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 実 repository の発行 SQL 全体と引数を固定する。条件の実評価は Postgres テストで行う。
func TestProcessedCandidatesIssuedSQL(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	until := from.AddDate(0, 1, 0)
	for _, after := range []bool{false, true} {
		for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
			for _, has := range []bool{false, true} {
				t.Run(fmt.Sprintf("after=%t/access=%d/has=%t", after, access, has), func(t *testing.T) {
					hidden := true
					f := ProcessedReviewFilters{AfterProcessed: after, Query: "歌", ChannelID: "guest", TagIDs: []string{"singing", "members_only", "singing"}, Hidden: &hidden, From: &from, Until: &until, HasPerformances: &has}
					restriction := "NOT " + strings.ReplaceAll(issue4Restricted, "st.", "s.")
					if access == RestrictedView {
						restriction = "TRUE"
					}
					existence := "(EXISTS (SELECT 1 FROM performances p WHERE p.stream_id = s.id) AND " + restriction + ")"
					if !has {
						existence = "NOT " + existence
					}
					where := `s.is_processed <> $1 AND s.title ILIKE '%' || $2 || '%' ESCAPE '!'
 AND EXISTS (SELECT 1 FROM stream_channels sc WHERE sc.stream_id = s.id AND sc.channel_id = $3)
 AND s.id IN (SELECT st.stream_id FROM stream_stream_tags st WHERE st.tag_id = ANY($4) GROUP BY st.stream_id HAVING COUNT(DISTINCT st.tag_id) = 2)
 AND s.is_hidden = $5 AND s.stream_date >= $6 AND s.stream_date < $7 AND ` + existence
					args := []driver.Value{after, "歌", "guest", `{"singing","members_only"}`, true, from, until}
					rr, _ := processedDB(t, []visibilityStep{
						{kind: "query", query: "SELECT COUNT(*) FROM streams s WHERE " + where, args: args, rows: [][]driver.Value{{int64(1)}}},
						{kind: "query", query: `SELECT s.id, s.title, s.stream_date, s.is_hidden, s.is_processed FROM streams s WHERE ` + where + ` ORDER BY s.stream_date DESC, s.id ASC LIMIT $8 OFFSET $9`, args: append(append([]driver.Value{}, args...), int64(100), int64(200)), rows: [][]driver.Value{{"v", "歌枠", from, true, !after}}},
					})
					rows, count, err := rr.Candidates(f, 100, 200, access)
					if err != nil || count != 1 || len(rows) != 1 || rows[0].IsProcessed == after || rows[0].Title != "歌枠" {
						t.Fatalf("rows=%v count=%d err=%v", rows, count, err)
					}
				})
			}
		}
	}
	// 条件無しでも非表示を除外せず、反対の処理済み状態だけを候補にする。
	r, _ := processedDB(t, []visibilityStep{
		{kind: "query", query: "SELECT COUNT(*) FROM streams s WHERE s.is_processed <> $1", args: []driver.Value{true}, rows: [][]driver.Value{{int64(0)}}},
		{kind: "query", query: `SELECT s.id, s.title, s.stream_date, s.is_hidden, s.is_processed FROM streams s WHERE s.is_processed <> $1 ORDER BY s.stream_date DESC, s.id ASC LIMIT $2 OFFSET $3`, args: []driver.Value{true, int64(100), int64(0)}},
	})
	rows, count, err := r.Candidates(ProcessedReviewFilters{AfterProcessed: true}, 100, 0, PublicAccess)
	if err != nil || count != 0 || !reflect.DeepEqual(rows, []ProcessedCandidate{}) {
		t.Fatal(rows, count, err)
	}
}

func TestProcessedCandidateErrorsAreNotEmptySuccess(t *testing.T) {
	for _, failing := range []string{"count", "list"} {
		expected := errors.New("database unavailable")
		steps := []perfAssociationStep{{query: "SELECT COUNT(*) FROM streams s WHERE s.is_processed <> $1", args: []driver.Value{true}, rows: [][]driver.Value{{int64(1)}}}}
		if failing == "count" {
			steps[0].err = expected
		} else {
			steps = append(steps, perfAssociationStep{query: "SELECT s.id, s.title, s.stream_date, s.is_hidden, s.is_processed FROM streams s WHERE s.is_processed <> $1 ORDER BY s.stream_date DESC, s.id ASC LIMIT $2 OFFSET $3", args: []driver.Value{true, int64(100), int64(0)}, err: expected})
		}
		r := NewProcessedReviewRepository(perfAssociationDB(t, steps))
		if _, _, err := r.Candidates(ProcessedReviewFilters{AfterProcessed: true}, 100, 0, PublicAccess); !errors.Is(err, expected) {
			t.Fatal("DB failure became empty success", err)
		}
	}
}

func TestProcessedTitleSearchLiteralIssuedSQL(t *testing.T) {
	// 期待する escape 済み引数は実装の変換関数から作らない。
	for _, tc := range []struct{ query, escaped string }{
		{"50%", "50!%"}, {"a_b", "a!_b"}, {"wow!", "wow!!"},
		{`C:\music`, `C:\music`}, {`歌!50%_\`, `歌!!50!%!_\`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			where := `s.is_processed <> $1 AND s.title ILIKE '%' || $2 || '%' ESCAPE '!'`
			r, _ := processedDB(t, []visibilityStep{
				{kind: "query", query: "SELECT COUNT(*) FROM streams s WHERE " + where, args: []driver.Value{true, tc.escaped}, rows: [][]driver.Value{{int64(0)}}},
				{kind: "query", query: `SELECT s.id, s.title, s.stream_date, s.is_hidden, s.is_processed FROM streams s WHERE ` + where + ` ORDER BY s.stream_date DESC, s.id ASC LIMIT $3 OFFSET $4`, args: []driver.Value{true, tc.escaped, int64(100), int64(0)}},
			})
			if rows, n, err := r.Candidates(ProcessedReviewFilters{AfterProcessed: true, Query: tc.query}, 100, 0, PublicAccess); err != nil || n != 0 || len(rows) != 0 {
				t.Fatal(rows, n, err)
			}
		})
	}
}
