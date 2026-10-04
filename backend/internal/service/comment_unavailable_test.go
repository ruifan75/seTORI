package service

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

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
	repository.NewStreamRepository(db).FindStreamsNeedingCommentRefresh(nil, 30, nil, false)
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
	mu         sync.Mutex
	queries    []string
	storedRaw  []byte
	executions []availExecution
}

func (d *availDriver) Open(string) (driver.Conn, error) { return &availConn{d: d}, nil }
func (d *availDriver) all() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.queries...)
}

type availExecution struct {
	query string
	args  []driver.Value
}

func (d *availDriver) recordExecution(query string, args []driver.Value) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.executions = append(d.executions, availExecution{query: query, args: append([]driver.Value(nil), args...)})
}

// Prepare の SQL だけでなく、実行時に結び付けた対象 ID も見る。
func (d *availDriver) assertAvailabilityBindings(t *testing.T, wantCount int) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	count := 0
	for _, call := range d.executions {
		if !strings.Contains(call.query, "comment_unavailable_") {
			continue
		}
		count++
		if !reflect.DeepEqual(call.args, []driver.Value{"abc"}) {
			t.Errorf("取得不能記録の対象が違う: args=%v want=[abc]", call.args)
		}
	}
	if count != wantCount {
		t.Errorf("取得不能記録の実行=%d want=%d", count, wantCount)
	}
}

type availConn struct{ d *availDriver }

func (c *availConn) Prepare(q string) (driver.Stmt, error) {
	c.d.mu.Lock()
	c.d.queries = append(c.d.queries, q)
	c.d.mu.Unlock()
	return &availStmt{q: q, d: c.d}, nil
}
func (c *availConn) Close() error              { return nil }
func (c *availConn) Begin() (driver.Tx, error) { return nil, io.ErrUnexpectedEOF }

type availStmt struct {
	q string
	d *availDriver
}

func (s *availStmt) Close() error  { return nil }
func (s *availStmt) NumInput() int { return -1 }
func (s *availStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.d.recordExecution(s.q, args)
	return driver.RowsAffected(1), nil
}

// 連続回数と、保存経路が先に読む配信を返す。空の comment_raw なら遠隔取得へ進む。
func (s *availStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.d.recordExecution(s.q, args)
	if strings.Contains(s.q, "RETURNING comment_unavailable_count") {
		return &availRows{values: []driver.Value{int64(1)}}, nil
	}
	if strings.Contains(s.q, "FROM streams WHERE id = $1") {
		now := time.Now()
		var raw driver.Value
		if len(s.d.storedRaw) > 0 {
			raw = s.d.storedRaw
		}
		return &availRows{values: []driver.Value{
			"abc", "t", now, nil, nil, nil, nil, raw, nil, nil, nil, nil,
			false, false, nil, nil, false, nil, nil, nil, now, now, false,
			false, // restriction_needs_review（#70）
		}}, nil
	}
	return &availRows{done: true}, nil
}

type availRows struct {
	values []driver.Value
	done   bool
}

func (r *availRows) Columns() []string {
	out := make([]string, len(r.values))
	for i := range out {
		out[i] = fmt.Sprintf("c%d", i)
	}
	return out
}
func (r *availRows) Close() error { return nil }
func (r *availRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.values)
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

// 記録時刻・連続回数・対象 ID を含む UPDATE 全体を、実装を呼ばずに固定する。
// 時刻更新だけを消すと backoff が効かないが、以前の部分一致検査では通っていた。
const markCommentUnavailableSQL = "UPDATE streams SET comment_unavailable_at = NOW(), comment_unavailable_count = comment_unavailable_count + 1 WHERE id = $1 RETURNING comment_unavailable_count"
const clearCommentUnavailableSQL = "UPDATE streams SET comment_unavailable_at = NULL, comment_unavailable_count = 0 WHERE id = $1 AND (comment_unavailable_at IS NOT NULL OR comment_unavailable_count <> 0)"

func TestCommentAvailabilityWritesExact(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		call       func(*repository.StreamRepository) error
	}{
		{"mark-with-current-time", markCommentUnavailableSQL, func(r *repository.StreamRepository) error {
			count, err := r.MarkCommentsUnavailable("abc")
			if err == nil && count != 1 {
				return fmt.Errorf("count=%d, want 1", count)
			}
			return err
		}},
		{"clear-time-and-count", clearCommentUnavailableSQL, func(r *repository.StreamRepository) error {
			return r.ClearCommentsUnavailable("abc")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, rec := newAvailDB(t)
			if err := tc.call(repository.NewStreamRepository(db)); err != nil {
				t.Fatal(err)
			}
			rec.assertAvailabilityBindings(t, 1)
			issued := rec.all()
			if len(issued) != 1 {
				t.Fatalf("queries=%d, want 1", len(issued))
			}
			if got := strings.Join(strings.Fields(issued[0]), " "); got != tc.want {
				t.Errorf("取得不能の記録 SQL が変わっている\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// 新たに遠隔取得する保存経路は 5 つ。取得結果から復旧が分かったときは
// 全経路で解除し、分からない空結果では解除しない。dry-run は記録を書かない。
func TestCommentRecoveryAllFetchPaths(t *testing.T) {
	const ytComment = `{"items":[{"snippet":{"topLevelComment":{"snippet":{"textOriginal":"0:10 曲"}}}}]}`
	const hdComment = `{"id":"abc","comments":[{"message":"0:10 曲"}]}`
	for _, path := range []string{"refresh", "youtube-sync", "holodex-sync", "analysis-fetch", "raw-comments", "dry-run"} {
		for _, tc := range []struct {
			name                                  string
			ytStatus                              int
			ytBody                                string
			hdStatus                              int
			hdBody                                string
			recovered, youtubeOK, markUnavailable bool
		}{
			{"youtube-normal-empty", 200, `{"items":[]}`, 200, `{"id":"abc","comments":[]}`, true, true, false},
			{"youtube-has-comments", 200, ytComment, 200, `{"id":"abc","comments":[]}`, true, true, false},
			{"unavailable-but-holodex-has-comments", 404, `{}`, 200, hdComment, true, false, false},
			{"temporary-youtube-error-empty-fallback", 500, `{}`, 200, `{"id":"abc","comments":[]}`, false, false, false},
			{"temporary-youtube-error-nonempty-fallback", 500, `{}`, 200, hdComment, true, false, false},
			{"quota-error-empty-fallback", 403, `{"error":{"errors":[{"reason":"quotaExceeded"}]}}`, 200, `{"id":"abc","comments":[]}`, false, false, false},
			{"unavailable-empty-fallback", 404, `{}`, 200, `{"id":"abc","comments":[]}`, false, false, true},
			{"normal-youtube-empty-holodex-error", 200, `{"items":[]}`, 500, `{}`, true, true, false},
			{"unavailable-holodex-error", 404, `{}`, 500, `{}`, false, false, false},
			{"youtube-unconfigured-empty-fallback", 0, ``, 200, `{"id":"abc","comments":[]}`, false, false, false},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				calls := 0
				key := "test-key"
				if tc.ytStatus == 0 {
					key = ""
				}
				yt := youtube.NewClient(key)
				yt.SetTransport(stubTransport(func(*http.Request) (int, string) { calls++; return tc.ytStatus, tc.ytBody }))
				hd := holodex.NewClient("test-key")
				hd.SetTransport(stubTransport(func(*http.Request) (int, string) { calls++; return tc.hdStatus, tc.hdBody }))
				db, rec := newAvailDB(t)
				repo := repository.NewStreamRepository(db)
				hs := &HolodexService{client: hd, youtubeClient: yt, streamRepo: repo}
				cs := &CommentService{holodexService: hs, streamRepo: repo}
				switch path {
				case "refresh":
					cs.RefreshCommentRaw("abc")
				case "youtube-sync":
					cs.SyncYouTubeCommentRaw("abc")
				case "holodex-sync":
					hs.loadAndSaveComments("abc")
				case "analysis-fetch":
					cs.getComments("abc", nil, false, false)
				case "raw-comments":
					cs.GetRawComments("abc")
				case "dry-run":
					cs.getComments("abc", nil, true, false)
				}
				if calls == 0 && !(path == "youtube-sync" && tc.ytStatus == 0) {
					t.Fatal("遠隔取得に到達していない")
				}
				wantClear := tc.recovered && path != "dry-run"
				if path == "youtube-sync" {
					wantClear = tc.youtubeOK
				}
				wantMark := tc.markUnavailable && path == "refresh"
				var availabilitySQL []string
				for _, q := range rec.all() {
					if strings.Contains(q, "comment_unavailable_") {
						availabilitySQL = append(availabilitySQL, strings.Join(strings.Fields(q), " "))
					}
				}
				var want []string
				if wantClear {
					want = []string{clearCommentUnavailableSQL}
				}
				if wantMark {
					want = []string{markCommentUnavailableSQL}
				}
				if len(availabilitySQL) != len(want) || (len(want) > 0 && availabilitySQL[0] != want[0]) {
					t.Errorf("availability SQL=%q, want %q", availabilitySQL, want)
				}
				rec.assertAvailabilityBindings(t, len(want))
				if path == "dry-run" && len(rec.all()) != 0 {
					t.Errorf("dry-run が DB を更新している: %q", rec.all())
				}
			})
		}
	}
}

// 保存済みコメントを読むだけでは、遠隔側の復旧を確認したことにならない。
func TestCachedCommentsLeaveAvailabilityAlone(t *testing.T) {
	db, rec := newAvailDB(t)
	rec.storedRaw = []byte(`["0:10 保存済み"]`)
	svc := &CommentService{streamRepo: repository.NewStreamRepository(db)}
	got, err := svc.GetRawComments("abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "0:10 保存済み" {
		t.Fatalf("comments=%q", got)
	}
	for _, q := range rec.all() {
		if strings.Contains(q, "UPDATE") {
			t.Errorf("キャッシュの読み取りで更新している: %s", q)
		}
	}
}

// info.json は過去に取得した入力で、現在の遠隔側の復旧の証拠にはならない。
// SaveCommentRaw 全体で解除するように移すと、この経路まで解除されてしまう。
func TestInfoJSONImportLeavesAvailabilityAlone(t *testing.T) {
	db, rec := newAvailDB(t)
	cs := &CommentService{streamRepo: repository.NewStreamRepository(db)}
	out, err := cs.ImportInfoJSON("abc", []byte(`{"id":"abc","comments":[{"text":"0:10 曲","parent":"root"}]}`))
	if err != nil || out.Saved != 1 {
		t.Fatalf("import=%+v err=%v", out, err)
	}
	issued := rec.all()
	if len(issued) != 1 || !strings.Contains(issued[0], "comment_raw = $2") {
		t.Fatalf("コメント保存の陽性対照: %q", issued)
	}
	rec.assertAvailabilityBindings(t, 0)
}
