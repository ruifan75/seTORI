package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

type auditReviewEditor struct {
	restricted bool
	access     repository.ViewerAccess
	applied    map[string]string
}

func (e *auditReviewEditor) GetEditableFields(_ uuid.UUID, access repository.ViewerAccess) (map[string]string, string, error) {
	e.access = access
	if e.restricted && access == repository.PublicAccess {
		return nil, "", nil
	}
	return map[string]string{"start_seconds": "100", "end_seconds": "200"}, "target", nil
}
func (e *auditReviewEditor) ApplyEditableFields(_ uuid.UUID, fields map[string]string) error {
	e.applied = fields
	return nil
}

func TestSuggestionMergeUsesReviewerAccess(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		for _, privileged := range []bool{false, true} {
			e := &auditReviewEditor{restricted: restricted}
			s := &SuggestionService{editors: map[string]TargetEditor{"performance": e}}
			perms := []string{"content:edit"}
			access := repository.PublicAccess
			if privileged {
				perms = append(perms, "restricted:view")
				access = repository.RestrictedView
			}
			response, err := s.Merge(&dto.MergeSuggestionsRequest{TargetType: "performance", TargetID: uuid.New().String(), Fields: map[string]string{"end_seconds": "210"}}, &models.User{Permissions: perms})
			if e.access != access {
				t.Errorf("access=%v want %v", e.access, access)
			}
			if restricted && !privileged {
				if !errors.Is(err, ErrTargetNotFound) || response != nil || e.applied != nil {
					t.Fatalf("private merge response=%+v err=%v applied=%v", response, err, e.applied)
				}
			} else if err != nil || response == nil || e.applied["end_seconds"] != "210" || response.Applied["end_seconds"] != "210" {
				t.Fatalf("positive merge response=%+v err=%v applied=%v", response, err, e.applied)
			}
		}
	}
}
