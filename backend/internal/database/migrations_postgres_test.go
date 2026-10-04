package database

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Use a dedicated test DB. Each case owns a new schema; no application rows are touched.
// A trigger rejects the ledger INSERT after real 066/067 DDL has executed.
func TestRunMigrationsPostgresRecordFailureCanRetry(t *testing.T) {
	url := os.Getenv("SETORI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SETORI_TEST_DATABASE_URL is not set")
	}
	for _, failedVersion := range []string{"066_add_prepare_tasks.sql", "067_add_visibility_review.sql"} {
		t.Run(failedVersion, func(t *testing.T) {
			db, err := sql.Open("postgres", url)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			schema := "migration_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := db.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
					t.Error(err)
				}
			}()
			if _, err := db.Exec("SET search_path TO " + schema + ", public; SET statement_timeout TO '15s'; SET lock_timeout TO '3s'"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TABLE users (id UUID PRIMARY KEY);
    CREATE TABLE schema_migrations (version VARCHAR(50) PRIMARY KEY, executed_at TIMESTAMPTZ DEFAULT NOW())`); err != nil {
				t.Fatal(err)
			}
			files := fstest.MapFS{}
			for _, v := range []string{"065_add_task_runs.sql", "066_add_prepare_tasks.sql", "067_add_visibility_review.sql"} {
				data, err := migrationFS.ReadFile("migrations/" + v)
				if err != nil {
					t.Fatal(err)
				}
				files["migrations/"+v] = &fstest.MapFile{Data: data}
			}
			// failedVersion comes only from the fixed literals above.
			if _, err := db.Exec(`CREATE FUNCTION reject_migration_record() RETURNS trigger LANGUAGE plpgsql AS $$
    BEGIN IF NEW.version = '` + failedVersion + `' THEN RAISE EXCEPTION 'migration probe record failure'; END IF; RETURN NEW; END $$;
    CREATE TRIGGER reject_migration_record BEFORE INSERT ON schema_migrations
    FOR EACH ROW EXECUTE FUNCTION reject_migration_record()`); err != nil {
				t.Fatal(err)
			}
			err = runMigrations(db, files)
			if err == nil || !strings.Contains(err.Error(), "migration probe record failure") || !strings.Contains(err.Error(), failedVersion) {
				t.Fatalf("got %v; want target ledger rejection", err)
			}
			var recorded int
			if err := db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil {
				t.Fatal(err)
			}
			want := 1
			if strings.HasPrefix(failedVersion, "067") {
				want = 2
			}
			if recorded != want {
				t.Fatalf("recorded=%d; want %d", recorded, want)
			}
			var phase, review bool
			if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=$1 AND table_name='task_runs' AND column_name='phase'), to_regclass($1 || '.visibility_review_runs') IS NOT NULL`, schema).Scan(&phase, &review); err != nil {
				t.Fatal(err)
			}
			if phase != (want == 2) || review {
				t.Fatalf("phase=%v, review=%v; want phase=%v, review=false", phase, review, want == 2)
			}
			var statusCheck string
			if err := db.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='task_runs'::regclass AND conname='task_runs_status_check'`).Scan(&statusCheck); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(statusCheck, "cancelled") != (want == 2) {
				t.Fatalf("unexpected status constraint after rollback: %s", statusCheck)
			}
			if _, err := db.Exec("DROP TRIGGER reject_migration_record ON schema_migrations"); err != nil {
				t.Fatal(err)
			}
			// Both retry and the next restart must succeed with non-idempotent DDL.
			for i := 0; i < 2; i++ {
				if err := runMigrations(db, files); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil {
				t.Fatal(err)
			}
			if recorded != 3 {
				t.Fatalf("recorded=%d; want 3", recorded)
			}
			for _, table := range []string{"visibility_review_runs", "visibility_review_items"} {
				var exists bool
				if err := db.QueryRow("SELECT to_regclass($1) IS NOT NULL", schema+"."+table).Scan(&exists); err != nil {
					t.Fatal(err)
				}
				if !exists {
					t.Fatalf("%s missing after retry", table)
				}
			}
		})
	}
}

// Tx 非対応の文は PostgreSQL の実行エラーで戻り、Tx を終える文は
// 実行前の検査で戻る。どちらも DDL と適用記録を残さないことを確認する。
func TestApplyMigrationPostgresRejectsTransactionEscape(t *testing.T) {
	url := os.Getenv("SETORI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SETORI_TEST_DATABASE_URL is not set")
	}
	for _, tc := range []struct {
		command string
		pgError bool
	}{
		{"CREATE INDEX CONCURRENTLY idx_probe ON probe(id)", true},
		{"VACUUM probe", true},
		{"COMMIT", false},
		{"COMMIT AND CHAIN", false},
		{"ROLLBACK", false},
		{"ROLLBACK AND CHAIN", false},
	} {
		t.Run(tc.command, func(t *testing.T) {
			db, err := sql.Open("postgres", url)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			schema := "migration_escape_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := db.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
					t.Error(err)
				}
			}()
			if _, err := db.Exec("SET search_path TO " + schema + "; SET statement_timeout TO '15s'; SET lock_timeout TO '3s'; CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)"); err != nil {
				t.Fatal(err)
			}
			err = applyMigration(db, "069_probe.sql", []byte("CREATE TABLE probe(id int); "+tc.command+";"))
			if tc.pgError {
				var pgErr *pq.Error
				if !errors.As(err, &pgErr) || pgErr.Code != "25001" {
					t.Fatalf("got %v; want PostgreSQL active_sql_transaction (25001)", err)
				}
			} else {
				head := strings.Fields(tc.command)[0]
				want := "マイグレーション 069_probe.sql の SQL 検査失敗: 文 2: " + head + " は使えません（トランザクションは migration runner が管理します）"
				if err == nil || err.Error() != want {
					t.Fatalf("got %v; want %s", err, want)
				}
			}
			var tableExists bool
			var recorded int
			if err := db.QueryRow("SELECT to_regclass($1) IS NOT NULL, (SELECT count(*) FROM schema_migrations)", schema+".probe").Scan(&tableExists, &recorded); err != nil {
				t.Fatal(err)
			}
			if tableExists || recorded != 0 {
				t.Fatalf("nontransactional SQL left state: table=%v records=%d", tableExists, recorded)
			}
		})
	}
}
