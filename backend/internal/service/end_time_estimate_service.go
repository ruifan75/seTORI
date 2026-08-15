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
	DefaultSongDuration = 240 // 4 分
	MaxSongDuration     = 600 // 10 分
	MinSongDuration     = 60  // 1 分
)

// EstimateEndTimes は終了時刻を推定する。
func (s *EndTimeEstimateService) EstimateEndTimes(req *dto.EstimateEndTimesRequest) (*dto.EstimateEndTimesResponse, error) {
	results := make([]dto.SongEndTimeEstimate, len(req.Songs))

	for i, song := range req.Songs {
		estimate := s.estimateSingleSongEndTime(song, req.Songs, i, req.StreamEnd)
		results[i] = estimate
	}

	return &dto.EstimateEndTimesResponse{
		Estimates: results,
		Message:   fmt.Sprintf("%d 曲の終了時刻を推定しました", len(results)),
	}, nil
}

func (s *EndTimeEstimateService) estimateSingleSongEndTime(
	song dto.SongEndTimeEstimateRequest,
	allSongs []dto.SongEndTimeEstimateRequest,
	index int,
	streamEnd int,
) dto.SongEndTimeEstimate {

	// 終了時刻が既にあればそのまま返す
	if song.End > 0 {
		return dto.SongEndTimeEstimate{
			EstimatedEnd:       song.End,
			IsEndTimeEstimated: false,
			Method:             "from_comment",
			Reason:             "楽曲には既に終了時刻があります",
		}
	}

	// 方針 1：iTunes ID があれば iTunes API を優先する
	if song.ItunesID > 0 {
		duration, err := s.queryItunesDuration(song.ItunesID)
		if err == nil && duration > 0 {
			estimatedEnd := song.Start + int(duration)
			// 次の曲の開始または動画の終了を越えないか確認する
			estimatedEnd = s.constrainEndTime(estimatedEnd, allSongs, index, streamEnd)
			return dto.SongEndTimeEstimate{
				EstimatedEnd:       estimatedEnd,
				IsEndTimeEstimated: true,
				Method:             "from_itunes",
				OriginalItunesDur:  int(duration),
				Reason:             fmt.Sprintf("iTunes API で取得した曲の長さ：%d 秒", int(duration)),
			}
		}
	}

	// 方針 2：次の曲の開始時刻を使う
	if index+1 < len(allSongs) {
		nextStart := allSongs[index+1].Start
		duration := nextStart - song.Start

		// 長さが妥当か確認する
		if duration >= MinSongDuration && duration <= MaxSongDuration {
			return dto.SongEndTimeEstimate{
				EstimatedEnd:       nextStart,
				IsEndTimeEstimated: true,
				Method:             "from_next_song",
				Reason:             fmt.Sprintf("次の曲の開始時刻を使用（曲の長さ：%d 秒）", duration),
			}
		}

		// 長すぎるか短すぎる場合は既定の長さを使う
		estimatedEnd := song.Start + DefaultSongDuration
		if estimatedEnd > nextStart {
			estimatedEnd = nextStart
		}
		return dto.SongEndTimeEstimate{
			EstimatedEnd:       estimatedEnd,
			IsEndTimeEstimated: true,
			Method:             "from_default",
			Reason:             fmt.Sprintf("推定した長さ：%d 秒（妥当な範囲に調整済み）", estimatedEnd-song.Start),
		}
	}

	// 方針 3：最後の曲には動画の終了時刻を使う
	if streamEnd > 0 && streamEnd > song.Start {
		duration := streamEnd - song.Start
		if duration <= MaxSongDuration {
			return dto.SongEndTimeEstimate{
				EstimatedEnd:       streamEnd,
				IsEndTimeEstimated: true,
				Method:             "from_stream_end",
				Reason:             fmt.Sprintf("動画の終了時刻を使用（曲の長さ：%d 秒）", duration),
			}
		}

		// 長すぎる場合は既定の長さを使う
		estimatedEnd := song.Start + DefaultSongDuration
		return dto.SongEndTimeEstimate{
			EstimatedEnd:       estimatedEnd,
			IsEndTimeEstimated: true,
			Method:             "from_default",
			Reason:             fmt.Sprintf("動画の残り時間が長すぎるため、既定の %d 秒を使用", DefaultSongDuration),
		}
	}

	// 方針 4：既定の長さを使う
	estimatedEnd := song.Start + DefaultSongDuration
	return dto.SongEndTimeEstimate{
		EstimatedEnd:       estimatedEnd,
		IsEndTimeEstimated: true,
		Method:             "from_default",
		Reason:             fmt.Sprintf("他の情報がないため、既定の曲の長さ %d 秒を使用", DefaultSongDuration),
	}
}

// constrainEndTime は終了時刻を妥当な範囲に収める。
func (s *EndTimeEstimateService) constrainEndTime(
	estimatedEnd int,
	allSongs []dto.SongEndTimeEstimateRequest,
	currentIndex int,
	streamEnd int,
) int {
	// 次の曲の開始時刻を越えないようにする
	if currentIndex+1 < len(allSongs) {
		nextStart := allSongs[currentIndex+1].Start
		if estimatedEnd > nextStart {
			estimatedEnd = nextStart
		}
	}

	// 動画の終了時刻を越えないようにする
	if streamEnd > 0 && estimatedEnd > streamEnd {
		estimatedEnd = streamEnd
	}

	return estimatedEnd
}

// queryItunesDuration は iTunes API から曲の長さ（秒）を取得する。
func (s *EndTimeEstimateService) queryItunesDuration(itunesID int64) (int, error) {
	if s.itunesClient == nil {
		return 0, fmt.Errorf("iTunes client not initialized")
	}

	// iTunes Lookup API で照会する
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

	// 秒に変換する
	durationSeconds := int(trackTime / 1000)
	return durationSeconds, nil
}

// ParseTimestamp はタイムスタンプを秒数に変換する（今後の拡張用）。
func ParseTimestamp(ts string) int {
	parts := regexp.MustCompile(`(\d+):(\d+)(?::(\d+))?`).FindStringSubmatch(ts)
	if len(parts) < 3 {
		return 0
	}

	// HH:MM:SS または MM:SS 形式を処理する
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

// AddRate limiting は iTunes API への過剰な問い合わせを防ぐ。
func (s *EndTimeEstimateService) addRateLimit() {
	time.Sleep(100 * time.Millisecond)
}
