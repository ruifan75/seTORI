package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

var (
	ErrSingerMetadataManagedByHolodex = errors.New("Holodex 登録済みチャンネルは手動編集できません")
	ErrSingerNameRequired             = errors.New("チャンネル名は必須です")
)

type SingerService struct {
	singerRepo *repository.SingerRepository
	streamRepo *repository.StreamRepository
	perfRepo   *repository.PerformanceRepository
}

func NewSingerService(
	singerRepo *repository.SingerRepository,
	streamRepo *repository.StreamRepository,
	perfRepo *repository.PerformanceRepository,
) *SingerService {
	return &SingerService{
		singerRepo: singerRepo,
		streamRepo: streamRepo,
		perfRepo:   perfRepo,
	}
}

// GetAll 取得所有演唱者
func (s *SingerService) GetAll(page, limit int, sort, dir string) (*dto.SingerListResponse, error) {
	offset := (page - 1) * limit

	singers, total, err := s.singerRepo.FindAll(limit, offset, sort, dir)
	if err != nil {
		return nil, fmt.Errorf("get singers: %w", err)
	}

	// 轉換為 DTO
	singerResponses := make([]dto.SingerResponse, len(singers))
	for i, singer := range singers {
		singerResponses[i] = s.toSingerResponse(singer)
	}

	totalPages := (total + limit - 1) / limit

	return &dto.SingerListResponse{
		Singers: singerResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// Search 搜尋演唱者
func (s *SingerService) Search(query string, limit int) ([]dto.SingerResponse, error) {
	if limit <= 0 {
		limit = 10
	}

	singers, err := s.singerRepo.Search(query, limit)
	if err != nil {
		return nil, fmt.Errorf("search singers: %w", err)
	}

	singerResponses := make([]dto.SingerResponse, len(singers))
	for i, singer := range singers {
		singerResponses[i] = s.toSingerResponse(singer)
	}

	return singerResponses, nil
}

// GetByID 取得演唱者詳情
func (s *SingerService) GetByID(id string) (*dto.SingerDetailResponse, error) {
	singer, err := s.singerRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if singer == nil {
		return nil, nil
	}

	// 取得統計資料
	streamCount, _ := s.singerRepo.GetStreamCount(id)
	performanceCount, _ := s.singerRepo.GetPerformanceCount(id)

	singerResp := s.toSingerResponse(*singer)

	return &dto.SingerDetailResponse{
		SingerResponse:   singerResp,
		StreamCount:      streamCount,
		PerformanceCount: performanceCount,
	}, nil
}

// UpdateManualMetadata updates metadata for channels that are not managed by Holodex.
func (s *SingerService) UpdateManualMetadata(id string, req *dto.UpdateSingerRequest) (*dto.SingerResponse, error) {
	singer, err := s.singerRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if singer == nil {
		return nil, nil
	}
	if singer.MetadataSource == "" {
		singer.MetadataSource = "holodex"
	}
	if singer.MetadataSource == "holodex" {
		return nil, ErrSingerMetadataManagedByHolodex
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrSingerNameRequired
	}

	singer.Name = name
	singer.EnglishName = nullableTrimmedString(req.EnglishName)
	singer.PhotoURL = nullableTrimmedString(req.PhotoURL)
	singer.Organization = nullableTrimmedString(req.Organization)

	if err := s.singerRepo.UpdateManualMetadata(singer); err != nil {
		return nil, fmt.Errorf("update singer metadata: %w", err)
	}

	resp := s.toSingerResponse(*singer)
	return &resp, nil
}

// GetStreams 取得演唱者參與的歌回（支援篩選）
func (s *SingerService) GetStreams(singerID string, page, limit int, processedFilter, hiddenFilter *bool) (*dto.StreamListResponse, error) {
	offset := (page - 1) * limit

	// 建構篩選條件
	filter := &repository.StreamFilter{
		ProcessedOnly: processedFilter,
		HiddenFilter:  hiddenFilter,
	}

	streams, total, err := s.streamRepo.FindBySingerID(singerID, limit, offset, filter)
	if err != nil {
		return nil, fmt.Errorf("get streams: %w", err)
	}

	// 轉換為 DTO
	streamResponses := make([]dto.StreamResponse, len(streams))
	for i, stream := range streams {
		tags, _ := s.streamRepo.GetTags(stream.ID)
		participants, _ := s.streamRepo.GetSingers(stream.ID)
		streamResponses[i] = s.toStreamResponse(stream, tags, participants)
	}

	totalPages := (total + limit - 1) / limit

	return &dto.StreamListResponse{
		Streams: streamResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// GetPerformances 取得演唱者的所有演出記錄
func (s *SingerService) GetPerformances(singerID string, page, limit int, sort, dir string) (*dto.SingerPerformanceListResponse, error) {
	offset := (page - 1) * limit

	// 先取得演唱者資訊
	singer, err := s.singerRepo.FindByID(singerID)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if singer == nil {
		return nil, nil
	}

	performances, total, err := s.perfRepo.FindBySingerID(singerID, limit, offset, sort, dir)
	if err != nil {
		return nil, fmt.Errorf("get performances: %w", err)
	}

	// 轉換為 DTO
	perfResponses := make([]dto.SongPerformanceResponse, len(performances))
	for i, perf := range performances {
		perfResponses[i] = s.toPerformanceResponse(perf)
	}

	totalPages := (total + limit - 1) / limit

	return &dto.SingerPerformanceListResponse{
		Singer:       s.toSingerResponse(*singer),
		Performances: perfResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// toSingerResponse 轉換 Model 到 DTO
func (s *SingerService) toSingerResponse(singer models.Singer) dto.SingerResponse {
	resp := dto.SingerResponse{
		ID:              singer.ID,
		Name:            singer.Name,
		MetadataSource:  singer.MetadataSource,
		CanEditMetadata: singer.MetadataSource != "holodex",
		CreatedAt:       singer.CreatedAt,
		UpdatedAt:       singer.UpdatedAt,
	}
	if resp.MetadataSource == "" {
		resp.MetadataSource = "holodex"
		resp.CanEditMetadata = false
	}

	if singer.EnglishName.Valid {
		resp.EnglishName = &singer.EnglishName.String
	}
	if singer.PhotoURL.Valid {
		resp.PhotoURL = &singer.PhotoURL.String
	}
	if singer.Organization.Valid {
		resp.Organization = &singer.Organization.String
	}

	return resp
}

func nullableTrimmedString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: trimmed, Valid: true}
}

// toStreamResponse 轉換 Model 到 DTO
func (s *SingerService) toStreamResponse(stream models.Stream, tags []models.StreamTag, participants []models.Singer) dto.StreamResponse {
	resp := dto.StreamResponse{
		ID:          stream.ID,
		Title:       stream.Title,
		StreamDate:  stream.StreamDate.Format(time.RFC3339),
		IsProcessed: stream.IsProcessed,
		IsHidden:    stream.IsHidden,
		CreatedAt:   stream.CreatedAt,
		UpdatedAt:   stream.UpdatedAt,
	}

	if stream.DurationSeconds.Valid {
		resp.DurationSeconds = &stream.DurationSeconds.Int32
	}
	if stream.ThumbnailURL.Valid {
		resp.ThumbnailURL = &stream.ThumbnailURL.String
	}

	// 轉換標籤
	resp.Tags = make([]dto.StreamTagResponse, len(tags))
	for i, tag := range tags {
		resp.Tags[i] = dto.StreamTagResponse{
			ID:          tag.ID,
			DisplayName: tag.DisplayName,
			Color:       tag.Color,
		}
	}

	// 轉換參與者
	resp.Participants = make([]dto.SingerResponse, len(participants))
	for i, singer := range participants {
		resp.Participants[i] = s.toSingerResponse(singer)
	}

	return resp
}

// toPerformanceResponse 轉換演出到 DTO
func (s *SingerService) toPerformanceResponse(perf repository.PerformanceWithDetails) dto.SongPerformanceResponse {
	resp := dto.SongPerformanceResponse{
		ID:             perf.ID,
		StreamID:       perf.StreamID,
		StreamTitle:    perf.StreamTitle,
		StreamDate:     perf.StreamDate,
		StartSeconds:   perf.StartSeconds,
		EndSeconds:     perf.EndSeconds,
		YouTubeURL:     fmt.Sprintf("https://www.youtube.com/watch?v=%s&t=%d", perf.StreamID, perf.StartSeconds),
		CreatedAt:      perf.CreatedAt,
		SongName:       perf.SongName,
		SongID:         perf.SongID,
		OriginalArtist: perf.OriginalArtist,
		Artists:        toArtistReferences(perf.Artists),
	}

	if perf.ThumbnailURL.Valid {
		resp.ThumbnailURL = &perf.ThumbnailURL.String
	}

	// 轉換標籤
	resp.Tags = make([]dto.PerformanceTagResponse, len(perf.Tags))
	for i, tag := range perf.Tags {
		resp.Tags[i] = dto.PerformanceTagResponse{
			ID:          tag.ID,
			DisplayName: tag.DisplayName,
			Color:       tag.Color,
		}
	}

	// Custom tags
	if len(perf.CustomTags) > 0 {
		resp.CustomTags = []string(perf.CustomTags)
	} else {
		resp.CustomTags = []string{}
	}

	// 轉換演唱者
	resp.Singers = make([]dto.SingerResponse, len(perf.Singers))
	for i, singer := range perf.Singers {
		resp.Singers[i] = s.toSingerResponse(singer)
	}

	return resp
}
