package service

import (
	"encoding/json"
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
// mode: "regex" = 正則解析 + 程式碼去重, "ai" = AI 解析（含去重）fallback 正則
func (s *CommentService) AnalyzeComments(videoID string, mode string) (*dto.AnalyzeCommentsResponse, error) {
	// 優先從 DB 讀取 comment_raw
	comments, err := s.getComments(videoID)
	if err != nil {
		return nil, err
	}

	// 根據 mode 選擇解析方式
	var parsedSongs []comment.ParsedSong

	switch mode {
	case "ai":
		// AI 解析（含去重），失敗時 fallback 到正則
		if s.holodexService != nil && s.holodexService.aiClient != nil {
			if aiSongs, err := comment.ParseCommentsWithAI(s.holodexService.aiClient, comments); err == nil && len(aiSongs) > 0 {
				parsedSongs = aiSongs
			}
		}
		if len(parsedSongs) == 0 {
			// AI 失敗或不可用，fallback 到正則 + 程式碼去重
			parsedSongs = comment.DeduplicateSongs(comment.ParseComments(comments))
		}
	default: // "regex"
		parsedSongs = comment.DeduplicateSongs(comment.ParseComments(comments))
	}

	validSongs := comment.ValidateSongs(parsedSongs)

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

// getComments 從 DB 讀取原始留言，若無則從 Holodex 抓取
func (s *CommentService) getComments(videoID string) ([]string, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}

	// 優先從 DB 的 comment_raw 讀取
	if stream != nil && len(stream.CommentRaw) > 0 {
		var comments []string
		if err := json.Unmarshal(stream.CommentRaw, &comments); err == nil && len(comments) > 0 {
			return comments, nil
		}
	}

	// Fallback: 從 Holodex 抓取
	comments, err := s.holodexService.GetVideoComments(videoID)
	if err != nil {
		return nil, fmt.Errorf("get comments from holodex: %w", err)
	}

	return comments, nil
}
