package repository

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// SQL の意味・行の可視性は TestIssue4StoredCopiesPostgres が検査する。
// ここでは実際に発行する SQL 全体・引数を独立したリテラルで固定する。
const issue4Restricted = `COALESCE(st.restriction_override,
 EXISTS (SELECT 1 FROM stream_stream_tags mt WHERE mt.stream_id = st.id AND mt.tag_id = 'members_only') AND NOT
 COALESCE((SELECT bool_and(COALESCE(eg.members_only_policy, '') = 'allow') FROM stream_channels eo
 JOIN channels eg ON eg.id = eo.channel_id WHERE eo.stream_id = st.id AND eo.is_owner), FALSE))`

const issue4PerformanceSelect = `SELECT p.id, p.stream_id, p.song_id, p.start_seconds, p.end_seconds, p.order_index,
 p.holodex_song_id, p.custom_tags, p.created_at, p.end_source, p.end_confirmed,
 st.title AS stream_title, st.stream_date, st.thumbnail_url, s.name AS song_name, s.original_artist, s.arts, ` + issue4Restricted + `
 FROM performances p JOIN streams st ON p.stream_id = st.id JOIN songs s ON p.song_id = s.id`

func issue4SQL(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestIssue4DirectReadersIssuedSQL(t *testing.T) {
	id := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
		scope := " AND NOT " + issue4Restricted
		if access == RestrictedView {
			scope = ""
		}
		for _, tc := range []struct {
			name, query string
			args        []driver.Value
			call        func(*PerformanceRepository, *PlaylistRepository) error
		}{
			{"performance", issue4PerformanceSelect + " WHERE p.id = $1" + scope, []driver.Value{id.String()},
				func(p *PerformanceRepository, _ *PlaylistRepository) error {
					_, err := p.FindByID(id, access)
					return err
				}},
			{"overlap-open-end", issue4PerformanceSelect + ` WHERE p.stream_id = $1 AND p.id <> $4
    AND p.start_seconds < $3 AND $2 < (CASE WHEN p.end_seconds = 0 THEN 1073741824 ELSE p.end_seconds END)` + scope + " ORDER BY p.start_seconds",
				[]driver.Value{"video", int64(30), int64(1073741824), id.String()},
				func(p *PerformanceRepository, _ *PlaylistRepository) error {
					_, err := p.FindOverlapping("video", 30, 0, id, access)
					return err
				}},
			{"playlist", issue4PerformanceSelect + " JOIN playlist_items pi ON pi.performance_id = p.id WHERE pi.playlist_id = $1" + scope + " ORDER BY pi.position ASC",
				[]driver.Value{id.String()}, func(_ *PerformanceRepository, p *PlaylistRepository) error {
					_, err := p.ListItems(id, access)
					return err
				}},
		} {
			t.Run(tc.name+"/"+accessName(access), func(t *testing.T) {
				db := perfAssociationDB(t, []perfAssociationStep{{query: issue4SQL(tc.query), args: tc.args}})
				p := NewPerformanceRepository(db)
				if err := tc.call(p, NewPlaylistRepository(db, p)); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestIssue4OwnSuggestionsIssuedSQL(t *testing.T) {
	userID := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	const columns = `id, target_type, target_id, target_key, target_label, kind,
 before_data, after_data, payload, note, status, created_by, created_by_name, client_hint,
 reviewed_by, review_note, created_at, reviewed_at`
	// コメントも含めて固定する。実装の restrictSuggestionsClause は呼ばない。
	const publicScope = ` AND ( CASE edit_suggestions.target_type
 WHEN 'performance' THEN EXISTS ( SELECT 1 FROM performances p JOIN streams st ON st.id = p.stream_id
 WHERE p.id = edit_suggestions.target_id AND NOT ` + issue4Restricted + ` )
 WHEN 'stream' THEN EXISTS ( SELECT 1 FROM streams st WHERE st.id = edit_suggestions.target_key AND NOT ` + issue4Restricted + ` )
 -- 配信に紐付かない対象は秘匿の対象外。**明示した種類だけ通す** ──
 -- ELSE TRUE にすると、将来知らない target_type が増えたときに公開側へ倒れる。
 WHEN 'song' THEN TRUE WHEN 'artist' THEN TRUE ELSE FALSE END )`
	for _, access := range []ViewerAccess{PublicAccess, RestrictedView} {
		for _, status := range []string{"", "pending"} {
			t.Run(accessName(access)+"/"+status, func(t *testing.T) {
				where := "WHERE created_by = $1"
				args := []driver.Value{userID.String()}
				if status != "" {
					where += " AND status = $2"
					args = append(args, status)
				}
				if access == PublicAccess {
					where += publicScope
				}
				listArgs := append(append([]driver.Value{}, args...), int64(20), int64(40))
				db := perfAssociationDB(t, []perfAssociationStep{
					{query: issue4SQL("SELECT COUNT(*) FROM edit_suggestions " + where), args: args, rows: [][]driver.Value{{int64(0)}}},
					{query: issue4SQL(fmt.Sprintf("SELECT %s FROM edit_suggestions %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d", columns, where, len(args)+1, len(args)+2)), args: listArgs},
				})
				rows, total, err := NewSuggestionRepository(db).ListByCreator(userID, status, 20, 40, access)
				if err != nil || total != 0 || len(rows) != 0 {
					t.Fatalf("rows=%v total=%d err=%v", rows, total, err)
				}
			})
		}
	}
}

func TestIssue4PlaylistCountIssuedSQL(t *testing.T) {
	id := uuid.MustParse("10000000-0000-0000-0000-000000000003")
	owner := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	query := `SELECT p.id, p.user_id, p.name, p.description, p.visibility, p.share_slug, p.created_at, p.updated_at,
 (SELECT count(*) FROM playlist_items pi JOIN performances pf ON pf.id = pi.performance_id
 JOIN streams st ON st.id = pf.stream_id WHERE pi.playlist_id = p.id AND NOT ` + issue4Restricted + `) AS item_count,
 COALESCE(NULLIF(u.display_name, ''), u.username) AS owner_name FROM playlists p JOIN users u ON u.id = p.user_id WHERE p.id = $1`
	db := perfAssociationDB(t, []perfAssociationStep{{query: issue4SQL(query), args: []driver.Value{id.String()},
		rows: [][]driver.Value{{id.String(), owner.String(), "playlist", "", "public", "slug", now, now, int64(2), "owner"}}}})
	row, err := NewPlaylistRepository(db, NewPerformanceRepository(db)).FindByIDWithMeta(id)
	if err != nil || row == nil || row.ItemCount != 2 || row.OwnerName != "owner" {
		t.Fatalf("row=%+v err=%v", row, err)
	}
}
