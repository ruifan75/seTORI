package service

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/comment"
)

type CommentService struct {
	holodexService    *HolodexService
	streamRepo        *repository.StreamRepository
	filterKeywordRepo *repository.FilterKeywordRepository
	aiClient          ai.Chatter // 留言 AI hybrid 解析用（多 provider 輪替）
}

func NewCommentService(
	holodexService *HolodexService,
	streamRepo *repository.StreamRepository,
	filterKeywordRepo *repository.FilterKeywordRepository,
	aiClient ai.Chatter,
) *CommentService {
	return &CommentService{
		holodexService:    holodexService,
		streamRepo:        streamRepo,
		filterKeywordRepo: filterKeywordRepo,
		aiClient:          aiClient,
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

	// 取得原始留言，於 edit-time 進行 AI hybrid 解析（AI 判斷歌曲行 + 正則抽取文字）；
	// AI 不可用或失敗時自動退回純正則。
	comments, err := s.getComments(videoID, stream)
	if err != nil {
		// 無法取得留言：退回已快取的 comment_songs（正則結果），否則回報錯誤
		if stream != nil && len(stream.CommentSongs) > 0 {
			_ = json.Unmarshal(stream.CommentSongs, &parsedSongs)
		} else {
			return nil, err
		}
	} else {
		parsedSongs = s.parseComments(comments)
		// AI/正則都解析不出歌曲時，退回已快取的 comment_songs
		if len(parsedSongs) == 0 && stream != nil && len(stream.CommentSongs) > 0 {
			_ = json.Unmarshal(stream.CommentSongs, &parsedSongs)
		}
	}

	// 從 DB 載入 filter/keep keywords
	filterKW, keepKW, err := s.loadFilterKeywords()
	if err != nil {
		log.Printf("failed to load filter keywords, skipping filter: %v", err)
	}

	// 過濾 + 去重 + 驗證（先過濾避免非歌曲項目影響去重）
	filteredSongs := comment.FilterSongs(parsedSongs, filterKW, keepKW)
	deduped := comment.DeduplicateSongs(filteredSongs)
	validSongs := comment.ValidateSongs(deduped)

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

// parseComments 於 edit-time 解析留言：優先用 AI hybrid（AI 選取歌曲行 + 正則抽取文字），
// 失敗、無歌曲或未設定 Groq key 時退回純正則。
func (s *CommentService) parseComments(comments []string) []comment.ParsedSong {
	if s.aiClient != nil {
		songs, err := comment.ParseCommentsWithAI(s.aiClient, comments)
		if err != nil {
			log.Printf("AI comment parse failed, falling back to regex: %v", err)
		} else if len(songs) > 0 {
			return songs
		}
	}
	return comment.ParseComments(comments)
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
