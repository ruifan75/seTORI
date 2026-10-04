package database

import (
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMigrationRejectsTransactionControlBeforeBegin(t *testing.T) {
	for _, command := range []string{
		"BEGIN", "begin transaction", "START TRANSACTION", "COMMIT", "COMMIT AND CHAIN",
		"END WORK", "ROLLBACK", "ROLLBACK AND CHAIN", "ROLLBACK TO SAVEPOINT x",
		"ABORT", "SAVEPOINT x", "RELEASE SAVEPOINT x", "PREPARE TRANSACTION 'probe'",
	} {
		head := strings.Fields(strings.ToUpper(command))[0]
		if head == "START" || head == "PREPARE" {
			head += " TRANSACTION"
		}
		for _, prefix := range []string{"", "CREATE TABLE before_probe(id int); /* outer /* nested */ comment */ "} {
			t.Run(command+"/"+prefix, func(t *testing.T) {
				db, state := newMigrationProbe(t, "")
				// Also place DDL after the command: the entire file must be checked
				// before any statement, rather than failing after a partial commit.
				err := applyMigration(db, "069_probe.sql", []byte(prefix+command+"; CREATE TABLE after_probe(id int);"))
				statement := 1
				if prefix != "" {
					statement = 2
				}
				want := fmt.Sprintf("マイグレーション 069_probe.sql の SQL 検査失敗: 文 %d: %s は使えません（トランザクションは migration runner が管理します）", statement, head)
				if state.began != 0 || len(state.events) != 0 || len(state.tables) != 0 || len(state.versions) != 0 {
					t.Fatalf("SQL reached DB before validation: %+v", state)
				}
				if err == nil || err.Error() != want {
					t.Fatalf("got %v; want %s", err, want)
				}
			})
		}
	}
}

func TestMigrationSQLQuotedBodiesAndComments(t *testing.T) {
	for _, query := range []string{
		"-- COMMIT;\r SELECT 1; /* BEGIN; /* ROLLBACK; */ END; */ SELECT 2;",
		"INSERT INTO probe VALUES ('it''s; COMMIT;', E'it\\'s; ROLLBACK;', 'END;');",
		`CREATE TABLE "COMMIT;" ("END" text); COMMENT ON TABLE "COMMIT;" IS 'BEGIN;';`,
		"DO $$ BEGIN PERFORM 1; END; $$; SELECT 1;",
		"DO $body_1$ BEGIN PERFORM 'COMMIT;'; END; $body_1$; SELECT 1;",
		"CREATE FUNCTION probe() RETURNS integer LANGUAGE SQL BEGIN ATOMIC SELECT CASE WHEN true THEN 1 ELSE 2 END; END; SELECT 1;",
		"PREPARE probe AS SELECT $1::int; EXECUTE probe(1);",
		"SELECT id$with$dollars FROM probe; SELECT 1;",
		// These fail inside the PostgreSQL transaction; the lexer must not
		// pretend to validate every kind of nontransactional SQL itself.
		"CREATE INDEX CONCURRENTLY idx_probe ON probe(id);",
		"VACUUM probe;",
	} {
		t.Run(query, func(t *testing.T) {
			if err := validateMigrationSQL(query); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, query := range []string{
		"DO $$ BEGIN PERFORM 1; END; $$; -- tail\n COMMIT;",
		"SELECT E'it\\'s; BEGIN;'; COMMIT;",
		"SELECT 'COMMIT;'; -- ignored\r COMMIT;",
		"CREATE FUNCTION probe() RETURNS integer LANGUAGE SQL BEGIN ATOMIC SELECT CASE WHEN true THEN 1 ELSE 2 END; END; COMMIT;",
	} {
		t.Run(query, func(t *testing.T) {
			want := "文 2: COMMIT は使えません（トランザクションは migration runner が管理します）"
			if err := validateMigrationSQL(query); err == nil || err.Error() != want {
				t.Fatalf("got %v; want %s", err, want)
			}
		})
	}
	for _, query := range []string{"/* open", "SELECT 'open", "SELECT E'open\\", "DO $tag$ open", `SELECT "open`} {
		t.Run(query, func(t *testing.T) {
			if err := validateMigrationSQL(query); err == nil {
				t.Fatal("unterminated SQL quote/comment accepted")
			}
		})
	}
}

func TestEmbeddedMigrationsUseRunnerTransaction(t *testing.T) {
	paths, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			query, err := migrationFS.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateMigrationSQL(string(query)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Seed the unchanged filename-based ledger through real migration 061. This
// driver executes only exact SQL from pending files and checks transaction order;
// it does not evaluate PostgreSQL DDL or claim to validate a production schema.
func TestRunMigrationsKeepsExistingLedgerThrough061(t *testing.T) {
	db, state := newMigrationProbe(t, "")
	state.files = fstest.MapFS{}
	paths, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	wantVersions := make(map[string]bool)
	var wantEvents []string
	for _, path := range paths {
		version := strings.TrimPrefix(path, "migrations/")
		data, err := migrationFS.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		state.files[path] = &fstest.MapFile{Data: data}
		wantVersions[version] = true
		if version[:3] <= "061" {
			state.versions[version], state.tables[version] = true, true
			// Recorded files must be skipped even before SQL validation; no
			// retroactive interpretation or rewrite of historical SQL is allowed.
			state.files[path] = &fstest.MapFile{Data: []byte("COMMIT;")}
		} else {
			wantEvents = append(wantEvents, "begin", "ddl:"+version+":tx", "record:"+version+":tx", "commit")
		}
	}
	for restart := 0; restart < 2; restart++ {
		if err := runMigrations(db, state.files); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(state.versions, wantVersions) || !reflect.DeepEqual(state.tables, wantVersions) || !reflect.DeepEqual(state.events, wantEvents) {
			t.Fatalf("restart=%d versions=%v tables=%v events=%v; want unchanged old ledger and only pending file transactions %v", restart, state.versions, state.tables, state.events, wantEvents)
		}
	}
}
