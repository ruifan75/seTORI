package repository

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lib/pq"
)

// 異なる二つの bool を実際の FindByID.Scan に通す。SELECT の文字列だけでは
// IsRestrictedEffective と RestrictionNeedsReview の読み先の交換を検出できない。
func TestFindByIDRestrictionValues(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		rowOverride                driver.Value
		rowRestricted, rowReview   bool
		wantRestricted, wantReview bool
	}{
		{"restricted-without-review", nil, true, false, true, false},
		{"public-needing-review", false, false, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openRestrictionTestDB(t, streamRestrictionValueDriver{
				override: tc.rowOverride, restricted: tc.rowRestricted, review: tc.rowReview,
			}, "")
			stream, err := NewStreamRepository(db).FindByID("abc")
			if err != nil {
				t.Fatal(err)
			}
			if stream == nil {
				t.Fatal("expected stream")
			}
			if stream.IsRestrictedEffective != tc.wantRestricted || stream.RestrictionNeedsReview != tc.wantReview {
				t.Fatalf("restricted=%v review=%v, want restricted=%v review=%v",
					stream.IsRestrictedEffective, stream.RestrictionNeedsReview, tc.wantRestricted, tc.wantReview)
			}
		})
	}
}

// SETORI_TEST_DATABASE_URL を指定して実行する実 Postgres の回帰テスト。
// 読み取り専用接続で CTE の合成データを FindByID の実際の SELECT に渡す。
// 実テーブル・migration に依存せず、式の三値論理と Scan の両方を検査する。
func TestFindByIDRestrictionReviewPostgres(t *testing.T) {
	dsn := os.Getenv("SETORI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SETORI_TEST_DATABASE_URL to run the PostgreSQL regression test")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("SETORI_TEST_DATABASE_URL must be a postgres:// or postgresql:// URL")
	}
	params := u.Query()
	params.Set("default_transaction_read_only", "on")
	u.RawQuery = params.Encode()

	unset := sql.NullBool{}
	no := sql.NullBool{Bool: false, Valid: true}
	yes := sql.NullBool{Bool: true, Valid: true}
	for _, tc := range []struct {
		name                       string
		override, basis            sql.NullBool
		detected, ownerAllows      bool
		wantRestricted, wantReview bool
	}{
		{"unadjudicated-auto-restricted-null-basis", unset, unset, true, false, true, false},
		{"unadjudicated-public-null-basis", unset, unset, false, false, false, false},
		{"public-before-detection", no, no, true, false, false, true},
		{"legacy-public-null-basis", no, unset, true, false, false, true},
		{"confirmed-public-exception", no, yes, true, false, false, false},
		{"restricted-override", yes, unset, true, false, true, false},
		{"no-detection", no, no, false, false, false, false},
		{"owner-allows", no, no, true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctes := fmt.Sprintf(`WITH streams AS (
				SELECT 'abc'::text AS id, 't'::text AS title, NOW() AS stream_date,
				NULL::int AS duration_seconds, NULL::text AS thumbnail_url,
				NULL::jsonb AS holodex_data, NULL::text AS holodex_hash,
				NULL::jsonb AS comment_raw, NULL::jsonb AS comment_songs,
				NULL::timestamptz AS comment_songs_analyzed_at,
				NULL::jsonb AS chapter_raw, NULL::jsonb AS chapter_songs,
				FALSE AS is_processed, FALSE AS is_hidden,
				%s AS restriction_override, %s AS restriction_override_auto,
				NULL::timestamptz AS holodex_uploaded_at, FALSE AS holodex_upload_unknown,
				NULL::text AS availability, NULL::boolean AS playable_in_embed,
				NULL::timestamptz AS availability_checked_at,
				NOW() AS created_at, NOW() AS updated_at
			), stream_stream_tags AS (
				SELECT 'abc'::text AS stream_id, 'members_only'::text AS tag_id WHERE %t
			), stream_singers AS (
				SELECT 'abc'::text AS stream_id, 'owner'::text AS singer_id, TRUE AS is_owner
			), singers AS (
				SELECT 'owner'::text AS id, CASE WHEN %t THEN 'allow' ELSE NULL::text END AS members_only_policy
			) `, restrictionFixtureBool(tc.override), restrictionFixtureBool(tc.basis), tc.detected, tc.ownerAllows)
			db := openRestrictionTestDB(t, restrictionPostgresDriver{ctes: ctes}, u.String())
			stream, err := NewStreamRepository(db).FindByID("abc")
			if err != nil {
				t.Fatal(err)
			}
			if stream == nil {
				t.Fatal("expected stream")
			}
			if stream.IsRestrictedEffective != tc.wantRestricted || stream.RestrictionNeedsReview != tc.wantReview {
				t.Fatalf("restricted=%v review=%v, want restricted=%v review=%v",
					stream.IsRestrictedEffective, stream.RestrictionNeedsReview, tc.wantRestricted, tc.wantReview)
			}
		})
	}
}

func restrictionFixtureBool(v sql.NullBool) string {
	if !v.Valid {
		return "NULL::boolean"
	}
	if v.Bool {
		return "TRUE"
	}
	return "FALSE"
}

var restrictionTestSequence atomic.Uint64

func openRestrictionTestDB(t *testing.T, d driver.Driver, dsn string) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("setori-restriction-values-%d", restrictionTestSequence.Add(1))
	sql.Register(name, d)
	db, err := sql.Open(name, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

type restrictionPostgresDriver struct{ ctes string }

func (d restrictionPostgresDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := (&pq.Driver{}).Open(dsn)
	if err != nil {
		return nil, err
	}
	return &restrictionPostgresConn{Conn: conn, ctes: d.ctes}, nil
}

type restrictionPostgresConn struct {
	driver.Conn
	ctes string
}

func (c *restrictionPostgresConn) Prepare(q string) (driver.Stmt, error) {
	if !strings.Contains(q, "FROM streams WHERE id = $1") {
		return nil, fmt.Errorf("unexpected query in read-only fixture: %s", q)
	}
	return c.Conn.Prepare(c.ctes + q)
}

type streamRestrictionValueDriver struct {
	override           driver.Value
	restricted, review bool
}

func (d streamRestrictionValueDriver) Open(string) (driver.Conn, error) {
	return &streamRestrictionValueConn{d: d}, nil
}

type streamRestrictionValueConn struct{ d streamRestrictionValueDriver }

func (c *streamRestrictionValueConn) Prepare(q string) (driver.Stmt, error) {
	if !strings.Contains(q, "FROM streams WHERE id = $1") {
		return nil, fmt.Errorf("unexpected query: %s", q)
	}
	return &streamRestrictionValueStmt{d: c.d}, nil
}
func (*streamRestrictionValueConn) Close() error              { return nil }
func (*streamRestrictionValueConn) Begin() (driver.Tx, error) { return nil, io.ErrUnexpectedEOF }

type streamRestrictionValueStmt struct{ d streamRestrictionValueDriver }

func (*streamRestrictionValueStmt) Close() error  { return nil }
func (*streamRestrictionValueStmt) NumInput() int { return -1 }
func (*streamRestrictionValueStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, io.ErrUnexpectedEOF
}
func (s *streamRestrictionValueStmt) Query([]driver.Value) (driver.Rows, error) {
	now := time.Now()
	return &streamRestrictionValueRows{values: []driver.Value{
		"abc", "t", now, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		false, false, s.d.override, nil, false, nil, nil, nil, now, now, s.d.restricted, s.d.review,
	}}, nil
}

type streamRestrictionValueRows struct {
	values []driver.Value
	done   bool
}

func (r *streamRestrictionValueRows) Columns() []string {
	out := make([]string, len(r.values))
	for i := range out {
		out[i] = fmt.Sprintf("c%d", i)
	}
	return out
}
func (*streamRestrictionValueRows) Close() error { return nil }
func (r *streamRestrictionValueRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.values)
	return nil
}
