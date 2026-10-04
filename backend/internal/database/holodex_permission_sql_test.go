package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"testing"
)

// 実行器から発行された 068 全体を独立した期待値と比較する。
// DB 未設定でも WHERE の反転・列の差し替え・片方だけの更新を検出する。
// 行の変更結果は TestHolodexUploadPermissionMigration の実 Postgres 検査で扱う。
func TestHolodexUploadMigrationEmittedSQL(t *testing.T) {
	capture := &holodexMigrationCapture{}
	db := sql.OpenDB(capture)
	defer db.Close()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	want := `
		UPDATE roles
		SET permissions = array_remove(permissions, 'holodex:upload'), updated_at = NOW()
		WHERE NOT (is_system AND name = 'admin')
		  AND 'holodex:upload' = ANY(permissions);
		UPDATE roles
		SET permissions = array_append(permissions, 'holodex:upload'), updated_at = NOW()
		WHERE is_system AND name = 'admin'
		  AND ('*' = ANY(permissions) OR 'sync:run' = ANY(permissions))
		  AND NOT ('holodex:upload' = ANY(permissions));`
	if got := holodexMigrationSQL(capture.query); got != holodexMigrationSQL(want) {
		t.Fatalf("emitted migration SQL:\n%s\nwant:\n%s", got, holodexMigrationSQL(want))
	}
	if capture.applied != 1 || capture.recorded != 1 {
		t.Fatalf("applied = %d, recorded = %d; want 1 each", capture.applied, capture.recorded)
	}
}

// SQL コメントと空白だけを除く。述語・列・演算子・定数は丸ごと比較する。
func holodexMigrationSQL(query string) string {
	var lines []string
	for _, line := range strings.Split(query, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		lines = append(lines, line)
	}
	return strings.Join(strings.Fields(strings.Join(lines, "\n")), " ")
}

// OpenDB の専用 connector を使い、他のテストの sql.Register と衝突させない。
type holodexMigrationCapture struct {
	query             string
	applied, recorded int
}

func (c *holodexMigrationCapture) Connect(context.Context) (driver.Conn, error) { return c, nil }
func (c *holodexMigrationCapture) Driver() driver.Driver                        { return c }
func (c *holodexMigrationCapture) Open(string) (driver.Conn, error)             { return c, nil }
func (c *holodexMigrationCapture) Close() error                                 { return nil }
func (c *holodexMigrationCapture) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("unexpected Prepare")
}
func (c *holodexMigrationCapture) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("unexpected Begin")
}

const holodexUploadMigrationVersion = "068_add_holodex_upload_permission.sql"

func (c *holodexMigrationCapture) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if query != "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)" || len(args) != 1 {
		return nil, fmt.Errorf("unexpected migration lookup: %s, %v", query, args)
	}
	return &holodexMigrationExists{exists: args[0].Value != holodexUploadMigrationVersion}, nil
}

func (c *holodexMigrationCapture) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	switch holodexMigrationSQL(query) {
	case "CREATE TABLE IF NOT EXISTS schema_migrations ( version VARCHAR(50) PRIMARY KEY, executed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() );":
		if len(args) != 0 {
			return nil, fmt.Errorf("unexpected create args: %v", args)
		}
	case "INSERT INTO schema_migrations (version) VALUES ($1)":
		if c.applied != 1 || len(args) != 1 || args[0].Value != holodexUploadMigrationVersion {
			return nil, fmt.Errorf("unexpected migration record: %v", args)
		}
		c.recorded++
	default:
		if len(args) != 0 {
			return nil, fmt.Errorf("unexpected migration args: %v", args)
		}
		c.query = query
		c.applied++
	}
	return driver.RowsAffected(1), nil
}

type holodexMigrationExists struct{ exists, read bool }

func (r *holodexMigrationExists) Columns() []string { return []string{"exists"} }
func (r *holodexMigrationExists) Close() error      { return nil }
func (r *holodexMigrationExists) Next(values []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	values[0] = r.exists
	return nil
}
