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
	if err := s.artistRepo.SyncSongArtist(song.ID, song.OriginalArtist, nullStr(song.OriginalArtistReading)); err != nil {
		logger.Warnf("sync song artist mapping failed (song: %s): %v", song.ID, err)
	}
}

// GetAll は楽曲一覧を取得する。
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

	// 歌唱回数と iTunes 関連を一括取得し、N+1 を避ける
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

	// DTO に変換する
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

// GetByID は楽曲を 1 件取得する。
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

// GetPerformances は楽曲のすべての歌唱を取得する（逆引き）。
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

	// 曲ページは発見面。秘匿された配信の歌唱は出さない。
	performances, total, err := s.perfRepo.FindBySongID(songID, limit, offset, repository.PublicAccess)
	if err != nil {
		return nil, fmt.Errorf("get performances: %w", err)
	}

	// DTO に変換する
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

// Create は新しい楽曲を作成する。
func (s *SongService) Create(req *dto.CreateSongRequest) (*dto.SongResponse, error) {
	// 既に存在するか確認する
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

	// iTunes ID を処理する。一つだけなら自動で Primary にする
	if len(req.ItunesIds) > 0 {
		for i, itunesItem := range req.ItunesIds {
			// 一つだけなら Primary にし、複数ならリクエストの設定に従う
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

// Update は楽曲を更新する。
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
	// OriginalArtistReading は req から取らない。読みはアーティストのもので、
	// 楽曲側の列は syncArtistMapping が artists から写す
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

	// iTunes ID は古い関連を削除してから新しいものを追加する
	if len(req.ItunesIds) > 0 {
		// 既存の iTunes 関連を削除する
		_ = s.songItunesRepo.DeleteBySongID(song.ID)

		// 新しい iTunes 関連を追加する
		for _, item := range req.ItunesIds {
			songItunes := &models.SongITunes{
				SongID:    song.ID,
				ITunesID:  item.ItunesID,
				IsPrimary: item.IsPrimary,
			}
			_ = s.songItunesRepo.Create(songItunes)
		}
	} else {
		// iTunes ID が送られていない場合も既存の関連を削除する
		_ = s.songItunesRepo.DeleteBySongID(song.ID)
	}

	count, _ := s.songRepo.GetPerformanceCount(song.ID)
	resp := s.toSongResponse(*song, count)
	return &resp, nil
}

// Delete は楽曲を削除する。
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
// access は使わない（楽曲は配信に紐付かないので秘匿の対象外）。interface を揃えるために受け取る。
func (s *SongService) GetEditableFields(id uuid.UUID, _ repository.ViewerAccess) (map[string]string, string, error) {
	song, err := s.songRepo.FindByID(id)
	if err != nil {
		return nil, "", err
	}
	if song == nil {
		return nil, "", nil
	}
	// 原曲アーティストの読みはここに無い。読みの持ち主は artists で、
	// 楽曲側の列はその写し ── 楽曲の提案として通すと artists が古いまま残り、
	// 次にアーティスト側を触った時点で黙って巻き戻る。
	// 読みを直す提案は artist を対象に出す
	fields := map[string]string{
		"name":            song.Name,
		"name_reading":    nullStr(song.NameReading),
		"original_artist": song.OriginalArtist,
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
	if err := s.songRepo.Update(song); err != nil {
		return fmt.Errorf("update song: %w", err)
	}
	s.syncArtistMapping(song)
	return nil
}

// MergeSongs は統合元楽曲を統合先楽曲へまとめる。
//
// 統合は「この 2 つは同じ曲だ」という人の確定した判断なので、照合の学習にも使う。
// 統合元の表記は消える前に控えておく必要がある。
//
// なおここで学習するのは**楽曲の別表記だけ**で、アーティストの別名義は作らない。
// 曲名が同じで統合されたからといって 2 人が同一人物とは限らない（カバー曲を
// 同じ楽曲として畳んだ場合など）。楽曲の別表記はその 1 組にしか効かないので
// 安全側だが、アーティストの別名義は全楽曲に効くため、より強い根拠を要求する。
func (s *SongService) MergeSongs(sourceSongID, targetSongID uuid.UUID) error {
	var source, target *models.Song
	if s.matchService != nil {
		source, _ = s.songRepo.FindByID(sourceSongID)
		target, _ = s.songRepo.FindByID(targetSongID)
	}

	if err := s.songRepo.MergeSong(sourceSongID, targetSongID); err != nil {
		return err
	}

	if s.matchService != nil {
		s.matchService.OnSongMerged(source, target)
		// 統合が済んだので、この 2 曲に紐づく未処理の統合候補は畳む。
		// 残しておくと解決済みの組が一覧に出続ける。
		if err := s.matchService.ResolveCandidatesForMergedSong(sourceSongID, targetSongID); err != nil {
			logger.Warnf("resolve merge candidates failed (%s → %s): %v", sourceSongID, targetSongID, err)
		}
	}
	return nil
}

// SearchSimilar は類似楽曲を検索する（AI 正規化候補用）。
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

// toSongResponse は Model を DTO に変換する（単件取得時に iTunes 関連を都度取得）。
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

// buildSongResponse は準備済みのデータから DTO を組み立てる（一括一覧と単件取得で共用）。
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

// toSongPerformanceResponse は歌唱を DTO に変換する。
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

	// タグを変換する
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

	// 歌手を変換する
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
