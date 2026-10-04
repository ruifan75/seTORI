package database

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// 専用 DB の独立 schema。実 runner で 061 -> 068 -> 069 を流す。
// 途中の名前衝突を除去して再試行し、同じ OID・データ・制約定義が保たれることを確認する。
func TestChannelRenamePostgres(t *testing.T) {
	url := os.Getenv("SETORI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SETORI_TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	schema := "channel_rename_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// 拡張は test schema の削除で消さない。他の専用 DB テストと同じ public に置く。
	exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public; CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public`)
	exec("CREATE SCHEMA " + schema)
	defer func() {
		if _, err := db.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	exec("SET search_path TO " + schema + ", public; SET statement_timeout TO '30s'; SET lock_timeout TO '3s'")
	through := func(version string) fstest.MapFS {
		t.Helper()
		files := fstest.MapFS{}
		entries, err := fs.ReadDir(migrationFS, "migrations")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") && e.Name()[:3] <= version {
				data, err := migrationFS.ReadFile("migrations/" + e.Name())
				if err != nil {
					t.Fatal(err)
				}
				files["migrations/"+e.Name()] = &fstest.MapFile{Data: data}
			}
		}
		return files
	}
	if err := runMigrations(db, through("061")); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO organizations(key, display_name) VALUES ('probe_org','所属'), ('probe_override','手動所属');
 INSERT INTO singers(id,name,english_name,organization,organization_override,is_hidden,metadata_source,members_only_policy,auto_fill_enabled,updated_at)
 VALUES ('owner','配信主','Owner','probe_org','probe_override',false,'manual','deny',true,'2020-01-01'),
 ('guest','参加者',NULL,NULL,NULL,true,'holodex',NULL,false,'2020-01-01');
 INSERT INTO streams(id,title,stream_date,is_hidden,holodex_data) VALUES
 ('probe_stream','歌枠','2020-01-01',false,'{"singer_ids":["owner"],"channel":{"id":"owner"}}');
 INSERT INTO stream_singers(stream_id,singer_id,is_owner) VALUES ('probe_stream','owner',true),('probe_stream','guest',false);
 INSERT INTO songs(id,name,original_artist) VALUES ('00000000-0000-0000-0000-000000000001','曲','原曲');
 INSERT INTO batch_fill_runs(id,mode,singer_id,status) VALUES ('00000000-0000-0000-0000-000000000002','force','owner,guest','done');
 INSERT INTO performances(id,stream_id,song_id,start_seconds,end_seconds,order_index,batch_run_id)
 VALUES ('00000000-0000-0000-0000-000000000003','probe_stream','00000000-0000-0000-0000-000000000001',10,100,1,'00000000-0000-0000-0000-000000000002');
 INSERT INTO performance_singers(performance_id,singer_id) VALUES ('00000000-0000-0000-0000-000000000003','owner');`)
	// 061 時点の既存行を 062..068 の追加列から独立して比べる。
	const legacyRows = `SELECT jsonb_build_object('channels',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM singers s),
 'participants',(SELECT jsonb_agg(to_jsonb(s) ORDER BY singer_id) FROM stream_singers s),
 'vocalists',(SELECT jsonb_agg(to_jsonb(s) ORDER BY singer_id) FROM performance_singers s),
 'batch_scope',(SELECT singer_id FROM batch_fill_runs WHERE id='00000000-0000-0000-0000-000000000002'))::text`
	before061 := channelRenameScalar(t, db, legacyRows)
	if err := runMigrations(db, through("068")); err != nil {
		t.Fatal(err)
	}
	if got := channelRenameScalar(t, db, legacyRows); got != before061 {
		t.Fatalf("062..068 changed existing rows:\n%s\nwant %s", got, before061)
	}
	exec(`INSERT INTO task_runs(id,kind,status,params) VALUES
 ('00000000-0000-0000-0000-000000000004','prepare','done','{"singer_id":"owner","singer_ids":["owner","guest"]}');
 INSERT INTO edit_suggestions(id,target_type,target_id,before_data,after_data) VALUES
 ('00000000-0000-0000-0000-000000000005','performance','00000000-0000-0000-0000-000000000003','{"singer_ids":["owner"]}','{"singer_ids":["guest"]}');`)
	before := channelRenameRows(t, db, false)
	catalog := channelRenameCatalog(t, db, schema)
	// ファイルの後半の ALTER INDEX を失敗させる。先行する table/column/constraint 改名も戻ること。
	exec(`CREATE TABLE name_collision(id int); CREATE INDEX idx_channels_auto_fill ON name_collision(id)`)
	err = runMigrations(db, through("069"))
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) || pgErr.Code != "42P07" || !strings.Contains(err.Error(), "069_rename_channels.sql") {
		t.Fatalf("want target index-name collision (42P07), got %v", err)
	}
	if got := channelRenameRows(t, db, false); got != before {
		t.Fatalf("rollback changed rows:\n%s\nwant %s", got, before)
	}
	if got := channelRenameCatalog(t, db, schema); !reflect.DeepEqual(got, catalog) {
		t.Fatalf("partial rename survived rollback:\n%v\nwant %v", got, catalog)
	}
	if got := channelRenameScalar(t, db, `SELECT count(*)::text FROM schema_migrations WHERE version='069_rename_channels.sql'`); got != "0" {
		t.Fatalf("failed migration recorded: %s", got)
	}
	exec(`DROP TABLE name_collision`)
	for i := 0; i < 2; i++ {
		if err := runMigrations(db, through("069")); err != nil {
			t.Fatal(err)
		}
	}
	if got := channelRenameRows(t, db, true); got != before {
		t.Fatalf("069 changed values/JSON:\n%s\nwant %s", got, before)
	}
	if got := channelRenameScalar(t, db, `SELECT count(*)::text FROM schema_migrations WHERE version='069_rename_channels.sql'`); got != "1" {
		t.Fatalf("migration ledger count: %s", got)
	}
	want := make([]string, len(catalog))
	// 固定の名称対応。OID と全 index/FK/check/trigger 定義は元のまま（名前だけ変更）。
	names := map[string]string{
		"singers": "channels", "stream_singers": "stream_channels", "singers_pkey": "channels_pkey",
		"singers_organization_fkey": "channels_organization_fkey", "singers_organization_override_fkey": "channels_organization_override_fkey",
		"singers_members_only_policy_check": "channels_members_only_policy_check", "stream_singers_pkey": "stream_channels_pkey",
		"stream_singers_stream_id_fkey": "stream_channels_stream_id_fkey", "stream_singers_singer_id_fkey": "stream_channels_channel_id_fkey",
		"idx_singers_visible": "idx_channels_visible", "idx_singers_auto_fill": "idx_channels_auto_fill",
		"idx_stream_singers_stream_id": "idx_stream_channels_stream_id", "idx_stream_singers_singer_id": "idx_stream_channels_channel_id",
		"idx_stream_singers_owner": "idx_stream_channels_owner", "update_singers_updated_at": "update_channels_updated_at",
	}
	word := regexp.MustCompile(`[a-zA-Z_][a-zA-Z_0-9]*`)
	for i, fact := range catalog {
		// singer_id は stream_channels の定義だけ変更。歌唱側は残す。
		if strings.Contains(fact, "|stream_singers|") {
			fact = regexp.MustCompile(`\bsinger_id\b`).ReplaceAllString(fact, "channel_id")
		}
		want[i] = word.ReplaceAllStringFunc(fact, func(v string) string {
			if next, ok := names[v]; ok {
				return next
			}
			return v
		})
	}
	if got := channelRenameCatalog(t, db, schema); !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata differs beyond fixed renames:\n%v\nwant %v", got, want)
	}
	if got := channelRenameScalar(t, db, `SELECT (to_regclass(current_schema()||'.singers') IS NULL AND to_regclass(current_schema()||'.stream_singers') IS NULL AND NOT EXISTS
 (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name IN ('stream_channels','batch_fill_runs') AND column_name='singer_id'))::text`); got != "true" {
		t.Fatalf("old channel identifiers remain: %s", got)
	}
	// 独立した陽性/陰性対照。関連が残る channel は削除不可、stream の削除は関連を連鎖削除する。
	for _, q := range []string{
		`DELETE FROM channels WHERE id='guest'`,                                                       // 配信参加者 FK の RESTRICT
		`DELETE FROM stream_channels WHERE channel_id='owner'; DELETE FROM channels WHERE id='owner'`, // 歌った人 FK の RESTRICT
		`DELETE FROM organizations WHERE key='probe_org'`,
		`DELETE FROM organizations WHERE key='probe_override'`,
		`UPDATE channels SET members_only_policy='invalid' WHERE id='owner'`,
	} {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(q)
		var pe *pq.Error
		code := pq.ErrorCode("23503")
		if strings.Contains(q, "invalid") {
			code = "23514"
		}
		if !errors.As(err, &pe) || pe.Code != code {
			t.Errorf("%s: want %s, got %v", q, code, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE organizations SET key='probe_org_renamed' WHERE key='probe_org';
 UPDATE organizations SET key='probe_override_renamed' WHERE key='probe_override';
 UPDATE channels SET name='更新後' WHERE id='owner'`)
	if got := channelRenameScalar(t, db, `SELECT (organization='probe_org_renamed' AND organization_override='probe_override_renamed' AND updated_at>'2020-01-01')::text FROM channels WHERE id='owner'`); got != "true" {
		t.Fatalf("FK UPDATE/trigger stopped working: %s", got)
	}
	exec(`DELETE FROM streams WHERE id='probe_stream'`)
	if got := channelRenameScalar(t, db, `SELECT ((SELECT count(*) FROM stream_channels)=0 AND (SELECT count(*) FROM performance_singers)=0 AND (SELECT count(*) FROM performances)=0 AND (SELECT count(*) FROM batch_fill_runs)=1)::text`); got != "true" {
		t.Fatalf("ON DELETE CASCADE/history retention changed: %s", got)
	}
	exec(`DELETE FROM channels`)
}

func channelRenameScalar(t *testing.T, db *sql.DB, q string) string {
	t.Helper()
	var out string
	if err := db.QueryRow(q).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
func channelRenameRows(t *testing.T, db *sql.DB, renamed bool) string {
	t.Helper()
	entity, participants, scope := "singers", "stream_singers", "to_jsonb(b)"
	participantRows := "to_jsonb(p)"
	participantOrder := "singer_id"
	if renamed {
		entity, participants = "channels", "stream_channels"
		scope = "to_jsonb(b)-'channel_id'||jsonb_build_object('singer_id',b.channel_id)"
		participantRows = "to_jsonb(p)-'channel_id'||jsonb_build_object('singer_id',p.channel_id)"
		participantOrder = "channel_id"
	}
	q := fmt.Sprintf(`SELECT jsonb_build_object(
 'channels',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM %s c),
 'participants',(SELECT jsonb_agg(%s ORDER BY %s) FROM %s p),
 'vocalists',(SELECT jsonb_agg(to_jsonb(v) ORDER BY singer_id) FROM performance_singers v),
 'batch',(SELECT jsonb_agg(%s ORDER BY id) FROM batch_fill_runs b),
 'streams',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM streams s),
 'performances',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM performances p),
 'tasks',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM task_runs r),
 'suggestions',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM edit_suggestions r))::text`, entity, participantRows, participantOrder, participants, scope)
	return channelRenameScalar(t, db, q)
}
func channelRenameCatalog(t *testing.T, db *sql.DB, schema string) []string {
	t.Helper()
	// 順序は改名しても変わらない category/OID。有効な索引、FK の動作、索引の列順も含む。
	rows, err := db.Query(`WITH target AS (SELECT oid,relname FROM pg_class WHERE relnamespace=$1::regnamespace
 AND relname IN ('singers','channels','stream_singers','stream_channels','performance_singers','batch_fill_runs'))
 SELECT category||'|'||oid||'|'||name||'|'||parent||'|'||definition FROM (
 SELECT 'table' category,c.oid,c.relname name,c.relname parent,c.relkind::text definition FROM pg_class c JOIN target t ON t.oid=c.oid
 UNION ALL SELECT 'index',i.indexrelid,ic.relname,t.relname,pg_get_indexdef(i.indexrelid)||' valid='||i.indisvalid::text FROM pg_index i JOIN target t ON t.oid=i.indrelid JOIN pg_class ic ON ic.oid=i.indexrelid
 UNION ALL SELECT 'constraint',c.oid,c.conname,t.relname,pg_get_constraintdef(c.oid)||' delete='||c.confdeltype::text||' update='||c.confupdtype::text FROM pg_constraint c JOIN target t ON t.oid=c.conrelid
 UNION ALL SELECT 'trigger',g.oid,g.tgname,t.relname,pg_get_triggerdef(g.oid) FROM pg_trigger g JOIN target t ON t.oid=g.tgrelid WHERE NOT g.tgisinternal
 ) facts ORDER BY category,oid`, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var fact string
		if err := rows.Scan(&fact); err != nil {
			t.Fatal(err)
		}
		out = append(out, fact)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
