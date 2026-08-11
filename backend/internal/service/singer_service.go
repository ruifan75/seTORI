package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

var (
	ErrSingerMetadataManagedByHolodex = errors.New("Holodex 登録済みチャンネルは手動編集できません")
	ErrSingerNameRequired             = errors.New("チャンネル名は必須です")
)

type SingerService struct {
	singerRepo *repository.SingerRepository
	streamRepo *repository.StreamRepository
	perfRepo   *repository.PerformanceRepository
}

func NewSingerService(
	singerRepo *repository.SingerRepository,
	streamRepo *repository.StreamRepository,
	perfRepo *repository.PerformanceRepository,
) *SingerService {
	return &SingerService{
		singerRepo: singerRepo,
		streamRepo: streamRepo,
		perfRepo:   perfRepo,
	}
}

// GetAll 取得所有演唱者。includeHidden は content:edit を持つ場合のみ true を渡す。
func (s *SingerService) GetAll(page, limit int, sort, dir string, includeHidden bool) (*dto.SingerListResponse, error) {
	offset := (page - 1) * limit

	singers, total, err := s.singerRepo.FindAll(limit, offset, sort, dir, includeHidden)
	if err != nil {
		return nil, fmt.Errorf("get singers: %w", err)
	}

	// 轉換為 DTO
	singerResponses := make([]dto.SingerResponse, len(singers))
	for i, singer := range singers {
		singerResponses[i] = s.toSingerResponse(singer)
	}

	totalPages := (total + limit - 1) / limit

	return &dto.SingerListResponse{
		Singers: singerResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// GetGrouped は事務所別のチャンネル一覧を返す（ページングなし）。
// 所属なしのチャンネルは最後の「所属なし」グループにまとめる。
func (s *SingerService) GetGrouped(includeHidden bool) (*dto.SingerGroupListResponse, error) {
	singers, err := s.singerRepo.FindAllGrouped(includeHidden)
	if err != nil {
		return nil, fmt.Errorf("get singers grouped: %w", err)
	}

	// SQL 側で「事務所 → 名前」順に並んでいるので、隣接する同じ事務所をまとめるだけでよい。
	// 束ねる鍵は key（表示名は重複しうるし、直された瞬間にグループが割れるため）。
	//
	// 「所属なし」だけは複数の key が 1 つの組になる：事務所が未設定のもの（NULL）と、
	// Holodex の Independents のように無所属を意味する分類（is_unaffiliated）。
	// 別の事実なので値は潰さないが、見る側にとっては同じ「事務所に属さない人たち」なので
	// 空文字の組にまとめる。SQL 側で末尾に固めてあるので隣接判定のままで足りる。
	groups := []dto.SingerGroupResponse{}
	for _, singer := range singers {
		org, display := "", ""
		if eff := singer.EffectiveOrganization(); eff.Valid && !singer.OrganizationUnaffil {
			org = strings.TrimSpace(eff.String)
			display = org // organizations に行が無い場合の保険
			if singer.OrganizationName.Valid {
				display = singer.OrganizationName.String
			}
		}
		if len(groups) == 0 || groups[len(groups)-1].Organization != org {
			groups = append(groups, dto.SingerGroupResponse{Organization: org, DisplayName: display})
		}
		last := &groups[len(groups)-1]
		last.Singers = append(last.Singers, s.toSingerResponse(singer))
	}

	return &dto.SingerGroupListResponse{Groups: groups, Total: len(singers)}, nil
}

// SetOrganizationOverride は Holodex の分類を手動で上書きする（空文字で解除）。
// Holodex の値は残るので、解除すれば最新の同期結果に戻る。
// 見つからなければ (false, nil) を返す。
func (s *SingerService) SetOrganizationOverride(id, org string) (bool, error) {
	found, err := s.singerRepo.SetOrganizationOverride(id, org)
	if err != nil {
		return false, fmt.Errorf("set organization override: %w", err)
	}
	return found, nil
}

// SetHidden はチャンネル一覧での表示/非表示を切り替える。
// 見つからなければ (false, nil) を返す。
func (s *SingerService) SetHidden(id string, hidden bool) (bool, error) {
	found, err := s.singerRepo.SetHidden(id, hidden)
	if err != nil {
		return false, fmt.Errorf("set singer hidden: %w", err)
	}
	return found, nil
}

// Search 搜尋演唱者
func (s *SingerService) Search(query string, limit int) ([]dto.SingerResponse, error) {
	if limit <= 0 {
		limit = 10
	}

	singers, err := s.singerRepo.Search(query, limit)
	if err != nil {
		return nil, fmt.Errorf("search singers: %w", err)
	}

	singerResponses := make([]dto.SingerResponse, len(singers))
	for i, singer := range singers {
		singerResponses[i] = s.toSingerResponse(singer)
	}

	return singerResponses, nil
}

// GetByID 取得演唱者詳情
func (s *SingerService) GetByID(id string) (*dto.SingerDetailResponse, error) {
	singer, err := s.singerRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if singer == nil {
		return nil, nil
	}

	// 取得統計資料
	streamCount, _ := s.singerRepo.GetStreamCount(id)
	performanceCount, _ := s.singerRepo.GetPerformanceCount(id)

	singerResp := s.toSingerResponse(*singer)

	return &dto.SingerDetailResponse{
		SingerResponse:   singerResp,
		StreamCount:      streamCount,
		PerformanceCount: performanceCount,
	}, nil
}

// UpdateManualMetadata updates metadata for channels that are not managed by Holodex.
func (s *SingerService) UpdateManualMetadata(id string, req *dto.UpdateSingerRequest) (*dto.SingerResponse, error) {
	singer, err := s.singerRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if singer == nil {
		return nil, nil
	}
	if singer.MetadataSource == "" {
		singer.MetadataSource = "holodex"
	}
	if singer.MetadataSource == "holodex" {
		return nil, ErrSingerMetadataManagedByHolodex
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrSingerNameRequired
	}

	singer.Name = name
	singer.EnglishName = nullableTrimmedString(req.EnglishName)
	singer.PhotoURL = nullableTrimmedString(req.PhotoURL)
	// 事務所はここでは扱わない。PUT /api/singers/{id}/organization（上書き）が唯一の窓口。

	if err := s.singerRepo.UpdateManualMetadata(singer); err != nil {
		return nil, fmt.Errorf("update singer metadata: %w", err)
	}

	resp := s.toSingerResponse(*singer)
	return &resp, nil
}

// GetStreams 取得演唱者參與的歌回（支援篩選）
func (s *SingerService) GetStreams(singerID string, page, limit int, processedFilter, hiddenFilter *bool) (*dto.StreamListResponse, error) {
	offset := (page - 1) * limit

	// 建構篩選條件
	filter := &repository.StreamFilter{
		ProcessedOnly: processedFilter,
		HiddenFilter:  hiddenFilter,
	}

	streams, total, err := s.streamRepo.FindBySingerID(singerID, limit, offset, filter)
	if err != nil {
		return nil, fmt.Errorf("get streams: %w", err)
	}

	// 轉換為 DTO
	streamResponses := make([]dto.StreamResponse, len(streams))
	for i, stream := range streams {
		tags, _ := s.streamRepo.GetTags(stream.ID)
		participants, _ := s.streamRepo.GetSingers(stream.ID)
		streamResponses[i] = s.toStreamResponse(stream, tags, participants)
	}

	totalPages := (total + limit - 1) / limit

	return &dto.StreamListResponse{
		Streams: streamResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// GetPerformances 取得演唱者的所有演出記錄
func (s *SingerService) GetPerformances(singerID string, page, limit int, sort, dir string) (*dto.SingerPerformanceListResponse, error) {
	offset := (page - 1) * limit

	// 先取得演唱者資訊
	singer, err := s.singerRepo.FindByID(singerID)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if singer == nil {
		return nil, nil
	}

	performances, total, err := s.perfRepo.FindBySingerID(singerID, limit, offset, sort, dir)
	if err != nil {
		return nil, fmt.Errorf("get performances: %w", err)
	}

	// 轉換為 DTO
	perfResponses := make([]dto.SongPerformanceResponse, len(performances))
	for i, perf := range performances {
		perfResponses[i] = s.toPerformanceResponse(perf)
	}

	totalPages := (total + limit - 1) / limit

	return &dto.SingerPerformanceListResponse{
		Singer:       s.toSingerResponse(*singer),
		Performances: perfResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// toSingerResponse 轉換 Model 到 DTO
func (s *SingerService) toSingerResponse(singer models.Singer) dto.SingerResponse {
	resp := dto.SingerResponse{
		ID:              singer.ID,
		Name:            singer.Name,
		MetadataSource:  singer.MetadataSource,
		CanEditMetadata: singer.MetadataSource != "holodex",
		IsHidden:        singer.IsHidden,
		CreatedAt:       singer.CreatedAt,
		UpdatedAt:       singer.UpdatedAt,
	}
	if resp.MetadataSource == "" {
		resp.MetadataSource = "holodex"
		resp.CanEditMetadata = false
	}

	if singer.EnglishName.Valid {
		resp.EnglishName = &singer.EnglishName.String
	}
	if singer.PhotoURL.Valid {
		resp.PhotoURL = &singer.PhotoURL.String
	}
	if singer.Organization.Valid {
		resp.OrganizationHolodex = &singer.Organization.String
	}
	if singer.OrganizationOverride.Valid {
		resp.OrganizationOverride = &singer.OrganizationOverride.String
	}

	if eff := singer.EffectiveOrganization(); eff.Valid {
		key := eff.String
		resp.Organization = &key
		// 「所属なし」を意味する分類（Independents など）は事務所名として出さない。
		// バッジに出すと、見出しが「所属なし」なのにバッジは別名という矛盾になる。
		if !singer.OrganizationUnaffil {
			// 表示名は organizations 側。取り込み直後などで行が無い場合は key を出す
			// （空欄にすると「所属なし」に見えてしまうため）。
			name := key
			if singer.OrganizationName.Valid {
				name = singer.OrganizationName.String
			}
			resp.OrganizationName = &name
		}
	}

	return resp
}

func nullableTrimmedString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: trimmed, Valid: true}
}

// toStreamResponse 轉換 Model 到 DTO
func (s *SingerService) toStreamResponse(stream models.Stream, tags []models.StreamTag, participants []models.Singer) dto.StreamResponse {
	resp := dto.StreamResponse{
		ID:          stream.ID,
		Title:       stream.Title,
		StreamDate:  stream.StreamDate.Format(time.RFC3339),
		IsProcessed: stream.IsProcessed,
		IsHidden:    stream.IsHidden,
		CreatedAt:   stream.CreatedAt,
		UpdatedAt:   stream.UpdatedAt,
	}

	if stream.DurationSeconds.Valid {
		resp.DurationSeconds = &stream.DurationSeconds.Int32
	}
	if stream.ThumbnailURL.Valid {
		resp.ThumbnailURL = &stream.ThumbnailURL.String
	}
	if stream.VisibilityOverride.Valid {
		override := stream.VisibilityOverride.Bool
		resp.VisibilityOverride = &override
	}

	// 轉換標籤
	resp.Tags = make([]dto.StreamTagResponse, len(tags))
	for i, tag := range tags {
		resp.Tags[i] = dto.StreamTagResponse{
			ID:          tag.ID,
			DisplayName: tag.DisplayName,
			Color:       tag.Color,
		}
	}

	// 轉換參與者
	resp.Participants = make([]dto.SingerResponse, len(participants))
	for i, singer := range participants {
		resp.Participants[i] = s.toSingerResponse(singer)
	}

	return resp
}

// toPerformanceResponse 轉換演出到 DTO
func (s *SingerService) toPerformanceResponse(perf repository.PerformanceWithDetails) dto.SongPerformanceResponse {
	resp := dto.SongPerformanceResponse{
		ID:             perf.ID,
		StreamID:       perf.StreamID,
		StreamTitle:    perf.StreamTitle,
		StreamDate:     perf.StreamDate,
		StartSeconds:   perf.StartSeconds,
		EndSeconds:     perf.EndSeconds,
		YouTubeURL:     fmt.Sprintf("https://www.youtube.com/watch?v=%s&t=%d", perf.StreamID, perf.StartSeconds),
		CreatedAt:      perf.CreatedAt,
		SongName:       perf.SongName,
		SongID:         perf.SongID,
		OriginalArtist: perf.OriginalArtist,
		Artists:        toArtistReferences(perf.Artists),
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
		resp.Singers[i] = s.toSingerResponse(singer)
	}

	return resp
}
