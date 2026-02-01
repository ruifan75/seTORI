package service

import (
	"fmt"
	"regexp"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/pkg/itunes"
)

type EndTimeEstimateService struct {
	itunesClient *itunes.Client
}

func NewEndTimeEstimateService(itunesClient *itunes.Client) *EndTimeEstimateService {
	return &EndTimeEstimateService{
		itunesClient: itunesClient,
	}
}

const (
	DefaultSongDuration = 240 // 4 分鐘
	MaxSongDuration     = 600 // 10 分鐘
	MinSongDuration     = 60  // 1 分鐘
)

// EstimateEndTimes 推算結束時間
func (s *EndTimeEstimateService) EstimateEndTimes(req *dto.EstimateEndTimesRequest) (*dto.EstimateEndTimesResponse, error) {
	results := make([]dto.SongEndTimeEstimate, len(req.Songs))

	for i, song := range req.Songs {
		estimate := s.estimateSingleSongEndTime(song, req.Songs, i, req.StreamEnd)
		results[i] = estimate
	}

	return &dto.EstimateEndTimesResponse{
		Estimates: results,
		Message:   fmt.Sprintf("已推算 %d 首歌曲的結束時間", len(results)),
	}, nil
}

func (s *EndTimeEstimateService) estimateSingleSongEndTime(
	song dto.SongEndTimeEstimateRequest,
	allSongs []dto.SongEndTimeEstimateRequest,
	index int,
	streamEnd int,
) dto.SongEndTimeEstimate {

	// 如果已經有結束時間，直接返回
	if song.End > 0 {
		return dto.SongEndTimeEstimate{
			EstimatedEnd:       song.End,
			IsEndTimeEstimated: false,
			Method:             "from_comment",
			Reason:             "歌曲已有結束時間",
		}
	}

	// 策略 1: 優先使用 iTunes API（如果有 iTunes ID）
	if song.ItunesID > 0 {
		duration, err := s.queryItunesDuration(song.ItunesID)
		if err == nil && duration > 0 {
			estimatedEnd := song.Start + int(duration)
			// 檢查是否超過下一首或影片結束
			estimatedEnd = s.constrainEndTime(estimatedEnd, allSongs, index, streamEnd)
			return dto.SongEndTimeEstimate{
				EstimatedEnd:       estimatedEnd,
				IsEndTimeEstimated: true,
				Method:             "from_itunes",
				OriginalItunesDur:  int(duration),
				Reason:             fmt.Sprintf("使用 iTunes API 查詢到歌曲長度 %d 秒", int(duration)),
			}
		}
	}

	// 策略 2: 使用下一首歌的開始時間
	if index+1 < len(allSongs) {
		nextStart := allSongs[index+1].Start
		duration := nextStart - song.Start

		// 檢查長度是否合理
		if duration >= MinSongDuration && duration <= MaxSongDuration {
			return dto.SongEndTimeEstimate{
				EstimatedEnd:       nextStart,
				IsEndTimeEstimated: true,
				Method:             "from_next_song",
				Reason:             fmt.Sprintf("使用下一首歌的開始時間 (%d 秒的歌曲)", duration),
			}
		}

		// 如果太長或太短，使用預設長度
		estimatedEnd := song.Start + DefaultSongDuration
		if estimatedEnd > nextStart {
			estimatedEnd = nextStart
		}
		return dto.SongEndTimeEstimate{
			EstimatedEnd:       estimatedEnd,
			IsEndTimeEstimated: true,
			Method:             "from_default",
			Reason:             fmt.Sprintf("推算長度 %d 秒（已調整至合理範圍）", estimatedEnd-song.Start),
		}
	}

	// 策略 3: 最後一首歌，使用影片結束時間
	if streamEnd > 0 && streamEnd > song.Start {
		duration := streamEnd - song.Start
		if duration <= MaxSongDuration {
			return dto.SongEndTimeEstimate{
				EstimatedEnd:       streamEnd,
				IsEndTimeEstimated: true,
				Method:             "from_stream_end",
				Reason:             fmt.Sprintf("使用影片結束時間 (%d 秒的歌曲)", duration),
			}
		}

		// 如果太長，使用預設長度
		estimatedEnd := song.Start + DefaultSongDuration
		return dto.SongEndTimeEstimate{
			EstimatedEnd:       estimatedEnd,
			IsEndTimeEstimated: true,
			Method:             "from_default",
			Reason:             fmt.Sprintf("影片剩餘時間過長，改用預設 %d 秒", DefaultSongDuration),
		}
	}

	// 策略 4: 使用預設長度
	estimatedEnd := song.Start + DefaultSongDuration
	return dto.SongEndTimeEstimate{
		EstimatedEnd:       estimatedEnd,
		IsEndTimeEstimated: true,
		Method:             "from_default",
		Reason:             fmt.Sprintf("無其他資訊，使用預設歌曲長度 %d 秒", DefaultSongDuration),
	}
}

// constrainEndTime 確保結束時間在合理範圍內
func (s *EndTimeEstimateService) constrainEndTime(
	estimatedEnd int,
	allSongs []dto.SongEndTimeEstimateRequest,
	currentIndex int,
	streamEnd int,
) int {
	// 不應超過下一首歌的開始時間
	if currentIndex+1 < len(allSongs) {
		nextStart := allSongs[currentIndex+1].Start
		if estimatedEnd > nextStart {
			estimatedEnd = nextStart
		}
	}

	// 不應超過影片結束時間
	if streamEnd > 0 && estimatedEnd > streamEnd {
		estimatedEnd = streamEnd
	}

	return estimatedEnd
}

// queryItunesDuration 查詢 iTunes API 獲取歌曲長度（秒）
func (s *EndTimeEstimateService) queryItunesDuration(itunesID int64) (int, error) {
	if s.itunesClient == nil {
		return 0, fmt.Errorf("iTunes client not initialized")
	}

	// 使用 iTunes Lookup API 查詢
	result, err := s.itunesClient.QueryByID(itunesID)
	if err != nil {
		return 0, fmt.Errorf("query iTunes: %w", err)
	}

	if result == nil {
		return 0, fmt.Errorf("no results from iTunes")
	}

	trackTime := result.TrackTimeMillis
	if trackTime <= 0 {
		return 0, fmt.Errorf("invalid track duration: %d", trackTime)
	}

	// 轉換為秒
	durationSeconds := int(trackTime / 1000)
	return durationSeconds, nil
}

// ParseTimestamp 解析時間戳為秒數（用於後續擴展）
func ParseTimestamp(ts string) int {
	parts := regexp.MustCompile(`(\d+):(\d+)(?::(\d+))?`).FindStringSubmatch(ts)
	if len(parts) < 3 {
		return 0
	}

	// 處理 HH:MM:SS 或 MM:SS 格式
	if len(parts) == 4 && parts[3] != "" {
		// HH:MM:SS
		hours := 0
		fmt.Sscanf(parts[1], "%d", &hours)
		minutes := 0
		fmt.Sscanf(parts[2], "%d", &minutes)
		seconds := 0
		fmt.Sscanf(parts[3], "%d", &seconds)
		return hours*3600 + minutes*60 + seconds
	}

	// MM:SS
	minutes := 0
	fmt.Sscanf(parts[1], "%d", &minutes)
	seconds := 0
	fmt.Sscanf(parts[2], "%d", &seconds)
	return minutes*60 + seconds
}

// AddRate limiting 避免過度查詢 iTunes API
func (s *EndTimeEstimateService) addRateLimit() {
	time.Sleep(100 * time.Millisecond)
}
