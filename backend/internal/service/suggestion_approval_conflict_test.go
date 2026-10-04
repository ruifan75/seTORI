package service

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

// 初回の提案参照だけで止めない。初回参照後に対象が秘匿へ変わったケースと
// 公開・閲覧権限ありの衝突を、実際の承認から通して現在値のアクセスを検査する。
func TestSuggestionApprovalConflictsUseReviewerAccess(t *testing.T) {
	publicQuery, err := os.ReadFile("testdata/suggestion_merge_view.sql")
	if err != nil {
		t.Fatal(err)
	}
	const privilegedQuery = `SELECT id, target_type, target_id, target_key, target_label, kind,
 before_data, after_data, payload, note, status,
 created_by, created_by_name, client_hint, reviewed_by, review_note,
 created_at, reviewed_at FROM edit_suggestions WHERE id = $1`
	for _, kind := range []string{KindField, KindSongSwap} {
		for _, tc := range []struct {
			name        string
			restricted  bool
			permissions []string
			access      repository.ViewerAccess
		}{
			{name: "参照後に秘匿になった対象", restricted: true, permissions: []string{"content:edit"}, access: repository.PublicAccess},
			{name: "公開の衝突", permissions: []string{"content:edit"}, access: repository.PublicAccess},
			{name: "restricted:view の衝突", restricted: true, permissions: []string{"content:edit", "restricted:view"}, access: repository.RestrictedView},
			{name: "管理者の衝突", restricted: true, permissions: []string{"*"}, access: repository.RestrictedView},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				id := uuid.MustParse("527b14c9-f292-4e20-836c-9c41c0ae944d")
				target := uuid.MustParse("212d7d97-7a99-4d87-bcf2-6c210bd9e400")
				e := &auditReviewEditor{restricted: tc.restricted}
				swapper := &auditConflictSwapper{restricted: tc.restricted}
				query := string(publicQuery)
				if tc.access == repository.RestrictedView {
					query = privilegedQuery
				}
				key, before, current := "end_seconds", "190", "200"
				if kind == KindSongSwap {
					key, before, current = "song", "old-song", "current-song"
				}
				c := &mergeAuditConnector{t: t, query: query, id: id.String(), editor: e, allowUnapplied: true,
					status: "conflict", note: fmt.Sprintf("提案の作成後に対象が変更されています（%s）", key),
					rows: [][]driver.Value{{id.String(), "performance", target.String(), "", "target", kind,
						[]byte(`{"end_seconds":"190"}`), []byte(`{"end_seconds":"210"}`),
						[]byte(`{"song_name":"proposed-song","current_song_name":"old-song"}`), "", "pending",
						nil, "", "", nil, "", time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), nil}}}
				db := sql.OpenDB(c)
				denied := tc.restricted && tc.access == repository.PublicAccess
				wantWrites := 1
				if denied {
					wantWrites = 0
				}
				t.Cleanup(func() {
					db.Close()
					if c.queries != 1 || c.writes != wantWrites {
						t.Errorf("queries/writes=%d/%d want 1/%d", c.queries, c.writes, wantWrites)
					}
				})
				s := &SuggestionService{repo: repository.NewSuggestionRepository(db), editors: map[string]TargetEditor{"performance": e}, swapper: swapper}
				err := s.ApproveWithEdits(id, &models.User{Permissions: tc.permissions}, false, nil)
				access := e.access
				if kind == KindSongSwap {
					access = swapper.access
				}
				if access != tc.access || e.applied != nil || swapper.applied {
					t.Fatalf("access=%v want %v applied=%v swap=%t", access, tc.access, e.applied, swapper.applied)
				}
				if denied {
					if !errors.Is(err, ErrTargetNotFound) {
						t.Fatalf("private target leaked into conflict: %#v", err)
					}
				} else {
					var conflict *ConflictError
					if !errors.As(err, &conflict) || !reflect.DeepEqual(conflict.Fields, map[string]FieldConflict{key: {Expected: before, Current: current}}) {
						t.Fatalf("positive conflict=%#v", err)
					}
				}
			})
		}
	}
}

type auditConflictSwapper struct {
	restricted, applied bool
	access              repository.ViewerAccess
}

func (s *auditConflictSwapper) SongLabelOf(_ uuid.UUID, access repository.ViewerAccess) (string, string, error) {
	s.access = access
	if s.restricted && access == repository.PublicAccess {
		return "", "", nil
	}
	return "current-song", "target", nil
}
func (s *auditConflictSwapper) ApplySongSwap(uuid.UUID, dto.SongSwapPayload) error {
	s.applied = true
	return nil
}
