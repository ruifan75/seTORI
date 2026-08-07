package service

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

type SongService struct {
	songRepo       *repository.SongRepository
	perfRepo       *repository.PerformanceRepository
	songItunesRepo *repository.SongItunesRepository
	artistRepo     *repository.ArtistRepository
	matchService   *SongMatchService
}

// SetMatchService は照合サービスを注入する。
// SongMatchService 側が SongService を必要としないので循環はしないが、
// 生成順の都合でコンストラクタ引数ではなく後入れにしている。
func (s *SongService) SetMatchService(m *SongMatchService) { s.matchService = m }

func NewSongService(
	songRepo *repository.SongRepository,
	perfRepo *repository.PerformanceRepository,
	songItunesRepo *repository.SongItunesRepository,
	artistRepo *repository.ArtistRepository,
) *SongService {
	return &SongService{
		songRepo:       songRepo,
		perfRepo:       perfRepo,
		songItunesRepo: songItunesRepo,
		artistRepo:     artistRepo,
	}
}

// syncArtistMapping は楽曲の original_artist と artists/song_artists を同期する。
// 失敗しても楽曲操作自体は成功扱い（マッピングは検索用の付随データのため）。
func (s *SongService) syncArtistMapping(song *models.Song) {
	if err := s.artistRepo.SyncSongArtist(song.ID, song.OriginalArtist); err != nil {
		logger.Warnf("sync song artist mapping failed (song: %s): %v", song.ID, err)
	}
}

// GetAll 取得歌曲列表
func (s *SongService) GetAll(page, limit int, search, sort, dir string) (*dto.SongListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	songs, total, err := s.songRepo.FindAll(limit, offset, search, sort, dir)
	if err != nil {
		return nil, fmt.Errorf("get songs: %w", err)
	}

	// 批次取得演出次數與 iTunes 關聯，避免 N+1
	songIDs := make([]uuid.UUID, len(songs))
	for i, song := range songs {
		songIDs[i] = song.ID
	}
	counts, err := s.songRepo.GetPerformanceCounts(songIDs)
	if err != nil {
		return nil, fmt.Errorf("get performance counts: %w", err)
	}
	itunesMap, err := s.songItunesRepo.FindBySongIDs(songIDs)
	if err != nil {
		return nil, fmt.Errorf("get song itunes: %w", err)
	}
	artistMap, err := s.artistRepo.FindReferencesBySongIDs(songIDs)
	if err != nil {
		return nil, fmt.Errorf("get song artists: %w", err)
	}

	// 轉換為 DTO
	songResponses := make([]dto.SongResponse, len(songs))
	for i, song := range songs {
		songResponses[i] = buildSongResponse(song, counts[song.ID], itunesMap[song.ID])
		songResponses[i].Artists = toArtistReferences(artistMap[song.ID])
	}

	totalPages := (total + limit - 1) / limit

	return &dto.SongListResponse{
		Songs: songResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// GetByID 取得單首歌曲
func (s *SongService) GetByID(id uuid.UUID) (*dto.SongResponse, error) {
	song, err := s.songRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get song: %w", err)
	}
	if song == nil {
		return nil, nil
	}

	count, _ := s.songRepo.GetPerformanceCount(song.ID)
	resp := s.toSongResponse(*song, count)
	return &resp, nil
}

// GetPerformances 取得歌曲的所有演出（反向查詢）
func (s *SongService) GetPerformances(songID uuid.UUID, page, limit int) (*dto.SongPerformanceListResponse, error) {
	song, err := s.songRepo.FindByID(songID)
	if err != nil {
		return nil, fmt.Errorf("get song: %w", err)
	}
	if song == nil {
		return nil, nil
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	performances, total, err := s.perfRepo.FindBySongID(songID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("get performances: %w", err)
	}

	// 轉換為 DTO
	perfResponses := make([]dto.SongPerformanceResponse, len(performances))
	for i, perf := range performances {
		perfResponses[i] = s.toSongPerformanceResponse(perf)
	}

	count, _ := s.songRepo.GetPerformanceCount(songID)
	songResp := s.toSongResponse(*song, count)
	totalPages := (total + limit - 1) / limit

	return &dto.SongPerformanceListResponse{
		Song:         songResp,
		Performances: perfResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// Create 建立新歌曲
func (s *SongService) Create(req *dto.CreateSongRequest) (*dto.SongResponse, error) {
	// 檢查是否已存在
	existing, err := s.songRepo.FindByNameAndArtist(req.Name, req.OriginalArtist)
	if err != nil {
		return nil, fmt.Errorf("check existing song: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("song already exists")
	}

	song := &models.Song{
		Name:           req.Name,
		OriginalArtist: req.OriginalArtist,
	}

	if req.NameReading != nil {
		song.NameReading = sql.NullString{String: *req.NameReading, Valid: true}
	}
	if req.OriginalArtistReading != nil {
		song.OriginalArtistReading = sql.NullString{String: *req.OriginalArtistReading, Valid: true}
	}
	if req.Arts != nil {
		song.Arts = sql.NullString{String: *req.Arts, Valid: true}
	}

	err = s.songRepo.Create(song)
	if err != nil {
		return nil, fmt.Errorf("create song: %w", err)
	}
	s.syncArtistMapping(song)

	// 處理 iTunes IDs - 如果只有一個，自動設為 Primary
	if len(req.ItunesIds) > 0 {
		for i, itunesItem := range req.ItunesIds {
			// 如果只有一個 iTunes ID，設為 Primary；否則遵循請求中的設定
			isPrimary := itunesItem.IsPrimary
			if len(req.ItunesIds) == 1 {
				isPrimary = true
			}

			err = s.songItunesRepo.Create(&models.SongITunes{
				SongID:    song.ID,
				ITunesID:  itunesItem.ItunesID,
				IsPrimary: isPrimary,
			})
			if err != nil {
				return nil, fmt.Errorf("create song itunes record %d: %w", i, err)
			}
		}
	}

	resp := s.toSongResponse(*song, 0)
	return &resp, nil
}

// Update 更新歌曲
func (s *SongService) Update(id uuid.UUID, req *dto.UpdateSongRequest) (*dto.SongResponse, error) {
	song, err := s.songRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get song: %w", err)
	}
	if song == nil {
		return nil, nil
	}

	song.Name = req.Name
	song.OriginalArtist = req.OriginalArtist

	if req.NameReading != nil {
		song.NameReading = sql.NullString{String: *req.NameReading, Valid: true}
	} else {
		song.NameReading = sql.NullString{Valid: false}
	}
	if req.OriginalArtistReading != nil {
		song.OriginalArtistReading = sql.NullString{String: *req.OriginalArtistReading, Valid: true}
	} else {
		song.OriginalArtistReading = sql.NullString{Valid: false}
	}
	if req.Arts != nil {
		song.Arts = sql.NullString{String: *req.Arts, Valid: true}
	} else {
		song.Arts = sql.NullString{Valid: false}
	}

	err = s.songRepo.Update(song)
	if err != nil {
		return nil, fmt.Errorf("update song: %w", err)
	}
	s.syncArtistMapping(song)

	// 處理 iTunes IDs - 先刪除舊的，再添加新的
	if len(req.ItunesIds) > 0 {
		// 刪除現有的 iTunes 關聯
		_ = s.songItunesRepo.DeleteBySongID(song.ID)

		// 添加新的 iTunes 關聯
		for _, item := range req.ItunesIds {
			songItunes := &models.SongITunes{
				SongID:    song.ID,
				ITunesID:  item.ItunesID,
				IsPrimary: item.IsPrimary,
			}
			_ = s.songItunesRepo.Create(songItunes)
		}
	} else {
		// 如果沒有傳送 iTunes IDs，也要刪除現有的
		_ = s.songItunesRepo.DeleteBySongID(song.ID)
	}

	count, _ := s.songRepo.GetPerformanceCount(song.ID)
	resp := s.toSongResponse(*song, count)
	return &resp, nil
}

// Delete 刪除歌曲
func (s *SongService) Delete(id uuid.UUID) error {
	return s.songRepo.Delete(id)
}

// nullStr は sql.NullString を文字列に変換する（NULL は ""）。
func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// GetEditableFields は修正提案の対象となる編集可能フィールドと表示ラベルを返す。
// 見つからなければ (nil, "", nil)。TargetEditor インターフェースを満たす。
func (s *SongService) GetEditableFields(id uuid.UUID) (map[string]string, string, error) {
	song, err := s.songRepo.FindByID(id)
	if err != nil {
		return nil, "", err
	}
	if song == nil {
		return nil, "", nil
	}
	fields := map[string]string{
		"name":                    song.Name,
		"name_reading":            nullStr(song.NameReading),
		"original_artist":         song.OriginalArtist,
		"original_artist_reading": nullStr(song.OriginalArtistReading),
	}
	return fields, song.Name + " / " + song.OriginalArtist, nil
}

// ApplyEditableFields は提案された編集値を楽曲へ反映する（iTunes/arts は変更しない）。
func (s *SongService) ApplyEditableFields(id uuid.UUID, fields map[string]string) error {
	song, err := s.songRepo.FindByID(id)
	if err != nil {
		return err
	}
	if song == nil {
		return fmt.Errorf("曲が見つかりません")
	}
	if v, ok := fields["name"]; ok {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("曲名は必須です")
		}
		song.Name = strings.TrimSpace(v)
	}
	if v, ok := fields["original_artist"]; ok {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("原曲アーティストは必須です")
		}
		song.OriginalArtist = strings.TrimSpace(v)
	}
	if v, ok := fields["name_reading"]; ok {
		song.NameReading = sql.NullString{String: strings.TrimSpace(v), Valid: strings.TrimSpace(v) != ""}
	}
	if v, ok := fields["original_artist_reading"]; ok {
		song.OriginalArtistReading = sql.NullString{String: strings.TrimSpace(v), Valid: strings.TrimSpace(v) != ""}
	}
	if err := s.songRepo.Update(song); err != nil {
		return fmt.Errorf("update song: %w", err)
	}
	s.syncArtistMapping(song)
	return nil
}

// MergeSongs 將來源歌曲合併至目標歌曲
func (s *SongService) MergeSongs(sourceSongID, targetSongID uuid.UUID) error {
	if err := s.songRepo.MergeSong(sourceSongID, targetSongID); err != nil {
		return err
	}
	// 統合が済んだので、この 2 曲に紐づく未処理の統合候補は畳む。
	// 残しておくと解決済みの組が一覧に出続ける。
	if s.matchService != nil {
		if err := s.matchService.ResolveCandidatesForMergedSong(sourceSongID, targetSongID); err != nil {
			logger.Warnf("resolve merge candidates failed (%s → %s): %v", sourceSongID, targetSongID, err)
		}
	}
	return nil
}

// SearchSimilar 搜尋相似歌曲（用於 AI 正規化建議）
func (s *SongService) SearchSimilar(name string, limit int) ([]dto.SongResponse, error) {
	songs, err := s.songRepo.SearchSimilar(name, limit)
	if err != nil {
		return nil, fmt.Errorf("search similar: %w", err)
	}

	responses := make([]dto.SongResponse, len(songs))
	for i, song := range songs {
		count, _ := s.songRepo.GetPerformanceCount(song.ID)
		responses[i] = s.toSongResponse(song, count)
	}
	return responses, nil
}

// toSongResponse 轉換 Model 到 DTO（單筆查詢時即時抓取 iTunes 關聯）
func (s *SongService) toSongResponse(song models.Song, count int) dto.SongResponse {
	var itunesRecords []models.SongITunes
	if s.songItunesRepo != nil {
		itunesRecords, _ = s.songItunesRepo.FindBySongID(song.ID)
	}
	resp := buildSongResponse(song, count, itunesRecords)
	if s.artistRepo != nil {
		artists, _ := s.artistRepo.FindReferencesBySongID(song.ID)
		resp.Artists = toArtistReferences(artists)
	}
	return resp
}

func toArtistReferences(artists []models.ArtistReference) []dto.ArtistReference {
	refs := make([]dto.ArtistReference, len(artists))
	for i, artist := range artists {
		refs[i] = dto.ArtistReference{ID: artist.ID, Name: artist.Name}
	}
	return refs
}

// buildSongResponse 由已備妥的資料組裝 DTO（供批次列表與單筆查詢共用）
func buildSongResponse(song models.Song, count int, itunesRecords []models.SongITunes) dto.SongResponse {
	resp := dto.SongResponse{
		ID:               song.ID,
		Name:             song.Name,
		OriginalArtist:   song.OriginalArtist,
		PerformanceCount: count,
		CreatedAt:        song.CreatedAt,
		UpdatedAt:        song.UpdatedAt,
		Artists:          []dto.ArtistReference{},
	}

	if song.NameReading.Valid {
		resp.NameReading = &song.NameReading.String
	}
	if song.OriginalArtistReading.Valid {
		resp.OriginalArtistReading = &song.OriginalArtistReading.String
	}
	if song.Arts.Valid {
		resp.Arts = &song.Arts.String
	}

	if len(itunesRecords) > 0 {
		resp.ItunesIDs = make([]dto.SongItunesResponse, len(itunesRecords))
		for i, rec := range itunesRecords {
			resp.ItunesIDs[i] = dto.SongItunesResponse{
				ItunesID:  rec.ITunesID,
				IsPrimary: rec.IsPrimary,
			}
			if rec.CollectionName.Valid {
				resp.ItunesIDs[i].CollectionName = &rec.CollectionName.String
			}
			if rec.Country.Valid {
				resp.ItunesIDs[i].Country = &rec.Country.String
			}
		}
	}

	return resp
}

// toSongPerformanceResponse 轉換演出到 DTO
func (s *SongService) toSongPerformanceResponse(perf repository.PerformanceWithDetails) dto.SongPerformanceResponse {
	resp := dto.SongPerformanceResponse{
		ID:             perf.ID,
		StreamID:       perf.StreamID,
		StreamTitle:    perf.StreamTitle,
		StreamDate:     perf.StreamDate,
		SongID:         perf.SongID,
		SongName:       perf.SongName,
		OriginalArtist: perf.OriginalArtist,
		Artists:        toArtistReferences(perf.Artists),
		StartSeconds:   perf.StartSeconds,
		EndSeconds:     perf.EndSeconds,
		YouTubeURL:     fmt.Sprintf("https://www.youtube.com/watch?v=%s&t=%d", perf.StreamID, perf.StartSeconds),
		CreatedAt:      perf.CreatedAt,
	}

	if perf.ThumbnailURL.Valid {
		resp.ThumbnailURL = &perf.ThumbnailURL.String
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
