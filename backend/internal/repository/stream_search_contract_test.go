package repository

import (
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/ruifan75/setori/internal/models"
)

// 実装の SQL 生成関数から期待値を作らない。CTE・集合の関連先・HAVING・
// 外側の列・引数まで、実際の SearchStreams が発行した2本を完全一致で検査する。
const streamSearchWantRestriction = `NOT COALESCE(st.restriction_override,
 EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = st.id AND mt.tag_id = 'members_only')
 AND NOT COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow')
 FROM stream_singers eo JOIN singers eg ON eg.id = eo.singer_id
 WHERE eo.stream_id = st.id AND eo.is_owner), FALSE))`

const streamSearchWantColumns = `s.id, s.title, s.stream_date, s.duration_seconds,
 s.thumbnail_url, s.holodex_data, s.holodex_hash, s.comment_raw, s.comment_songs,
 s.is_processed, s.is_hidden, s.restriction_override, s.created_at, s.updated_at,
 COALESCE(s.restriction_override,
 EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = s.id AND mt.tag_id = 'members_only')
 AND NOT COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow')
 FROM stream_singers eo JOIN singers eg ON eg.id = eo.singer_id
 WHERE eo.stream_id = s.id AND eo.is_owner), FALSE))`

func TestStreamSearchIssuedSQL(t *testing.T) {
	// なし・1条件・全ての2条件・3条件・全4条件。各配列には複数IDを渡す。
	for mask := 0; mask < 16; mask++ {
		f := models.StreamSearchFilters{}
		if mask&1 != 0 {
			f.ParticipantIDs = []string{"owner", "guest"}
		}
		if mask&2 != 0 {
			f.VocalistIDs = []string{"voice", "guest"}
		}
		if mask&4 != 0 {
			f.StreamTagIDs = []string{"singing", "3d"}
		}
		if mask&8 != 0 {
			f.PerformanceTagIDs = []string{"acoustic", "piano"}
		}
		for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
			t.Run(fmt.Sprintf("mask=%02d/access=%d", mask, access), func(t *testing.T) {
				assertStreamSearchSQL(t, f, access)
			})
		}
	}
	for _, f := range []models.StreamSearchFilters{
		{Query: "歌枠", OwnerID: "owner", ParticipantIDs: []string{"owner"}, VocalistIDs: []string{"voice"}, StreamTagIDs: []string{"singing"}, PerformanceTagIDs: []string{"acoustic"}},
		{Query: "歌枠", OwnerID: "owner"},
		// HTTP入口で重複を除く。repositoryへ直接重複を渡した時は、旧実装と同じ
		// COUNT(DISTINCT) = 配列長の条件（成立しない）を保つ。
		{ParticipantIDs: []string{"owner", "owner"}, VocalistIDs: []string{"voice", "voice"}, StreamTagIDs: []string{"singing", "singing"}, PerformanceTagIDs: []string{"acoustic", "acoustic"}},
	} {
		for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
			assertStreamSearchSQL(t, f, access)
		}
	}
}

func assertStreamSearchSQL(t *testing.T, f models.StreamSearchFilters, access ViewerAccess) {
	t.Helper()
	prefix := ""
	if len(f.VocalistIDs) > 0 || len(f.PerformanceTagIDs) > 0 {
		scope := streamSearchWantRestriction
		if access == RestrictedView {
			scope = "TRUE"
		}
		prefix = "WITH searchable_performance_streams AS MATERIALIZED (SELECT st.id FROM streams st WHERE " + scope + ") "
	}
	where := "WHERE TRUE"
	var args []driver.Value
	if f.Query != "" {
		args = append(args, f.Query)
		where += fmt.Sprintf(" AND s.title ILIKE '%%' || $%d || '%%'", len(args))
	}
	if f.OwnerID != "" {
		args = append(args, f.OwnerID)
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = $%d AND ss.is_owner = TRUE)", len(args))
	}
	for _, part := range []struct {
		ids []string
		sql string
	}{
		{f.ParticipantIDs, ` AND s.id IN (SELECT ss.stream_id FROM stream_singers ss WHERE ss.singer_id = ANY($%d) GROUP BY ss.stream_id HAVING COUNT(DISTINCT ss.singer_id) = %d)`},
		{f.VocalistIDs, ` AND s.id IN (SELECT p.stream_id FROM performances p JOIN performance_singers ps ON ps.performance_id = p.id JOIN searchable_performance_streams st ON st.id = p.stream_id WHERE ps.singer_id = ANY($%d) GROUP BY p.stream_id HAVING COUNT(DISTINCT ps.singer_id) = %d)`},
		{f.StreamTagIDs, ` AND s.id IN (SELECT sst.stream_id FROM stream_stream_tags sst WHERE sst.tag_id = ANY($%d) GROUP BY sst.stream_id HAVING COUNT(DISTINCT sst.tag_id) = %d)`},
		{f.PerformanceTagIDs, ` AND s.id IN (SELECT p.stream_id FROM performances p JOIN performance_performance_tags ppt ON ppt.performance_id = p.id JOIN searchable_performance_streams st ON st.id = p.stream_id WHERE ppt.tag_id = ANY($%d) GROUP BY p.stream_id HAVING COUNT(DISTINCT ppt.tag_id) = %d)`},
	} {
		if len(part.ids) == 0 {
			continue
		}
		value, err := pq.Array(part.ids).Value()
		if err != nil {
			t.Fatal(err)
		}
		args = append(args, value)
		where += fmt.Sprintf(part.sql, len(args), len(part.ids))
	}
	listArgs := append(append([]driver.Value(nil), args...), int64(20), int64(40))
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	db := streamTagContractDB(t,
		streamTagSQLStep{prefix + "SELECT COUNT(*) FROM streams s " + where, args, [][]driver.Value{{int64(81)}}},
		streamTagSQLStep{prefix + "SELECT " + streamSearchWantColumns + " FROM streams s " + where + fmt.Sprintf(" ORDER BY s.stream_date DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2), listArgs,
			[][]driver.Value{{"fixture", "配信", date, int64(1800), nil, nil, nil, nil, nil, true, true, true, date, date, true}}})
	streams, total, err := NewStreamRepository(db).SearchStreams(f, 20, 40, access)
	if err != nil {
		t.Fatal(err)
	}
	if total != 81 || len(streams) != 1 || streams[0].ID != "fixture" || !streams[0].IsHidden || !streams[0].IsRestrictedEffective {
		t.Fatalf("total=%d rows=%+v", total, streams)
	}
}

// 対照 SQL は修正前の4つの相関条件を固定したもの。統合テストの結果比較用で、
// 変更後の集合や権限の式を呼んで期待値を作らない。
func legacyStreamSearchWhere(f models.StreamSearchFilters, access ViewerAccess) (string, []any) {
	where := "WHERE TRUE"
	var args []any
	if f.Query != "" {
		args = append(args, f.Query)
		where += fmt.Sprintf(" AND s.title ILIKE '%%' || $%d || '%%'", len(args))
	}
	if f.OwnerID != "" {
		args = append(args, f.OwnerID)
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = $%d AND ss.is_owner = TRUE)", len(args))
	}
	scope := streamSearchWantRestriction
	if access == RestrictedView {
		scope = "TRUE"
	}
	for _, part := range []struct {
		ids []string
		sql string
	}{
		{f.ParticipantIDs, " AND (SELECT COUNT(DISTINCT ss.singer_id) FROM stream_singers ss WHERE ss.stream_id = s.id AND ss.singer_id = ANY($%d)) = %d"},
		{f.VocalistIDs, " AND (SELECT COUNT(DISTINCT ps.singer_id) FROM performances p JOIN performance_singers ps ON ps.performance_id = p.id JOIN streams st ON st.id = p.stream_id WHERE p.stream_id = s.id AND " + scope + " AND ps.singer_id = ANY($%d)) = %d"},
		{f.StreamTagIDs, " AND (SELECT COUNT(DISTINCT sst.tag_id) FROM stream_stream_tags sst WHERE sst.stream_id = s.id AND sst.tag_id = ANY($%d)) = %d"},
		{f.PerformanceTagIDs, " AND (SELECT COUNT(DISTINCT ppt.tag_id) FROM performances p JOIN performance_performance_tags ppt ON ppt.performance_id = p.id JOIN streams st ON st.id = p.stream_id WHERE p.stream_id = s.id AND " + scope + " AND ppt.tag_id = ANY($%d)) = %d"},
	} {
		if len(part.ids) == 0 {
			continue
		}
		args = append(args, pq.Array(part.ids))
		where += fmt.Sprintf(part.sql, len(args), len(part.ids))
	}
	return where, args
}
