package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

type StreamService struct {
	streamRepo *repository.StreamRepository
	perfRepo   *repository.PerformanceRepository
}

func NewStreamService(streamRepo *repository.StreamRepository, perfRepo *repository.PerformanceRepository) *StreamService {
	return &StreamService{
		streamRepo: streamRepo,
		perfRepo:   perfRepo,
	}
}

// GetAll 取得歌回列表（預設不顯示隱藏的）
func (s *StreamService) GetAll(page, limit int) (*dto.StreamListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	// 預設不顯示隱藏的歌回
	streams, total, err := s.streamRepo.FindAll(limit, offset, false)
	if err != nil {
		return nil, fmt.Errorf("get streams: %w", err)
	}

	// 轉換為 DTO
	streamResponses := make([]dto.StreamResponse, len(streams))
	for i, stream := range streams {
		tags, _ := s.streamRepo.GetTags(stream.ID)
		participants, _ := s.streamRepo.GetSingers(stream.ID)
		channelOwner, _ := s.streamRepo.GetChannelOwner(stream.ID)
		streamResponses[i] = s.toStreamResponse(stream, tags, participants, channelOwner)
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

// GetByID 取得歌回詳情（含歌單）
func (s *StreamService) GetByID(id string) (*dto.StreamDetailResponse, error) {
	stream, err := s.streamRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get stream: %w", err)
	}
	if stream == nil {
		return nil, nil
	}

	tags, _ := s.streamRepo.GetTags(stream.ID)
	participants, _ := s.streamRepo.GetSingers(stream.ID)
	channelOwner, _ := s.streamRepo.GetChannelOwner(stream.ID)
	streamResp := s.toStreamResponse(*stream, tags, participants, channelOwner)

	// 取得演出清單
	performances, err := s.perfRepo.FindByStreamID(id)
	if err != nil {
		return nil, fmt.Errorf("get performances: %w", err)
	}

	perfResponses := make([]dto.PerformanceResponse, len(performances))
	for i, perf := range performances {
		perfResponses[i] = s.toPerformanceResponse(perf)
	}

	return &dto.StreamDetailResponse{
		StreamResponse: streamResp,
		Performances:   perfResponses,
	}, nil
}

// toStreamResponse 轉換 Model 到 DTO
func (s *StreamService) toStreamResponse(stream models.Stream, tags []models.StreamTag, participants []models.Singer, channelOwner *models.Singer) dto.StreamResponse {
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
		d := stream.DurationSeconds.Int32
		resp.DurationSeconds = &d
	}
	if stream.ThumbnailURL.Valid {
		resp.ThumbnailURL = &stream.ThumbnailURL.String
	}

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
		resp.Participants[i] = dto.SingerResponse{
			ID:        singer.ID,
			Name:      singer.Name,
			CreatedAt: singer.CreatedAt,
			UpdatedAt: singer.UpdatedAt,
		}
		if singer.EnglishName.Valid {
			resp.Participants[i].EnglishName = &singer.EnglishName.String
		}
		if singer.PhotoURL.Valid {
			resp.Participants[i].PhotoURL = &singer.PhotoURL.String
		}
		if singer.Organization.Valid {
			resp.Participants[i].Organization = &singer.Organization.String
		}
	}

	// 轉換頻道擁有者
	if channelOwner != nil {
		ownerResp := dto.SingerResponse{
			ID:        channelOwner.ID,
			Name:      channelOwner.Name,
			CreatedAt: channelOwner.CreatedAt,
			UpdatedAt: channelOwner.UpdatedAt,
		}
		if channelOwner.EnglishName.Valid {
			ownerResp.EnglishName = &channelOwner.EnglishName.String
		}
		if channelOwner.PhotoURL.Valid {
			ownerResp.PhotoURL = &channelOwner.PhotoURL.String
		}
		if channelOwner.Organization.Valid {
			ownerResp.Organization = &channelOwner.Organization.String
		}
		resp.ChannelOwner = &ownerResp
	}

	// 解析並加入 Holodex timeline 資料（從完整的 Video JSON 中提取 songs）
	if len(stream.HolodexData) > 0 {
		// HolodexData 存的是完整的 holodex.Video 物件
		var video struct {
			Songs []struct {
				Name           string `json:"name"`
				OriginalArtist string `json:"original_artist"`
				ArtURL         string `json:"art"`
				ITunesID       int64  `json:"itunesid"`
				Start          int    `json:"start"`
				End            int    `json:"end"`
			} `json:"songs"`
		}
		if err := json.Unmarshal(stream.HolodexData, &video); err == nil && len(video.Songs) > 0 {
			holodexSongs := make([]dto.SongSuggestion, len(video.Songs))
			for i, song := range video.Songs {
				holodexSongs[i] = dto.SongSuggestion{
					Name:           song.Name,
					OriginalArtist: song.OriginalArtist,
					StartSeconds:   song.Start,
					EndSeconds:     song.End,
					Tags:           []string{},
					SingerIDs:      []string{},
				}
				if song.ArtURL != "" {
					holodexSongs[i].ArtURL = &song.ArtURL
				}
				if song.ITunesID > 0 {
					itunesID := song.ITunesID
					holodexSongs[i].ItunesID = &itunesID
				}
				// 如果沒有結束時間，使用下一首的開始時間
				if holodexSongs[i].EndSeconds == 0 && i+1 < len(video.Songs) {
					holodexSongs[i].EndSeconds = video.Songs[i+1].Start
				}
			}
			resp.HolodexTimelineSongs = holodexSongs
		}
	}

	// 從 comment_songs 載入解析結果（未去重）
	if len(stream.CommentSongs) > 0 {
		var commentSongs []dto.CommentSong
		if err := json.Unmarshal(stream.CommentSongs, &commentSongs); err == nil && len(commentSongs) > 0 {
			resp.CommentTimelineSongs = commentSongs
		}
	}

	return resp
}

// toPerformanceResponse 轉換演出到 DTO
func (s *StreamService) toPerformanceResponse(perf repository.PerformanceWithDetails) dto.PerformanceResponse {
	resp := dto.PerformanceResponse{
		ID:             perf.ID,
		StreamID:       perf.StreamID,
		SongID:         perf.SongID,
		SongName:       perf.SongName,
		OriginalArtist: perf.OriginalArtist,
		StartSeconds:   perf.StartSeconds,
		EndSeconds:     perf.EndSeconds,
		OrderIndex:     perf.OrderIndex,
		YouTubeURL:     fmt.Sprintf("https://www.youtube.com/watch?v=%s&t=%d", perf.StreamID, perf.StartSeconds),
		CreatedAt:      perf.CreatedAt,
	}

	if perf.Arts.Valid {
		resp.Arts = &perf.Arts.String
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

// Update 更新歌回資訊
func (s *StreamService) Update(id string, req *dto.UpdateStreamRequest) (*dto.StreamDetailResponse, error) {
	stream, err := s.streamRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, nil
	}

	// 更新標題
	if req.Title != nil && *req.Title != "" {
		stream.Title = *req.Title
	}

	// 更新日期
	if req.StreamDate != nil && *req.StreamDate != "" {
		// 嘗試解析 RFC3339 格式（完整時間），失敗則嘗試日期格式
		date, err := time.Parse(time.RFC3339, *req.StreamDate)
		if err != nil {
			// 如果不是 RFC3339，嘗試解析為日期格式
			date, err = time.Parse("2006-01-02", *req.StreamDate)
			if err != nil {
				return nil, fmt.Errorf("parse stream date: %w", err)
			}
		}
		stream.StreamDate = date
	}

	// 更新處理狀態
	if req.IsProcessed != nil {
		stream.IsProcessed = *req.IsProcessed
	}

	// 更新隱藏狀態
	if req.IsHidden != nil {
		stream.IsHidden = *req.IsHidden
	}

	// 更新 Stream
	if err := s.streamRepo.Update(stream); err != nil {
		return nil, fmt.Errorf("update stream: %w", err)
	}

	// 更新標籤
	if req.TagIDs != nil {
		if err := s.streamRepo.SetTags(id, req.TagIDs); err != nil {
			return nil, fmt.Errorf("set tags: %w", err)
		}
	}

	// 更新參與者
	if req.ParticipantIDs != nil {
		// 找出頻道擁有者（第一個參與者通常是擁有者）
		ownerID := ""
		if len(req.ParticipantIDs) > 0 {
			ownerID = req.ParticipantIDs[0]
		}
		if err := s.streamRepo.SetSingers(id, req.ParticipantIDs, ownerID); err != nil {
			return nil, fmt.Errorf("set participants: %w", err)
		}
	}

	// 返回更新後的資料
	return s.GetByID(id)
}
