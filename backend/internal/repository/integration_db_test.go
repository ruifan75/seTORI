package repository

import (
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/ruifan75/setori/internal/database"
)

// SETORI_TEST_DATABASE_URL は専用のテスト DB を指定する。各テストは独立 schema を作り、削除する。
func reviewTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("SETORI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SETORI_TEST_DATABASE_URL が未設定")
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`SELECT pg_advisory_lock(901122)`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`SELECT pg_advisory_unlock(901122)`)
	// 複数パッケージが並行に schema を作っても、extension の関数を共有できるよう public に置く。
	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public; CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	schema := "review_" + uuid.New().String()[:8]
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.Exec(`SET search_path TO ` + schema + `, public`); err != nil {
		t.Fatal(err)
	}
	if err := database.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}
