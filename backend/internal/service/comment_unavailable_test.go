package service

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/holodex"
	"github.com/ruifan75/setori/pkg/youtube"
)

// **「YouTube がコメントは取れないと明言した」が取り直しの記録まで届くこと**（issue #56）。
//
// 実際に RefreshCommentRaw を呼び、YouTube と Holodex の HTTP を差し替え、
// 発行された SQL を見る。判定関数（youtube.commentsUnavailable）だけを検査しても、
// 途中の層（Holodex への退避）が結果を捨てていれば気付けない ── 以前は
// まさにそこで 404 が消え、「0 件」としか見えなかった。
func TestRefreshCommentRawRecordsAvailability(t *testing.T) {
	const (
		mark  = "comment_unavailable_count = comment_unavailable_count + 1"
		clear = "SET comment_unavailable_at = NULL"
	)
	cases := []struct {
		name     string
		ytStatus int
		ytBody   string
		hdStatus int
		hdBody   string
		want     string // mark / clear / ""（どちらも書かない）
	}{
		{"404 で Holodex も 0 件 → 記録", 404, `{"error":{"errors":[{"reason":"videoNotFound"}]}}`, 200, `{"id":"abc","comments":[]}`, mark},
		{"commentsDisabled で Holodex も 0 件 → 記録", 403, `{"error":{"errors":[{"reason":"commentsDisabled"}]}}`, 200, `{"id":"abc","comments":[]}`, mark},
		// どこかから取れたならコメント欄はある
		{"404 でも Holodex に有る → 解除", 404, `{}`, 200, `{"id":"abc","comments":[{"message":"0:10 曲"}]}`, clear},
		// YouTube が正常に応答した＝コメント欄はある（まだ誰も書いていないだけ）
		{"YouTube が正常に 0 件 → 解除", 200, `{"items":[]}`, 200, `{"id":"abc","comments":[]}`, clear},
		// **分からないことは記録しない。** 障害のあいだに触った配信を全部後回しにしない
		{"YouTube の 500 → 何もしない", 500, `{}`, 200, `{"id":"abc","comments":[]}`, ""},
		{"クォータ超過 → 何もしない", 403, `{"error":{"errors":[{"reason":"quotaExceeded"}]}}`, 200, `{"id":"abc","comments":[]}`, ""},
		// Holodex も落ちたなら取得自体が失敗（記録の前で戻る）
		{"404 で Holodex も失敗 → 何もしない", 404, `{}`, 500, `{}`, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yt := youtube.NewClient("test-key")
			yt.SetTransport(stubTransport(func(r *http.Request) (int, string) {
				if !strings.Contains(r.URL.Path, "/commentThreads") {
					t.Fatalf("想定外の YouTube 要求: %s", r.URL)
				}
				return tc.ytStatus, tc.ytBody
			}))
			hd := holodex.NewClient("test-key")
			hd.SetTransport(stubTransport(func(r *http.Request) (int, string) {
				return tc.hdStatus, tc.hdBody
			}))

			db, rec := newAvailDB(t)
			repo := repository.NewStreamRepository(db)
			svc := &CommentService{
				holodexService: &HolodexService{client: hd, youtubeClient: yt, streamRepo: repo},
				streamRepo:     repo,
			}
			svc.RefreshCommentRaw("abc")

			var marked, cleared bool
			for _, q := range rec.all() {
				marked = marked || strings.Contains(q, mark)
				cleared = cleared || strings.Contains(q, clear)
			}
			if marked != (tc.want == mark) || cleared != (tc.want == clear) {
				t.Errorf("記録=%v 解除=%v、want %q\nSQL: %q", marked, cleared, tc.want, rec.all())
			}
		})
	}
}

// **取り直しの対象から、間隔が空いていない配信を外す。** 条件は
// CommentRefreshBackoffExpr だけで、終端まで完全一致させる（後ろに `OR TRUE` を足す・
// `NOT` を外す改変を通さないため）。期待値は実装を呼ばずに書く。
func TestCommentRefreshBackoffExprExact(t *testing.T) {
	want := "COALESCE(s.comment_unavailable_at > NOW() - LEAST(power(2, GREATEST(s.comment_unavailable_count, 1) - 1), 7) * INTERVAL '1 day', FALSE)"
	if got := repository.CommentRefreshBackoffExpr("s"); got != want {
		t.Errorf("式が変わっている\n got: %s\nwant: %s", got, want)
	}
}

func TestFindStreamsNeedingCommentRefreshAppliesBackoff(t *testing.T) {
	db, rec := newAvailDB(t)
	repository.NewStreamRepository(db).FindStreamsNeedingCommentRefresh(nil, 30, nil)
	issued := rec.all()
	if len(issued) != 1 {
		t.Fatalf("1 本のはずが %d 本: %q", len(issued), issued)
	}
	norm := strings.Join(strings.Fields(issued[0]), " ")
	// 会限の除外の直後に、否定つきで AND されていること（ORDER BY の手前で終わる）。
	want := "AND NOT EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = s.id AND mt.tag_id = 'members_only')" +
		" AND NOT COALESCE(s.comment_unavailable_at > NOW() - LEAST(power(2, GREATEST(s.comment_unavailable_count, 1) - 1), 7) * INTERVAL '1 day', FALSE)" +
		" ORDER BY s.stream_date DESC"
	if !strings.HasSuffix(norm, want) {
		t.Errorf("取り直しの条件の末尾が期待と違う\n got: %s\nwant 末尾: %s", norm, want)
	}
}

// ---- 偽の HTTP と DB ----

type stubTransport func(*http.Request) (int, string)

func (f stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	status, body := f(r)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

type availDriver struct {
	mu      sync.Mutex
	queries []string
}

func (d *availDriver) Open(string) (driver.Conn, error) { return &availConn{d: d}, nil }
func (d *availDriver) all() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.queries...)
}

type availConn struct{ d *availDriver }

func (c *availConn) Prepare(q string) (driver.Stmt, error) {
	c.d.mu.Lock()
	c.d.queries = append(c.d.queries, q)
	c.d.mu.Unlock()
	return &availStmt{q: q}, nil
}
func (c *availConn) Close() error              { return nil }
func (c *availConn) Begin() (driver.Tx, error) { return nil, io.ErrUnexpectedEOF }

type availStmt struct{ q string }

func (s *availStmt) Close() error  { return nil }
func (s *availStmt) NumInput() int { return -1 }
func (s *availStmt) Exec([]driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}

// RETURNING の 1 列（連続回数）だけ返す。それ以外は空。
func (s *availStmt) Query([]driver.Value) (driver.Rows, error) {
	if strings.Contains(s.q, "RETURNING comment_unavailable_count") {
		return &availRows{}, nil
	}
	return &availRows{done: true}, nil
}

type availRows struct{ done bool }

func (r *availRows) Columns() []string { return []string{"c"} }
func (r *availRows) Close() error      { return nil }
func (r *availRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = int64(1)
	return nil
}

var availSeq int

func newAvailDB(t *testing.T) (*sql.DB, *availDriver) {
	t.Helper()
	d := &availDriver{}
	availSeq++
	name := fmt.Sprintf("setori-avail-%d", availSeq)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, d
}
