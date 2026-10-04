package repository

import (
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/database"
	"net/url"
	"os"
	"reflect"
	"sort"
	"testing"
)

// 専用DB内の独立schema。全connectionのsearch_pathをDSNで指定し、外側のDBは変更しない。
func visibilityPostgres(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SETORI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SETORI_TEST_DATABASE_URL が未設定")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin.SetMaxOpenConns(1)
	schema := "visibility_" + uuid.New().String()[:8]
	if _, err := admin.Exec(`SELECT pg_advisory_lock(901127)`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public; CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public; CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	q.Set("search_path", schema+",public")
	parsed.RawQuery = q.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if err := database.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`SELECT pg_advisory_unlock(901127)`); err != nil {
		t.Fatal(err)
	}
	return db
}
func visibilityExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func visibilityHidden(t *testing.T, db *sql.DB, id string) bool {
	t.Helper()
	var hidden bool
	if err := db.QueryRow(`SELECT is_hidden FROM streams WHERE id=$1`, id).Scan(&hidden); err != nil {
		t.Fatal(err)
	}
	return hidden
}
func TestVisibilityPostgresCandidatesApplyRevertAndRestriction(t *testing.T) {
	db := visibilityPostgres(t)
	r := NewVisibilityReviewRepository(db)
	visibilityExec(t, db, `INSERT INTO channels (id,name,is_hidden) VALUES ('owner','Owner',FALSE)`)
	for _, x := range []struct {
		id       string
		duration any
		hidden   bool
		music    bool
	}{{"long", 181, true, true}, {"member", 600, true, true}, {"short", 180, true, true}, {"unknown", nil, true, true}, {"other", 600, true, false}, {"visible", 600, false, true}, {"dismissed", 600, true, true}} {
		visibilityExec(t, db, `INSERT INTO streams (id,title,stream_date,duration_seconds,is_hidden) VALUES ($1,$1,NOW(),$2,$3)`, x.id, x.duration, x.hidden)
		visibilityExec(t, db, `INSERT INTO stream_channels (stream_id,channel_id,is_owner) VALUES ($1,'owner',TRUE)`, x.id)
		if x.music {
			visibilityExec(t, db, `INSERT INTO stream_stream_tags (stream_id,tag_id) VALUES ($1,'singing')`, x.id)
		}
	}
	visibilityExec(t, db, `INSERT INTO non_singing_checks(stream_id) VALUES ('dismissed'); INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES ('member','members_only')`)
	visibilityExec(t, db, `INSERT INTO songs(id,name,original_artist) VALUES ($1,'test','artist')`, visibilityID)
	for _, id := range []string{"long", "member"} {
		visibilityExec(t, db, `INSERT INTO performances(stream_id,song_id,start_seconds,end_seconds,order_index) VALUES ($1,$2,10,60,1)`, id, visibilityID)
	}
	rows, count, err := r.Candidates(false, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, x := range rows {
		ids = append(ids, x.ID)
	}
	sort.Strings(ids)
	if count != 2 || !reflect.DeepEqual(ids, []string{"long", "member"}) {
		t.Fatalf("candidates=%v count=%d", ids, count)
	}
	run, err := r.Preview(ids, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !visibilityHidden(t, db, "long") {
		t.Fatal("previewが表示を変更した")
	}
	n, err := r.Apply(run)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	perf := NewPerformanceRepository(db)
	// 陰性だけでなく陽性も実際の読み取りから確認する。SQLエラーは握りつぶさない。
	for _, x := range []struct {
		id     string
		access ViewerAccess
		want   int
	}{{"long", PublicAccess, 1}, {"member", PublicAccess, 0}, {"member", RestrictedView, 1}} {
		rows, err := perf.FindByStreamID(x.id, x.access)
		if err != nil || len(rows) != x.want {
			t.Fatalf("performances %s=%d want%d err=%v", x.id, len(rows), x.want, err)
		}
	}
	var restricted bool
	if err := db.QueryRow(`SELECT ` + EffectiveRestrictedExpr("s") + ` FROM streams s WHERE s.id='member'`).Scan(&restricted); err != nil || !restricted {
		t.Fatal("表示変更で秘匿が解けた", restricted, err)
	}
	// 後のタイトル編集がある行は、取り消しで触らない。
	visibilityExec(t, db, `UPDATE streams SET title='edited', updated_at=NOW()+INTERVAL '1 second' WHERE id='long'`)
	reverted, skipped, err := r.Revert(run)
	if err != nil || reverted != 1 || skipped != 1 {
		t.Fatal(reverted, skipped, err)
	}
	if visibilityHidden(t, db, "long") || !visibilityHidden(t, db, "member") {
		t.Fatal("撤回が後の編集を巻き込んだ/対象を戻していない")
	}
	var auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM visibility_review_items WHERE run_id=$1 AND field_name='is_hidden' AND before_hidden=TRUE AND after_hidden=FALSE AND after_updated_at IS NOT NULL`, run).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatal("前後値の退避", auditCount, err)
	}
	// 候補条件が変わっても否定の判断は一覧・取消ができる。
	visibilityExec(t, db, `DELETE FROM stream_stream_tags WHERE stream_id='dismissed'`)
	rows, count, err = r.Candidates(true, 100, 0)
	if err != nil || count != 1 || len(rows) != 1 || rows[0].ID != "dismissed" {
		t.Fatal(rows, count, err)
	}
	if err := NewStreamRepository(db).DeleteNonSingingCheck("dismissed"); err != nil {
		t.Fatal(err)
	}
}
func TestVisibilityPostgresConflictIsAllOrNothing(t *testing.T) {
	db := visibilityPostgres(t)
	r := NewVisibilityReviewRepository(db)
	visibilityExec(t, db, `INSERT INTO streams(id,title,stream_date,duration_seconds,is_hidden) VALUES ('one','one',NOW(),500,TRUE),('two','two',NOW(),500,TRUE); INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES ('one','singing'),('two','singing')`)
	for _, change := range []string{"timestamp", "tag", "dismissal"} {
		t.Run(change, func(t *testing.T) {
			run, err := r.Preview([]string{"one", "two"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "timestamp":
				visibilityExec(t, db, `UPDATE streams SET updated_at=NOW()+INTERVAL '1 second' WHERE id='two'`)
			case "tag":
				visibilityExec(t, db, `DELETE FROM stream_stream_tags WHERE stream_id='two'`)
			case "dismissal":
				visibilityExec(t, db, `INSERT INTO non_singing_checks(stream_id) VALUES ('two')`)
			}
			if _, err := r.Apply(run); !errors.Is(err, ErrVisibilityConflict) {
				t.Fatal("stale preview was applied", err)
			}
			if !visibilityHidden(t, db, "one") || !visibilityHidden(t, db, "two") {
				t.Fatal("一部だけ表示へ変えた")
			}
			switch change {
			case "tag":
				visibilityExec(t, db, `INSERT INTO stream_stream_tags(stream_id,tag_id) VALUES ('two','singing')`)
			case "dismissal":
				visibilityExec(t, db, `DELETE FROM non_singing_checks WHERE stream_id='two'`)
			}
		})
	}
}
