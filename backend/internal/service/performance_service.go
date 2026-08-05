package service

import (
	"database/sql"
	"fmt"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

type PerformanceService struct {
	perfRepo       *repository.PerformanceRepository
	songRepo       *repository.SongRepository
	songItunesRepo *repository.SongItunesRepository
	artistRepo     *repository.ArtistRepository
}

func NewPerformanceService(
	perfRepo *repository.PerformanceRepository,
	songRepo *repository.SongRepository,
	songItunesRepo *repository.SongItunesRepository,
	artistRepo *repository.ArtistRepository,
) *PerformanceService {
	return &PerformanceService{
		perfRepo:       perfRepo,
		songRepo:       songRepo,
		songItunesRepo: songItunesRepo,
		artistRepo:     artistRepo,
	}
}

// CreatePerformances 直接從前端編輯結果建立演出記錄
// 既存記録は全削除せず差分更新する（performance ID を保つため。ID はプレイリストから参照される）
func (s *PerformanceService) CreatePerformances(streamID string, items []dto.CreatePerformanceItem) (*dto.CreatePerformancesResponse, error) {
	desired := make([]models.Performance, 0, len(items))

	for _, item := range items {
		// 1. 尋找或建立歌曲（優先使用 iTunes ID 配對）
		song, isNewSong, err := s.findOrCreateSong(item)
		if err != nil {
			return nil, fmt.Errorf("find or create song: %w", err)
		}

		// 2. 如果有 iTunes ID，確保新增 iTunes ID 與歌曲的關聯
		if item.ItunesID != nil && *item.ItunesID > 0 {
			// 檢查這個 iTunes ID 是否已經被其他歌曲關聯
			existingItunes, _ := s.songItunesRepo.FindByItunesID(*item.ItunesID)

			// 只有在以下情況才新增：
			// 1. iTunes ID 還沒有被關聯到任何歌曲，或
			// 2. iTunes ID 已關聯到其他歌曲（這種情況不應該發生，但為安全起見檢查）
			if existingItunes == nil {
				// 檢查該歌曲是否已經有其他 iTunes ID
				existingSongItunes, _ := s.songItunesRepo.FindBySongID(song.ID)
				isPrimary := isNewSong || len(existingSongItunes) == 0 // 新歌曲或該歌曲還沒有任何 iTunes ID 時設為主要

				// iTunes ID 未被關聯，新增關聯
				songItunes := &models.SongITunes{
					SongID:    song.ID,
					ITunesID:  *item.ItunesID,
					IsPrimary: isPrimary,
				}
				if err := s.songItunesRepo.Create(songItunes); err != nil {
					// 記錄錯誤但不中斷
					fmt.Printf("create song itunes error: %v\n", err)
				}
			} else if existingItunes.SongID != song.ID {
				// iTunes ID 關聯到不同的歌曲（這可能表示重複的歌曲，記錄警告）
				fmt.Printf("warning: iTunes ID %d already associated with different song\n", *item.ItunesID)
			}
		}

		// 3. 目標状態を組み立てる（書き込みは後段の差分更新でまとめて行う）
		desired = append(desired, models.Performance{
			StreamID:     streamID,
			SongID:       song.ID,
			StartSeconds: item.StartSeconds,
			EndSeconds:   item.EndSeconds,
			OrderIndex:   0, // 不再使用，改用 start_seconds 排序
			CustomTags:   item.CustomTags,
		})
	}

	// 4. 既存記録との差分を反映（ID を維持）。返る ID は desired と同じ並び。
	perfIDs, err := s.perfRepo.ReconcilePerformances(streamID, desired)
	if err != nil {
		return nil, fmt.Errorf("reconcile performances: %w", err)
	}

	// 5. タグ・歌手は毎回設定し直す（維持された記録も内容が変わりうるため、空でも呼ぶ）
	for i, item := range items {
		if err := s.perfRepo.SetTags(perfIDs[i], item.Tags); err != nil {
			return nil, fmt.Errorf("set performance tags: %w", err)
		}
		if err := s.perfRepo.SetSingers(perfIDs[i], item.SingerIDs); err != nil {
			return nil, fmt.Errorf("set performance singers: %w", err)
		}
	}

	return &dto.CreatePerformancesResponse{
		CreatedCount: len(perfIDs),
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
	// artists / song_artists マッピングを同期（失敗は警告のみ）
	if err := s.artistRepo.SyncSongArtist(song.ID, song.OriginalArtist); err != nil {
		logger.Warnf("sync song artist mapping failed (song: %s): %v", song.ID, err)
	}

	return song, true, nil
}

// DeleteByStreamID 刪除指定 stream 的所有演出記錄（セットリストの明示的な全削除用）
// 編集時の保存は ReconcilePerformances による差分更新を使い、ここは通らない。
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
