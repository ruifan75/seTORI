package service

import (
	"database/sql"
	"fmt"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

type PerformanceService struct {
	perfRepo       *repository.PerformanceRepository
	songRepo       *repository.SongRepository
	songItunesRepo *repository.SongItunesRepository
}

func NewPerformanceService(
	perfRepo *repository.PerformanceRepository,
	songRepo *repository.SongRepository,
	songItunesRepo *repository.SongItunesRepository,
) *PerformanceService {
	return &PerformanceService{
		perfRepo:       perfRepo,
		songRepo:       songRepo,
		songItunesRepo: songItunesRepo,
	}
}

// CreatePerformances 直接從前端編輯結果建立演出記錄
// 會先刪除該 stream 的所有現有演出記錄，再建立新的
func (s *PerformanceService) CreatePerformances(streamID string, items []dto.CreatePerformanceItem) (*dto.CreatePerformancesResponse, error) {
	// 先刪除現有的演出記錄
	if err := s.DeleteByStreamID(streamID); err != nil {
		return nil, fmt.Errorf("delete existing performances: %w", err)
	}

	createdCount := 0

	for _, item := range items {
		// 1. 尋找或建立歌曲（優先使用 iTunes ID 配對）
		song, isNewSong, err := s.findOrCreateSong(item)
		if err != nil {
			return nil, fmt.Errorf("find or create song: %w", err)
		}

		// 2. 如果有 iTunes ID 且歌曲是新建的或還沒有這個 iTunes ID，建立關聯
		if item.ItunesID != nil && *item.ItunesID > 0 {
			// 檢查這個 iTunes ID 是否已經關聯到這首歌
			existingItunes, _ := s.songItunesRepo.FindByItunesID(*item.ItunesID)
			if existingItunes == nil {
				// 新增 iTunes ID 關聯
				songItunes := &models.SongITunes{
					SongID:    song.ID,
					ITunesID:  *item.ItunesID,
					IsPrimary: isNewSong, // 如果是新歌曲，設為主要
				}
				if err := s.songItunesRepo.Create(songItunes); err != nil {
					// 記錄錯誤但不中斷
					fmt.Printf("create song itunes error: %v\n", err)
				}
			}
		}

		// 3. 建立演出記錄
		perf := &models.Performance{
			StreamID:     streamID,
			SongID:       song.ID,
			StartSeconds: item.StartSeconds,
			EndSeconds:   item.EndSeconds,
			OrderIndex:   0, // 不再使用，改用 start_seconds 排序
		}

		if err := s.perfRepo.Create(perf); err != nil {
			return nil, fmt.Errorf("create performance: %w", err)
		}

		// 4. 設定標籤
		if len(item.Tags) > 0 {
			if err := s.perfRepo.SetTags(perf.ID, item.Tags); err != nil {
				return nil, fmt.Errorf("set performance tags: %w", err)
			}
		}

		// 5. 設定演唱者
		if len(item.SingerIDs) > 0 {
			if err := s.perfRepo.SetSingers(perf.ID, item.SingerIDs); err != nil {
				return nil, fmt.Errorf("set performance singers: %w", err)
			}
		}

		createdCount++
	}

	return &dto.CreatePerformancesResponse{
		CreatedCount: createdCount,
	}, nil
}

// findOrCreateSong 尋找或建立歌曲
// 優先順序：iTunes ID -> 歌名 + 藝人 -> 建立新歌曲
// 返回：歌曲, 是否為新建立的, 錯誤
func (s *PerformanceService) findOrCreateSong(item dto.CreatePerformanceItem) (*models.Song, bool, error) {
	// 1. 優先使用 iTunes ID 配對
	if item.ItunesID != nil && *item.ItunesID > 0 {
		song, err := s.songRepo.FindByItunesID(*item.ItunesID)
		if err != nil {
			return nil, false, fmt.Errorf("find by itunes id: %w", err)
		}
		if song != nil {
			// 歌曲已存在，檢查是否需要補上封面圖
			if (!song.Arts.Valid || song.Arts.String == "") && item.ArtURL != nil && *item.ArtURL != "" {
				song.Arts = sql.NullString{String: *item.ArtURL, Valid: true}
				if err := s.songRepo.Update(song); err != nil {
					return nil, false, fmt.Errorf("update song arts: %w", err)
				}
			}
			return song, false, nil
		}
	}

	// 2. 使用歌名和藝人配對
	song, err := s.songRepo.FindByNameAndArtist(item.Name, item.OriginalArtist)
	if err != nil {
		return nil, false, fmt.Errorf("find song: %w", err)
	}

	if song != nil {
		// 歌曲已存在，檢查是否需要補上封面圖
		if (!song.Arts.Valid || song.Arts.String == "") && item.ArtURL != nil && *item.ArtURL != "" {
			song.Arts = sql.NullString{String: *item.ArtURL, Valid: true}
			if err := s.songRepo.Update(song); err != nil {
				return nil, false, fmt.Errorf("update song arts: %w", err)
			}
		}
		return song, false, nil
	}

	// 3. 建立新歌曲
	song = &models.Song{
		Name:           item.Name,
		OriginalArtist: item.OriginalArtist,
	}
	// 設定讀音（如果有提供）
	if item.NameReading != "" {
		song.NameReading = sql.NullString{String: item.NameReading, Valid: true}
	}
	if item.OriginalArtistReading != "" {
		song.OriginalArtistReading = sql.NullString{String: item.OriginalArtistReading, Valid: true}
	}
	// 設定封面圖（如果有提供）
	if item.ArtURL != nil && *item.ArtURL != "" {
		song.Arts = sql.NullString{String: *item.ArtURL, Valid: true}
	}
	if err := s.songRepo.Create(song); err != nil {
		return nil, false, fmt.Errorf("create song: %w", err)
	}

	return song, true, nil
}

// DeleteByStreamID 刪除指定 stream 的所有演出記錄（用於重新編輯時）
func (s *PerformanceService) DeleteByStreamID(streamID string) error {
	// 取得所有演出
	performances, err := s.perfRepo.FindByStreamID(streamID)
	if err != nil {
		return fmt.Errorf("find performances: %w", err)
	}

	// 逐一刪除
	for _, perf := range performances {
		if err := s.perfRepo.Delete(perf.ID); err != nil {
			return fmt.Errorf("delete performance: %w", err)
		}
	}

	return nil
}
