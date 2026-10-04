package service

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

// 裁定と食い違いの旗（issue #26）は**編集者だけ**に載せる。
//
// 実際の変換（toStreamResponse）を通す。DTO の struct タグを見るだけでは、
// 分岐を消しても通ってしまう。
func TestToStreamResponseRestrictionFields(t *testing.T) {
	svc := &StreamService{}
	stream := models.Stream{
		ID: "abc", Title: "t", StreamDate: time.Now(),
		RestrictionOverride:    sql.NullBool{Bool: false, Valid: true},
		RestrictionNeedsReview: true,
	}
	decode := func(t *testing.T, s models.Stream, view streamView) map[string]json.RawMessage {
		t.Helper()
		b, err := json.Marshal(svc.toStreamResponse(s, nil, nil, nil, view))
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]json.RawMessage{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	t.Run("編集者には裁定と旗を載せる", func(t *testing.T) {
		m := decode(t, stream, editorView(true))
		// false の裁定が**消えずに** false として載ること（「未裁定」と区別するため）。
		if string(m["restriction_override"]) != "false" {
			t.Errorf("restriction_override = %s, want false", m["restriction_override"])
		}
		if string(m["restriction_needs_review"]) != "true" {
			t.Errorf("restriction_needs_review = %s, want true", m["restriction_needs_review"])
		}
	})

	t.Run("閲覧者には載せない", func(t *testing.T) {
		m := decode(t, stream, streamView{})
		for _, k := range []string{"restriction_override", "restriction_needs_review"} {
			if _, ok := m[k]; ok {
				t.Errorf("%s が閲覧向けの応答に載っている", k)
			}
		}
		// 陽性対照：実効値は閲覧者にも返す（画面で「未公開」と言うために要る）。
		if _, ok := m["is_restricted"]; !ok {
			t.Error("is_restricted まで消えている（落としすぎ）")
		}
	})

	t.Run("未裁定は載せない", func(t *testing.T) {
		s := stream
		s.RestrictionOverride = sql.NullBool{}
		if _, ok := decode(t, s, editorView(true))["restriction_override"]; ok {
			t.Error("未裁定なのに restriction_override が載っている（false と区別できない）")
		}
	})
}

// **裁定はタグと参加者のあとに書く。** 同じ要求で members_only を外して
// 「公開してよい」にしたとき、控えるべきは外したあとの判定。先に書くと
// 外す前（＝伏せる）を控えるので、同期がタグを付け直しても警告が出ない。
//
// 実際に Update を呼び、発行された文の順序を見る。
func TestUpdateWritesRestrictionAfterTagsAndParticipants(t *testing.T) {
	no := false
	db, rec := newOrderDB(t)
	svc := NewStreamService(repository.NewStreamRepository(db), repository.NewPerformanceRepository(db))
	svc.Update("abc", &dto.UpdateStreamRequest{
		TagIDs:         []string{"singing"},
		ParticipantIDs: []string{"UC1"},
		IsRestricted:   &no,
	}, true, repository.RestrictedView)

	issued := rec.all()
	pos := func(marker string) int {
		for i, q := range issued {
			if strings.Contains(q, marker) {
				return i
			}
		}
		return -1
	}
	tags := pos("DELETE FROM stream_stream_tags")
	singers := pos("DELETE FROM stream_singers")
	// 詳細の SELECT も `restriction_override_auto` を含む（警告の判定式）ので、
	// UPDATE にしか現れない形で探す。
	override := pos("SET restriction_override")
	if tags < 0 || singers < 0 || override < 0 {
		t.Fatalf("必要な文が発行されていない（tags=%d singers=%d override=%d）: %q", tags, singers, override, issued)
	}
	if override < tags || override < singers {
		t.Errorf("裁定がタグ・参加者より先に書かれている（tags=%d singers=%d override=%d）", tags, singers, override)
	}
}

// **裁定を含まない更新では裁定を書かない。** 題名の編集のたびに控えを取り直すと、
// 食い違いの警告が理由なく消える。
func TestUpdateWithoutRestrictionLeavesOverrideAlone(t *testing.T) {
	title := "新しい題名"
	db, rec := newOrderDB(t)
	svc := NewStreamService(repository.NewStreamRepository(db), repository.NewPerformanceRepository(db))
	svc.Update("abc", &dto.UpdateStreamRequest{Title: &title, TagIDs: []string{"singing"}}, true, repository.RestrictedView)

	issued := rec.all()
	sawUpdate := false
	for _, q := range issued {
		if strings.Contains(q, "SET restriction_override") {
			t.Errorf("裁定を含まない更新で裁定を書いている: %s", q)
		}
		if strings.Contains(q, "UPDATE streams") {
			sawUpdate = true
		}
	}
	// 陽性対照：更新そのものは走っていること。
	if !sawUpdate {
		t.Fatalf("UPDATE が 1 本も発行されていない: %q", issued)
	}
}

// ---- 順序を見るための偽 driver ----
//
// 配信 1 件を返し（Update は最初に FindByID で存在を確かめる）、トランザクションと
// UPDATE … RETURNING を受け付ける。**FindByID の列数が変わったらここも変える**
// （ずれると Scan で落ち、Update は最初の文で戻るので「必要な文が発行されていない」で落ちる）。

type orderDriver struct {
	mu      sync.Mutex
	queries []string
}

func (d *orderDriver) Open(string) (driver.Conn, error) { return &orderConn{d: d}, nil }
func (d *orderDriver) all() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.queries...)
}

type orderConn struct{ d *orderDriver }

func (c *orderConn) Prepare(q string) (driver.Stmt, error) {
	c.d.mu.Lock()
	c.d.queries = append(c.d.queries, q)
	c.d.mu.Unlock()
	return &orderStmt{q: q}, nil
}
func (c *orderConn) Close() error              { return nil }
func (c *orderConn) Begin() (driver.Tx, error) { return orderTx{}, nil }

type orderTx struct{}

func (orderTx) Commit() error   { return nil }
func (orderTx) Rollback() error { return nil }

type orderStmt struct{ q string }

func (s *orderStmt) Close() error  { return nil }
func (s *orderStmt) NumInput() int { return -1 }
func (s *orderStmt) Exec([]driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (s *orderStmt) Query([]driver.Value) (driver.Rows, error) {
	now := time.Now()
	switch {
	case strings.Contains(s.q, "FROM streams WHERE id = $1"):
		// FindByID の 24 列
		return &orderRows{cols: 24, row: []driver.Value{
			"abc", "t", now, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			false, false, nil, nil, false, nil, nil, nil, now, now, false, false,
		}}, nil
	case strings.Contains(s.q, "RETURNING"):
		return &orderRows{cols: 1, row: []driver.Value{now}}, nil
	}
	return &orderRows{}, nil
}

type orderRows struct {
	cols int
	row  []driver.Value
	done bool
}

func (r *orderRows) Columns() []string {
	out := make([]string, r.cols)
	for i := range out {
		out[i] = fmt.Sprintf("c%d", i)
	}
	return out
}
func (r *orderRows) Close() error { return nil }
func (r *orderRows) Next(dest []driver.Value) error {
	if r.done || r.row == nil {
		return io.EOF
	}
	r.done = true
	copy(dest, r.row)
	return nil
}

var orderSeq int

func newOrderDB(t *testing.T) (*sql.DB, *orderDriver) {
	t.Helper()
	d := &orderDriver{}
	orderSeq++
	name := fmt.Sprintf("setori-order-%d", orderSeq)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, d
}
