package service

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

type SongService struct {
	songRepo       *repository.SongRepository
	perfRepo       *repository.PerformanceRepository
	songItunesRepo *repository.SongItunesRepository
}

func NewSongService(
	songRepo *repository.SongRepository,
	perfRepo *repository.PerformanceRepository,
	songItunesRepo *repository.SongItunesRepository,
) *SongService {
	return &SongService{
		songRepo:       songRepo,
		perfRepo:       perfRepo,
		songItunesRepo: songItunesRepo,
	}
}

// GetAll 取得歌曲列表
func (s *SongService) GetAll(page, limit int, search string) (*dto.SongListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	songs, total, err := s.songRepo.FindAll(limit, offset, search)
	if err != nil {
		return nil, fmt.Errorf("get songs: %w", err)
	}

	// 轉換為 DTO
	songResponses := make([]dto.SongResponse, len(songs))
	for i, song := range songs {
		count, _ := s.songRepo.GetPerformanceCount(song.ID)
		songResponses[i] = s.toSongResponse(song, count)
	}

	totalPages := (total + limit - 1) / limit

	return &dto.SongListResponse{
		Songs: songResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// GetByID 取得單首歌曲
func (s *SongService) GetByID(id uuid.UUID) (*dto.SongResponse, error) {
	song, err := s.songRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get song: %w", err)
	}
	if song == nil {
		return nil, nil
	}

	count, _ := s.songRepo.GetPerformanceCount(song.ID)
	resp := s.toSongResponse(*song, count)
	return &resp, nil
}

// GetPerformances 取得歌曲的所有演出（反向查詢）
func (s *SongService) GetPerformances(songID uuid.UUID, page, limit int) (*dto.SongPerformanceListResponse, error) {
	song, err := s.songRepo.FindByID(songID)
	if err != nil {
		return nil, fmt.Errorf("get song: %w", err)
	}
	if song == nil {
		return nil, nil
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	performances, total, err := s.perfRepo.FindBySongID(songID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("get performances: %w", err)
	}

	// 轉換為 DTO
	perfResponses := make([]dto.SongPerformanceResponse, len(performances))
	for i, perf := range performances {
		perfResponses[i] = s.toSongPerformanceResponse(perf)
	}

	count, _ := s.songRepo.GetPerformanceCount(songID)
	songResp := s.toSongResponse(*song, count)
	totalPages := (total + limit - 1) / limit

	return &dto.SongPerformanceListResponse{
		Song:         songResp,
		Performances: perfResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// Create 建立新歌曲
func (s *SongService) Create(req *dto.CreateSongRequest) (*dto.SongResponse, error) {
	// 檢查是否已存在
	existing, err := s.songRepo.FindByNameAndArtist(req.Name, req.OriginalArtist)
	if err != nil {
		return nil, fmt.Errorf("check existing song: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("song already exists")
	}

	song := &models.Song{
		Name:           req.Name,
		OriginalArtist: req.OriginalArtist,
	}

	if req.NameReading != nil {
		song.NameReading = sql.NullString{String: *req.NameReading, Valid: true}
	}
	if req.OriginalArtistReading != nil {
		song.OriginalArtistReading = sql.NullString{String: *req.OriginalArtistReading, Valid: true}
	}
	if req.Arts != nil {
		song.Arts = sql.NullString{String: *req.Arts, Valid: true}
	}

	err = s.songRepo.Create(song)
	if err != nil {
		return nil, fmt.Errorf("create song: %w", err)
	}

	resp := s.toSongResponse(*song, 0)
	return &resp, nil
}

// Update 更新歌曲
func (s *SongService) Update(id uuid.UUID, req *dto.UpdateSongRequest) (*dto.SongResponse, error) {
	song, err := s.songRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get song: %w", err)
	}
	if song == nil {
		return nil, nil
	}

	song.Name = req.Name
	song.OriginalArtist = req.OriginalArtist

	if req.NameReading != nil {
		song.NameReading = sql.NullString{String: *req.NameReading, Valid: true}
	} else {
		song.NameReading = sql.NullString{Valid: false}
	}
	if req.OriginalArtistReading != nil {
		song.OriginalArtistReading = sql.NullString{String: *req.OriginalArtistReading, Valid: true}
	} else {
		song.OriginalArtistReading = sql.NullString{Valid: false}
	}
	if req.Arts != nil {
		song.Arts = sql.NullString{String: *req.Arts, Valid: true}
	} else {
		song.Arts = sql.NullString{Valid: false}
	}

	err = s.songRepo.Update(song)
	if err != nil {
		return nil, fmt.Errorf("update song: %w", err)
	}

	count, _ := s.songRepo.GetPerformanceCount(song.ID)
	resp := s.toSongResponse(*song, count)
	return &resp, nil
}

// Delete 刪除歌曲
func (s *SongService) Delete(id uuid.UUID) error {
	return s.songRepo.Delete(id)
}

// SearchSimilar 搜尋相似歌曲（用於 AI 正規化建議）
func (s *SongService) SearchSimilar(name string, limit int) ([]dto.SongResponse, error) {
	songs, err := s.songRepo.SearchSimilar(name, limit)
	if err != nil {
		return nil, fmt.Errorf("search similar: %w", err)
	}

	responses := make([]dto.SongResponse, len(songs))
	for i, song := range songs {
		count, _ := s.songRepo.GetPerformanceCount(song.ID)
		responses[i] = s.toSongResponse(song, count)
	}
	return responses, nil
}

// toSongResponse 轉換 Model 到 DTO
func (s *SongService) toSongResponse(song models.Song, count int) dto.SongResponse {
	resp := dto.SongResponse{
		ID:               song.ID,
		Name:             song.Name,
		OriginalArtist:   song.OriginalArtist,
		PerformanceCount: count,
		CreatedAt:        song.CreatedAt,
		UpdatedAt:        song.UpdatedAt,
	}

	if song.NameReading.Valid {
		resp.NameReading = &song.NameReading.String
	}
	if song.OriginalArtistReading.Valid {
		resp.OriginalArtistReading = &song.OriginalArtistReading.String
	}
	if song.Arts.Valid {
		resp.Arts = &song.Arts.String
	}

	// 取得 iTunes IDs
	if s.songItunesRepo != nil {
		itunesRecords, _ := s.songItunesRepo.FindBySongID(song.ID)
		if len(itunesRecords) > 0 {
			resp.ItunesIDs = make([]dto.SongItunesResponse, len(itunesRecords))
			for i, rec := range itunesRecords {
				resp.ItunesIDs[i] = dto.SongItunesResponse{
					ItunesID:  rec.ITunesID,
					IsPrimary: rec.IsPrimary,
				}
				if rec.CollectionName.Valid {
					resp.ItunesIDs[i].CollectionName = &rec.CollectionName.String
				}
				if rec.Country.Valid {
					resp.ItunesIDs[i].Country = &rec.Country.String
				}
			}
		}
	}

	return resp
}

// toSongPerformanceResponse 轉換演出到 DTO
func (s *SongService) toSongPerformanceResponse(perf repository.PerformanceWithDetails) dto.SongPerformanceResponse {
	resp := dto.SongPerformanceResponse{
		ID:           perf.ID,
		StreamID:     perf.StreamID,
		StreamTitle:  perf.StreamTitle,
		StreamDate:   perf.StreamDate,
		StartSeconds: perf.StartSeconds,
		EndSeconds:   perf.EndSeconds,
		YouTubeURL:   fmt.Sprintf("https://www.youtube.com/watch?v=%s&t=%d", perf.StreamID, perf.StartSeconds),
		CreatedAt:    perf.CreatedAt,
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

	// 轉換演唱者
	resp.Singers = make([]dto.SingerResponse, len(perf.Singers))
	for i, singer := range perf.Singers {
		resp.Singers[i] = dto.SingerResponse{
			ID:        singer.ID,
			Name:      singer.Name,
			CreatedAt: singer.CreatedAt,
			UpdatedAt: singer.UpdatedAt,
		}
		if singer.EnglishName.Valid {
			resp.Singers[i].EnglishName = &singer.EnglishName.String
		}
		if singer.PhotoURL.Valid {
			resp.Singers[i].PhotoURL = &singer.PhotoURL.String
		}
		if singer.Organization.Valid {
			resp.Singers[i].Organization = &singer.Organization.String
		}
	}

	return resp
}
