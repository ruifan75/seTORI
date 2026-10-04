package repository

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
)

// NULL/false/true の裁定・控え × 検出の有無 × 所有者の組を実際の FindByID/Scan に通す。
// 各所有者 fixture の期待値は、SQL の実装を呼ばずに指定する。
func TestRestrictionReviewTruthTablePostgres(t *testing.T) {
	dsn := os.Getenv("SETORI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SETORI_TEST_DATABASE_URL が未設定")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("専用 PostgreSQL URL を指定してください")
	}
	params := u.Query()
	params.Set("default_transaction_read_only", "on")
	u.RawQuery = params.Encode()
	states := []struct {
		name  string
		value sql.NullBool
	}{
		{"null", sql.NullBool{}}, {"false", sql.NullBool{Valid: true}}, {"true", sql.NullBool{Valid: true, Bool: true}},
	}
	owners := []struct {
		name, rows string
		allAllow   bool
	}{
		{"none", `SELECT NULL::text id,NULL::text policy,NULL::boolean is_owner WHERE FALSE`, false},
		{"allow", `SELECT 'a'::text id,'allow'::text policy,TRUE is_owner`, true},
		{"deny", `SELECT 'a'::text id,'deny'::text policy,TRUE is_owner`, false},
		{"unknown", `SELECT 'a'::text id,NULL::text policy,TRUE is_owner`, false},
		{"empty", `SELECT 'a'::text id,''::text policy,TRUE is_owner`, false},
		{"both-allow", `SELECT * FROM (VALUES ('a'::text,'allow'::text,TRUE),('b','allow',TRUE)) v(id,policy,is_owner)`, true},
		{"allow-and-null", `SELECT * FROM (VALUES ('a'::text,'allow'::text,TRUE),('b',NULL,TRUE)) v(id,policy,is_owner)`, false},
		{"allow-and-deny", `SELECT * FROM (VALUES ('a'::text,'allow'::text,TRUE),('b','deny',TRUE)) v(id,policy,is_owner)`, false},
		{"non-owner-allow", `SELECT 'a'::text id,'allow'::text policy,FALSE is_owner`, false},
		{"null-owner-flag", `SELECT 'a'::text id,'allow'::text policy,NULL::boolean is_owner`, false},
	}
	for _, override := range states {
		for _, basis := range states {
			for _, detected := range []bool{false, true} {
				for _, owner := range owners {
					name := fmt.Sprintf("override-%s/basis-%s/detected-%t/%s", override.name, basis.name, detected, owner.name)
					t.Run(name, func(t *testing.T) {
						cte := fmt.Sprintf(`WITH streams AS (
    SELECT 'abc'::text id,'t'::text title,NOW() stream_date,NULL::int duration_seconds,
    NULL::text thumbnail_url,NULL::jsonb holodex_data,NULL::text holodex_hash,
    NULL::jsonb comment_raw,NULL::jsonb comment_songs,NULL::timestamptz comment_songs_analyzed_at,
    NULL::jsonb chapter_raw,NULL::jsonb chapter_songs,FALSE is_processed,FALSE is_hidden,
    %s restriction_override,%s restriction_override_auto,NULL::timestamptz holodex_uploaded_at,
    FALSE holodex_upload_unknown,NULL::text availability,NULL::boolean playable_in_embed,
    NULL::timestamptz availability_checked_at,NOW() created_at,NOW() updated_at
   ), fixture_owners AS (%s), stream_stream_tags AS (
    SELECT 'abc'::text stream_id,'members_only'::text tag_id WHERE %t
    UNION ALL SELECT 'other','members_only'
   ), stream_singers AS (
    SELECT 'abc'::text stream_id,id singer_id,is_owner FROM fixture_owners
    UNION ALL SELECT 'other','decoy',TRUE
   ), singers AS (
    SELECT id,policy members_only_policy FROM fixture_owners
    UNION ALL SELECT 'decoy','allow'
   ) `, restrictionFixtureBool(override.value), restrictionFixtureBool(basis.value), owner.rows, detected)
						db := openRestrictionTestDB(t, restrictionPostgresDriver{ctes: cte}, u.String())
						stream, err := NewStreamRepository(db).FindByID("abc")
						if err != nil {
							t.Fatal(err)
						}
						if stream == nil {
							t.Fatal("陽性対照の配信が消えた")
						}
						automatic := detected && !owner.allAllow
						wantRestricted := automatic
						if override.value.Valid {
							wantRestricted = override.value.Bool
						}
						wantReview := override.value.Valid && !override.value.Bool && automatic && (!basis.value.Valid || !basis.value.Bool)
						if stream.IsRestrictedEffective != wantRestricted || stream.RestrictionNeedsReview != wantReview {
							t.Fatalf("restricted=%v review=%v want restricted=%v review=%v", stream.IsRestrictedEffective, stream.RestrictionNeedsReview, wantRestricted, wantReview)
						}
					})
				}
			}
		}
	}
}
