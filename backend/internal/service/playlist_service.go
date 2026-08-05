package service

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

var (
	ErrPlaylistNotFound   = fmt.Errorf("プレイリストが見つかりません")
	ErrPlaylistForbidden  = fmt.Errorf("このプレイリストを編集する権限がありません")
	ErrPlaylistName       = fmt.Errorf("プレイリスト名を入力してください")
	ErrPlaylistVisibility = fmt.Errorf("公開範囲の指定が不正です")
	ErrPerformanceInvalid = fmt.Errorf("歌唱記録の指定が不正です")
)

const maxPlaylistNameLen = 200

// PlaylistService はプレイリストの操作と公開範囲の判定を担う。
//
// 認可について：既存の requiredPermission はパス単位の権限鍵しか表現できないため、
// 「所有者本人か、公開されているか」という行単位の判定はここで行う。
// 参照できない場合は一律 ErrPlaylistNotFound を返す（存在の有無を漏らさないため、
// 他人の private/unlisted は「権限がない」ではなく「無い」として扱う）。
type PlaylistService struct {
	repo *repository.PlaylistRepository
}

func NewPlaylistService(repo *repository.PlaylistRepository) *PlaylistService {
	return &PlaylistService{repo: repo}
}

func normalizeVisibility(v string) (string, error) {
	switch v {
	case "":
		return models.PlaylistPrivate, nil // 既定は非公開（意図せず公開しない）
	case models.PlaylistPrivate, models.PlaylistUnlisted, models.PlaylistPublic:
		return v, nil
	default:
		return "", ErrPlaylistVisibility
	}
}

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrPlaylistName
	}
	if len([]rune(name)) > maxPlaylistNameLen {
		return "", fmt.Errorf("プレイリスト名は %d 文字以内にしてください", maxPlaylistNameLen)
	}
	return name, nil
}

// toResponse は所有者かどうかで share_slug の露出を切り替えて DTO 化する。
func toPlaylistResponse(p *repository.PlaylistWithMeta, viewerID *uuid.UUID) dto.PlaylistResponse {
	isOwner := viewerID != nil && *viewerID == p.UserID
	resp := dto.PlaylistResponse{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Visibility:  p.Visibility,
		ItemCount:   p.ItemCount,
		OwnerName:   p.OwnerName,
		IsOwner:     isOwner,
		CreatedAt:   p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   p.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if isOwner {
		resp.ShareSlug = p.ShareSlug
	}
	return resp
}

// canView は閲覧可否を返す。公開なら誰でも、そうでなければ所有者のみ。
// unlisted は ID 経由では所有者にしか見せない（共有 URL からは GetByShareSlug で入る）。
func canView(p *models.Playlist, viewerID *uuid.UUID) bool {
	if p.Visibility == models.PlaylistPublic {
		return true
	}
	return viewerID != nil && *viewerID == p.UserID
}

func (s *PlaylistService) Create(userID uuid.UUID, req *dto.CreatePlaylistRequest) (*dto.PlaylistResponse, error) {
	name, err := validateName(req.Name)
	if err != nil {
		return nil, err
	}
	visibility, err := normalizeVisibility(req.Visibility)
	if err != nil {
		return nil, err
	}

	p := &models.Playlist{
		UserID:      userID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Visibility:  visibility,
	}
	if err := s.repo.Create(p); err != nil {
		return nil, err
	}

	meta, err := s.repo.FindByIDWithMeta(p.ID)
	if err != nil {
		return nil, err
	}
	resp := toPlaylistResponse(meta, &userID)
	return &resp, nil
}

// ListMine は本人の全プレイリスト（private 含む）を返す。
func (s *PlaylistService) ListMine(userID uuid.UUID) (*dto.PlaylistListResponse, error) {
	items, err := s.repo.ListByUser(userID)
	if err != nil {
		return nil, err
	}
	resp := &dto.PlaylistListResponse{
		Playlists: make([]dto.PlaylistResponse, len(items)),
		Total:     len(items),
	}
	for i := range items {
		resp.Playlists[i] = toPlaylistResponse(&items[i], &userID)
	}
	return resp, nil
}

// ListPublic は公開プレイリストの一覧。unlisted は含めない。
func (s *PlaylistService) ListPublic(limit, offset int, viewerID *uuid.UUID) (*dto.PlaylistListResponse, error) {
	items, total, err := s.repo.ListPublic(limit, offset)
	if err != nil {
		return nil, err
	}
	resp := &dto.PlaylistListResponse{
		Playlists: make([]dto.PlaylistResponse, len(items)),
		Total:     total,
	}
	for i := range items {
		resp.Playlists[i] = toPlaylistResponse(&items[i], viewerID)
	}
	return resp, nil
}

// Get は ID で取得する。閲覧できない場合は ErrPlaylistNotFound。
func (s *PlaylistService) Get(id uuid.UUID, viewerID *uuid.UUID) (*dto.PlaylistResponse, error) {
	meta, err := s.repo.FindByIDWithMeta(id)
	if err != nil {
		return nil, err
	}
	if meta == nil || !canView(&meta.Playlist, viewerID) {
		return nil, ErrPlaylistNotFound
	}
	resp := toPlaylistResponse(meta, viewerID)
	return &resp, nil
}

// GetByShareSlug は限定公開 URL からの取得。private のものは開かない。
func (s *PlaylistService) GetByShareSlug(slug string, viewerID *uuid.UUID) (*dto.PlaylistResponse, error) {
	meta, err := s.repo.FindByShareSlug(slug)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, ErrPlaylistNotFound
	}
	isOwner := viewerID != nil && *viewerID == meta.UserID
	if meta.Visibility == models.PlaylistPrivate && !isOwner {
		return nil, ErrPlaylistNotFound
	}
	resp := toPlaylistResponse(meta, viewerID)
	return &resp, nil
}

// requireOwner は編集操作の前に所有者であることを確かめる。
// 他人のものは存在を伏せるため ErrPlaylistNotFound を返す。
func (s *PlaylistService) requireOwner(id uuid.UUID, userID uuid.UUID) (*models.Playlist, error) {
	p, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrPlaylistNotFound
	}
	if p.UserID != userID {
		return nil, ErrPlaylistNotFound
	}
	return p, nil
}

func (s *PlaylistService) Update(id, userID uuid.UUID, req *dto.UpdatePlaylistRequest) (*dto.PlaylistResponse, error) {
	p, err := s.requireOwner(id, userID)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		name, err := validateName(*req.Name)
		if err != nil {
			return nil, err
		}
		p.Name = name
	}
	if req.Description != nil {
		p.Description = strings.TrimSpace(*req.Description)
	}
	if req.Visibility != nil {
		v, err := normalizeVisibility(*req.Visibility)
		if err != nil {
			return nil, err
		}
		p.Visibility = v
	}

	if err := s.repo.Update(p); err != nil {
		return nil, err
	}
	meta, err := s.repo.FindByIDWithMeta(id)
	if err != nil {
		return nil, err
	}
	resp := toPlaylistResponse(meta, &userID)
	return &resp, nil
}

func (s *PlaylistService) Delete(id, userID uuid.UUID) error {
	if _, err := s.requireOwner(id, userID); err != nil {
		return err
	}
	return s.repo.Delete(id)
}

// ========== 項目 ==========

// ListItems は並び順どおりの歌唱を返す。閲覧可否は Get と同じ判定。
func (s *PlaylistService) ListItems(id uuid.UUID, viewerID *uuid.UUID) ([]repository.PerformanceWithDetails, error) {
	p, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if p == nil || !canView(p, viewerID) {
		return nil, ErrPlaylistNotFound
	}
	return s.repo.ListItems(id)
}

// ListItemsByShareSlug は限定公開 URL からの項目取得。
func (s *PlaylistService) ListItemsByShareSlug(slug string, viewerID *uuid.UUID) ([]repository.PerformanceWithDetails, error) {
	meta, err := s.repo.FindByShareSlug(slug)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, ErrPlaylistNotFound
	}
	isOwner := viewerID != nil && *viewerID == meta.UserID
	if meta.Visibility == models.PlaylistPrivate && !isOwner {
		return nil, ErrPlaylistNotFound
	}
	return s.repo.ListItems(meta.ID)
}

func (s *PlaylistService) AddItem(id, userID uuid.UUID, performanceIDStr string) error {
	if _, err := s.requireOwner(id, userID); err != nil {
		return err
	}
	performanceID, err := uuid.Parse(performanceIDStr)
	if err != nil {
		return ErrPerformanceInvalid
	}
	return s.repo.AddItem(id, performanceID)
}

func (s *PlaylistService) RemoveItem(id, userID uuid.UUID, performanceIDStr string) error {
	if _, err := s.requireOwner(id, userID); err != nil {
		return err
	}
	performanceID, err := uuid.Parse(performanceIDStr)
	if err != nil {
		return ErrPerformanceInvalid
	}
	return s.repo.RemoveItem(id, performanceID)
}

func (s *PlaylistService) Reorder(id, userID uuid.UUID, performanceIDStrs []string) error {
	if _, err := s.requireOwner(id, userID); err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0, len(performanceIDStrs))
	for _, str := range performanceIDStrs {
		pid, err := uuid.Parse(str)
		if err != nil {
			return ErrPerformanceInvalid
		}
		ids = append(ids, pid)
	}
	return s.repo.Reorder(id, ids)
}
