package service

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/repository"
)

// SETORI_TEST_DATABASE_URL で有効にする実 Postgres のテスト。
// 各接続に private な TEMP streams を作り、共有テーブルには書かない。
// MarkCommentsUnavailable の後に、実際の記録時刻・回数・対象外の行を読む。
func TestCommentsUnavailableTimestampPostgres(t *testing.T) {
	dsn := os.Getenv("SETORI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SETORI_TEST_DATABASE_URL to run the PostgreSQL timestamp test")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("SETORI_TEST_DATABASE_URL must be a postgres:// or postgresql:// URL")
	}
	params := u.Query()
	params.Set("default_transaction_read_only", "on")
	u.RawQuery = params.Encode()
	name := fmt.Sprintf("setori-availability-postgres-%d", availabilityPostgresSequence.Add(1))
	sql.Register(name, availabilityPostgresDriver{})
	db, err := sql.Open(name, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := repository.NewStreamRepository(db)
	var before, after time.Time
	if err := db.QueryRow("SELECT clock_timestamp()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.MarkCommentsUnavailable("abc"); err != nil || count != 1 {
		t.Fatalf("mark count=%d error=%v, want count 1 and no error", count, err)
	}
	if err := db.QueryRow("SELECT clock_timestamp()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	var at sql.NullTime
	var count int
	if err := db.QueryRow("SELECT comment_unavailable_at, comment_unavailable_count FROM streams WHERE id = $1", "abc").Scan(&at, &count); err != nil {
		t.Fatal(err)
	}
	if !at.Valid || at.Time.Before(before) || at.Time.After(after) || count != 1 {
		t.Fatalf("at=%v count=%d, want current database time in [%v, %v] and count 1", at, count, before, after)
	}
	if err := db.QueryRow("SELECT comment_unavailable_at, comment_unavailable_count FROM streams WHERE id = $1", "untouched").Scan(&at, &count); err != nil {
		t.Fatal(err)
	}
	if at.Valid || count != 0 {
		t.Fatalf("対象外の行が変わった: at=%v count=%d", at, count)
	}
	if err := repo.ClearCommentsUnavailable("abc"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT comment_unavailable_at, comment_unavailable_count FROM streams WHERE id = $1", "abc").Scan(&at, &count); err != nil {
		t.Fatal(err)
	}
	if at.Valid || count != 0 {
		t.Fatalf("解除後 at=%v count=%d, want NULL and 0", at, count)
	}
}

var availabilityPostgresSequence atomic.Uint64

type availabilityPostgresDriver struct{}

func (availabilityPostgresDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := (&pq.Driver{}).Open(dsn)
	if err != nil {
		return nil, err
	}
	// TEMP の作成・初期化だけ読み書き可能にし、検査の前に読み取り専用へ戻す。
	// 再接続時も必ず初期化するので、UPDATE streams が共有表へ向くことはない。
	for _, q := range []string{
		"SET default_transaction_read_only = off",
		"CREATE TEMP TABLE streams (id TEXT PRIMARY KEY, comment_unavailable_at TIMESTAMPTZ, comment_unavailable_count INTEGER NOT NULL DEFAULT 0)",
		"INSERT INTO streams (id) VALUES ('abc'), ('untouched')",
		"SET default_transaction_read_only = on",
	} {
		stmt, err := conn.Prepare(q)
		if err == nil {
			_, err = stmt.Exec(nil)
			stmt.Close()
		}
		if err != nil {
			conn.Close()
			return nil, err
		}
	}
	return &availabilityPostgresConn{Conn: conn}, nil
}

type availabilityPostgresConn struct{ driver.Conn }

func (c *availabilityPostgresConn) Prepare(q string) (driver.Stmt, error) {
	norm := strings.Join(strings.Fields(q), " ")
	if !strings.HasPrefix(norm, "SELECT ") && !strings.HasPrefix(norm, "UPDATE streams SET comment_unavailable_") {
		return nil, fmt.Errorf("unexpected statement in TEMP fixture: %s", q)
	}
	return c.Conn.Prepare(q)
}
