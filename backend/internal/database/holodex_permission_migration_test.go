package database

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/lib/pq"
)

// SETORI_TEST_DATABASE_URL を専用 DB に設定したときだけ実行する。
// 実際の 068 を TEMP roles に適用する。READ ONLY transaction なので、
// 仮に migration が public.roles を指しても既存の行は書き換えられない。
func TestHolodexUploadPermissionMigration(t *testing.T) {
	url := os.Getenv("SETORI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SETORI_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// DDL は read-only transaction の開始前に行う。行の変更は TEMP 表に限る。
	if _, err := conn.ExecContext(ctx, `CREATE TEMP TABLE roles (
		name TEXT PRIMARY KEY, is_system BOOLEAN NOT NULL,
		permissions TEXT[] NOT NULL, updated_at TIMESTAMPTZ NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path = pg_temp"); err != nil {
		t.Fatal(err)
	}
	content, err := migrationFS.ReadFile("migrations/068_add_holodex_upload_permission.sql")
	if err != nil {
		t.Fatal(err)
	}
	baseline := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, admin := range []struct {
		name   string
		system bool
		before []string
		after  []string
	}{
		{"default admin", true, []string{"*"}, []string{"*", "holodex:upload"}},
		{"edited admin with sync", true, []string{"content:edit", "sync:run"}, []string{"content:edit", "sync:run", "holodex:upload"}},
		{"admin without write permissions", true, []string{"content:edit", "users:manage"}, []string{"content:edit", "users:manage"}},
		{"admin with empty permissions", true, []string{}, []string{}},
		{"admin with prior upload", true, []string{"holodex:upload"}, []string{"holodex:upload"}},
		{"admin with all prior keys", true, []string{"*", "sync:run", "holodex:upload"}, []string{"*", "sync:run", "holodex:upload"}},
		{"non-system admin", false, []string{"sync:run", "holodex:upload"}, []string{"sync:run"}},
	} {
		t.Run(admin.name, func(t *testing.T) {
			if _, err := tx.ExecContext(ctx, "DELETE FROM roles"); err != nil {
				t.Fatal(err)
			}
			fixtures := []struct {
				name   string
				system bool
				before []string
				after  []string
			}{
				{"admin", admin.system, admin.before, admin.after},
				{"editor", true, []string{"content:edit", "sync:run", "logs:view"}, []string{"content:edit", "sync:run", "logs:view"}},
				{"viewer", true, []string{}, []string{}},
				{"custom sync", false, []string{"sync:run", "content:edit"}, []string{"sync:run", "content:edit"}},
				{"custom wildcard", false, []string{"*"}, []string{"*"}},
				{"custom wildcard with prior upload", false, []string{"*", "holodex:upload"}, []string{"*"}},
				{"custom prior unknown key", false, []string{"sync:run", "holodex:upload"}, []string{"sync:run"}},
				{"editor prior unknown key", true, []string{"holodex:upload", "content:edit"}, []string{"content:edit"}},
			}
			for _, f := range fixtures {
				if _, err := tx.ExecContext(ctx, "INSERT INTO roles VALUES ($1, $2, $3, $4)", f.name, f.system, pq.Array(f.before), baseline); err != nil {
					t.Fatal(err)
				}
			}
			for run := 0; run < 2; run++ {
				// NOW() は transaction 内で一定なので、再実行前に時刻を戻して
				// 不要な UPDATE も検出する（時刻同士を比べるだけでは見逃す）。
				if run == 1 {
					if _, err := tx.ExecContext(ctx, "UPDATE roles SET updated_at = $1", baseline); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := tx.ExecContext(ctx, string(content)); err != nil {
					t.Fatal(err)
				}
				for _, f := range fixtures {
					var got []string
					var updated time.Time
					if err := tx.QueryRowContext(ctx, "SELECT permissions, updated_at FROM roles WHERE name = $1", f.name).Scan(pq.Array(&got), &updated); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, f.after) {
						t.Errorf("run %d role %q: permissions = %v, want %v", run, f.name, got, f.after)
					}
					changed := run == 0 && !reflect.DeepEqual(f.before, f.after)
					if changed == updated.Equal(baseline) {
						t.Errorf("run %d role %q: changed = %v, updated_at = %v", run, f.name, changed, updated)
					}
				}
			}
		})
	}
}
