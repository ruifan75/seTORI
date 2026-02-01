package service

import (
	"fmt"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/comment"
)

type CommentService struct {
	holodexService *HolodexService
	streamRepo     *repository.StreamRepository
}

func NewCommentService(
	holodexService *HolodexService,
	streamRepo *repository.StreamRepository,
) *CommentService {
	return &CommentService{
		holodexService: holodexService,
		streamRepo:     streamRepo,
	}
}

// AnalyzeComments 分析影片的評論並提取歌曲資訊
func (s *CommentService) AnalyzeComments(videoID string) (*dto.AnalyzeCommentsResponse, error) {
	// 從 Holodex 獲取評論
	comments, err := s.holodexService.GetVideoComments(videoID)
	if err != nil {
		return nil, fmt.Errorf("get comments: %w", err)
	}

	// 解析評論
	parsedSongs := comment.ParseComments(comments)

	// 過濾非歌曲項目
	filteredSongs := comment.FilterSongs(parsedSongs)

	// 去重
	dedupedSongs := comment.DeduplicateSongs(filteredSongs)

	// 取得影片資訊以獲取總長度
	stream, _ := s.streamRepo.FindByID(videoID)
	totalDuration := 0
	if stream != nil && stream.DurationSeconds.Valid {
		totalDuration = int(stream.DurationSeconds.Int32)
	}

	// 推算結束時間
	estimatedSongs := comment.EstimateEndTimes(dedupedSongs, totalDuration)

	// 驗證
	validSongs := comment.ValidateSongs(estimatedSongs)

	// 轉換為 DTO
	songDTOs := make([]dto.CommentSong, len(validSongs))
	for i, song := range validSongs {
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
		Songs:       songDTOs,
		RawComments: comments,
	}, nil
}
