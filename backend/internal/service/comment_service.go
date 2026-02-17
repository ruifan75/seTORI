package service

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/comment"
)

type CommentService struct {
	holodexService    *HolodexService
	streamRepo        *repository.StreamRepository
	filterKeywordRepo *repository.FilterKeywordRepository
}

func NewCommentService(
	holodexService *HolodexService,
	streamRepo *repository.StreamRepository,
	filterKeywordRepo *repository.FilterKeywordRepository,
) *CommentService {
	return &CommentService{
		holodexService:    holodexService,
		streamRepo:        streamRepo,
		filterKeywordRepo: filterKeywordRepo,
	}
}

// AnalyzeComments 分析影片的評論並提取歌曲資訊（去重 + 過濾）
// 優先從 comment_songs 讀取已解析結果，若無則從 comment_raw 解析
func (s *CommentService) AnalyzeComments(videoID string) (*dto.AnalyzeCommentsResponse, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}

	var parsedSongs []comment.ParsedSong

	// 優先從 comment_songs 讀取
	if stream != nil && len(stream.CommentSongs) > 0 {
		if err := json.Unmarshal(stream.CommentSongs, &parsedSongs); err != nil {
			parsedSongs = nil
		}
	}

	// Fallback: 從 comment_raw 或 Holodex 解析
	if len(parsedSongs) == 0 {
		comments, err := s.getComments(videoID, stream)
		if err != nil {
			return nil, err
		}
		parsedSongs = comment.ParseComments(comments)
	}

	// 從 DB 載入 filter/keep keywords
	filterKW, keepKW, err := s.loadFilterKeywords()
	if err != nil {
		log.Printf("failed to load filter keywords, skipping filter: %v", err)
	}

	// 去重 + 驗證 + 過濾
	deduped := comment.DeduplicateSongs(parsedSongs)
	validSongs := comment.ValidateSongs(deduped)
	filteredSongs := comment.FilterSongs(validSongs, filterKW, keepKW)

	songDTOs := make([]dto.CommentSong, len(filteredSongs))
	for i, song := range filteredSongs {
		songDTOs[i] = dto.CommentSong{
			Start:              song.Start,
			End:                song.End,
			Name:               song.Name,
			OriginalArtist:     song.OriginalArtist,
			OriginalComment:    song.OriginalComment,
			IsEndTimeEstimated: song.IsEndTimeEstimated,
		}
	}

	return &dto.AnalyzeCommentsResponse{
		Songs: songDTOs,
	}, nil
}

// BackfillCommentSongs 補填所有有 comment_raw 但沒有 comment_songs 的 stream
func (s *CommentService) BackfillCommentSongs() (int, error) {
	streams, err := s.streamRepo.FindWithoutCommentSongs()
	if err != nil {
		return 0, fmt.Errorf("find streams: %w", err)
	}

	count := 0
	for _, stream := range streams {
		var comments []string
		if err := json.Unmarshal(stream.CommentRaw, &comments); err != nil || len(comments) == 0 {
			continue
		}

		parsed := comment.ParseComments(comments)
		if len(parsed) == 0 {
			continue
		}

		songsJSON, err := json.Marshal(parsed)
		if err != nil {
			log.Printf("backfill marshal error (video: %s): %v", stream.ID, err)
			continue
		}

		stream.CommentSongs = songsJSON
		if err := s.streamRepo.Update(&stream); err != nil {
			log.Printf("backfill update error (video: %s): %v", stream.ID, err)
			continue
		}
		count++
	}

	return count, nil
}

// loadFilterKeywords 從 DB 載入 filter/keep keywords
func (s *CommentService) loadFilterKeywords() (filterKW, keepKW []string, err error) {
	keywords, err := s.filterKeywordRepo.FindAll()
	if err != nil {
		return nil, nil, err
	}

	for _, kw := range keywords {
		switch kw.Type {
		case "filter":
			filterKW = append(filterKW, kw.Keyword)
		case "keep":
			keepKW = append(keepKW, kw.Keyword)
		}
	}

	return filterKW, keepKW, nil
}

// getComments 從 DB 讀取原始留言，若無則從 Holodex 抓取
func (s *CommentService) getComments(videoID string, stream *models.Stream) ([]string, error) {
	if stream != nil && len(stream.CommentRaw) > 0 {
		var comments []string
		if err := json.Unmarshal(stream.CommentRaw, &comments); err == nil && len(comments) > 0 {
			return comments, nil
		}
	}

	comments, err := s.holodexService.GetVideoComments(videoID)
	if err != nil {
		return nil, fmt.Errorf("get comments from holodex: %w", err)
	}

	return comments, nil
}
