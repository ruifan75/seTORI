package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

// TargetEditor は修正提案の対象（曲・アーティスト）が満たすインターフェース。
// SongService / ArtistService が実装する。
type TargetEditor interface {
	GetEditableFields(id uuid.UUID) (map[string]string, string, error)
	ApplyEditableFields(id uuid.UUID, fields map[string]string) error
}

var (
	ErrInvalidTarget      = fmt.Errorf("対象の種類が不正です")
	ErrTargetNotFound     = fmt.Errorf("対象が見つかりません")
	ErrNoChange           = fmt.Errorf("変更がありません")
	ErrSuggestionNotFound = fmt.Errorf("提案が見つかりません")
	ErrAlreadyReviewed    = fmt.Errorf("この提案は既に処理済みです")
)

// SuggestionService は閲覧モードからの修正提案の投稿・レビュー・反映を担う。
type SuggestionService struct {
	repo    *repository.SuggestionRepository
	editors map[string]TargetEditor
}

func NewSuggestionService(repo *repository.SuggestionRepository, songService *SongService, artistService *ArtistService) *SuggestionService {
	return &SuggestionService{
		repo: repo,
		editors: map[string]TargetEditor{
			"song":   songService,
			"artist": artistService,
		},
	}
}

// Create は修正提案を登録する。対象の現状を before、提案値を after として保存する。
// 変更が無い（全フィールドが現状と同じ）場合は ErrNoChange。
func (s *SuggestionService) Create(req *dto.CreateSuggestionRequest) (*models.EditSuggestion, error) {
	editor, ok := s.editors[req.TargetType]
	if !ok {
		return nil, ErrInvalidTarget
	}
	id, err := uuid.Parse(req.TargetID)
	if err != nil {
		return nil, ErrInvalidTarget
	}

	before, label, err := editor.GetEditableFields(id)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrTargetNotFound
	}

	// after = before に提案値を上書き（既知フィールドのみ採用）。差分の有無を確認する。
	after := make(map[string]string, len(before))
	changed := false
	for k, cur := range before {
		if v, ok := req.Fields[k]; ok {
			nv := strings.TrimSpace(v)
			after[k] = nv
			if nv != cur {
				changed = true
			}
		} else {
			after[k] = cur
		}
	}
	if !changed {
		return nil, ErrNoChange
	}

	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	sug := &models.EditSuggestion{
		TargetType:  req.TargetType,
		TargetID:    id,
		TargetLabel: label,
		BeforeData:  beforeJSON,
		AfterData:   afterJSON,
		Note:        strings.TrimSpace(req.Note),
	}
	created, err := s.repo.Create(sug)
	if err != nil {
		return nil, err
	}
	logger.Infof("edit suggestion created: %s %s (%s)", req.TargetType, id, label)
	return created, nil
}

// List は status（空なら全件）で絞った提案一覧を返す。
func (s *SuggestionService) List(status string, page, limit int) (*dto.SuggestionListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	items, total, err := s.repo.List(status, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	resp := make([]dto.SuggestionResponse, len(items))
	for i, it := range items {
		resp[i] = toSuggestionResponse(it)
	}
	return &dto.SuggestionListResponse{
		Suggestions: resp,
		Pagination: dto.PaginationResponse{
			Page: page, Limit: limit, Total: total, TotalPages: (total + limit - 1) / limit,
		},
	}, nil
}

// CountPending は未処理提案数を返す（バッジ表示用）。
func (s *SuggestionService) CountPending() (int, error) {
	return s.repo.CountPending()
}

// Approve は提案を対象へ反映し approved にする。反映に失敗した場合はステータスを変えない。
func (s *SuggestionService) Approve(id uuid.UUID) error {
	sug, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if sug == nil {
		return ErrSuggestionNotFound
	}
	if sug.Status != "pending" {
		return ErrAlreadyReviewed
	}
	editor, ok := s.editors[sug.TargetType]
	if !ok {
		return ErrInvalidTarget
	}

	var fields map[string]string
	if err := json.Unmarshal(sug.AfterData, &fields); err != nil {
		return fmt.Errorf("提案内容の解析に失敗しました: %w", err)
	}
	if err := editor.ApplyEditableFields(sug.TargetID, fields); err != nil {
		return err // 反映失敗（例：対象削除済み・名前衝突）。pending のまま残す。
	}
	if err := s.repo.UpdateStatus(id, "approved"); err != nil {
		return err
	}
	logger.Infof("edit suggestion approved: %s", id)
	return nil
}

// Reject は提案を却下する（対象は変更しない）。
func (s *SuggestionService) Reject(id uuid.UUID) error {
	sug, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if sug == nil {
		return ErrSuggestionNotFound
	}
	if sug.Status != "pending" {
		return ErrAlreadyReviewed
	}
	return s.repo.UpdateStatus(id, "rejected")
}

func toSuggestionResponse(s models.EditSuggestion) dto.SuggestionResponse {
	before := map[string]string{}
	after := map[string]string{}
	_ = json.Unmarshal(s.BeforeData, &before)
	_ = json.Unmarshal(s.AfterData, &after)
	return dto.SuggestionResponse{
		ID:          s.ID,
		TargetType:  s.TargetType,
		TargetID:    s.TargetID,
		TargetLabel: s.TargetLabel,
		Before:      before,
		After:       after,
		Note:        s.Note,
		Status:      s.Status,
		CreatedAt:   s.CreatedAt,
		ReviewedAt:  s.ReviewedAt,
	}
}
