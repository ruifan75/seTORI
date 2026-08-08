package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

var (
	ErrOrganizationKeyRequired  = errors.New("事務所のキーは必須です")
	ErrOrganizationNameRequired = errors.New("表示名は必須です")
	ErrOrganizationExists       = errors.New("同じキーの事務所が既にあります")
	ErrOrganizationInUse        = errors.New("所属しているチャンネルがあるため削除できません")
)

type OrganizationService struct {
	orgRepo *repository.OrganizationRepository
}

func NewOrganizationService(orgRepo *repository.OrganizationRepository) *OrganizationService {
	return &OrganizationService{orgRepo: orgRepo}
}

// GetAll は事務所を並び順で返す。所属チャンネル数も併せて返す
// （管理画面で「消してよいか」「表示名を直すべきか」の判断材料になるため）。
func (s *OrganizationService) GetAll() (*dto.OrganizationListResponse, error) {
	orgs, err := s.orgRepo.FindAll()
	if err != nil {
		return nil, fmt.Errorf("get organizations: %w", err)
	}
	counts, err := s.orgRepo.CountSingers()
	if err != nil {
		return nil, fmt.Errorf("count singers: %w", err)
	}

	resp := make([]dto.OrganizationResponse, len(orgs))
	for i, o := range orgs {
		resp[i] = toOrganizationResponse(o, counts[o.Key])
	}
	return &dto.OrganizationListResponse{Organizations: resp}, nil
}

// Create は事務所を手で追加する。key を省略すると表示名をそのまま key にする
// （Holodex に無い事務所は突き合わせ相手がいないので、key を別に考える意味がない）。
func (s *OrganizationService) Create(req *dto.CreateOrganizationRequest) (*dto.OrganizationResponse, error) {
	display := strings.TrimSpace(req.DisplayName)
	if display == "" {
		return nil, ErrOrganizationNameRequired
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		key = display
	}

	org := &models.Organization{Key: key, DisplayName: display, SortOrder: req.SortOrder}
	created, err := s.orgRepo.Create(org)
	if err != nil {
		return nil, fmt.Errorf("create organization: %w", err)
	}
	if !created {
		return nil, ErrOrganizationExists
	}

	resp := toOrganizationResponse(*org, 0)
	return &resp, nil
}

// Update は表示名と並び順を更新する。見つからなければ (nil, nil)。
func (s *OrganizationService) Update(key string, req *dto.UpdateOrganizationRequest) (*dto.OrganizationResponse, error) {
	display := strings.TrimSpace(req.DisplayName)
	if display == "" {
		return nil, ErrOrganizationNameRequired
	}

	updated, err := s.orgRepo.Update(&models.Organization{
		Key: key, DisplayName: display, SortOrder: req.SortOrder,
	})
	if err != nil {
		return nil, fmt.Errorf("update organization: %w", err)
	}
	if updated == nil {
		return nil, nil
	}

	counts, _ := s.orgRepo.CountSingers()
	resp := toOrganizationResponse(*updated, counts[updated.Key])
	return &resp, nil
}

// Delete は事務所を消す。所属チャンネルが残っていれば ErrOrganizationInUse。
// 見つからなければ (false, nil)。
func (s *OrganizationService) Delete(key string) (bool, error) {
	deleted, inUse, err := s.orgRepo.Delete(key)
	if err != nil {
		return false, fmt.Errorf("delete organization: %w", err)
	}
	if inUse {
		return false, ErrOrganizationInUse
	}
	return deleted, nil
}

// EnsureExists は取り込み経路から呼ぶ。未知の事務所を display_name = key で作る。
func (s *OrganizationService) EnsureExists(key string) error {
	return s.orgRepo.EnsureExists(key)
}

func toOrganizationResponse(o models.Organization, singerCount int) dto.OrganizationResponse {
	return dto.OrganizationResponse{
		Key:         o.Key,
		DisplayName: o.DisplayName,
		SortOrder:   o.SortOrder,
		SingerCount: singerCount,
		CreatedAt:   o.CreatedAt,
		UpdatedAt:   o.UpdatedAt,
	}
}
