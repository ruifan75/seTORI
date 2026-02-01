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

	// 驗證（不推算結束時間，由前端在需要時請求）
	validSongs := comment.ValidateSongs(dedupedSongs)

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

	// 注意：不再儲存到資料庫，CommentData 已在 sync 時由 loadAndSaveComments 儲存
	// 這個 API 主要用於即時分析和返回資料給前端

	return &dto.AnalyzeCommentsResponse{
		Songs:       songDTOs,
		RawComments: comments,
	}, nil
}
