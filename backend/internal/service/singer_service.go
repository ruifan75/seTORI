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
	// ErrInvalidMembersOnlyPolicy は方針の値が不正。DB エラーと区別して 400 を返すために要る。
	ErrInvalidMembersOnlyPolicy = errors.New("不明な方針です")
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

// GetAll はすべての歌手を取得する。includeHidden は content:edit を持つ場合のみ true を渡す。
func (s *SingerService) GetAll(page, limit int, sort, dir string, includeHidden, includeOperational bool) (*dto.SingerListResponse, error) {
	offset := (page - 1) * limit

	singers, total, err := s.singerRepo.FindAll(limit, offset, sort, dir, includeHidden)
	if err != nil {
		return nil, fmt.Errorf("get singers: %w", err)
	}

	// DTO に変換する
	counts, err := s.membersOnlyCounts(includeOperational)
	if err != nil {
		return nil, err
	}
	singerResponses := make([]dto.SingerResponse, len(singers))
	for i, singer := range singers {
		singerResponses[i] = s.toSingerResponseFor(singer, includeOperational, counts)
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
func (s *SingerService) GetGrouped(includeHidden, includeOperational bool) (*dto.SingerGroupListResponse, error) {
	singers, err := s.singerRepo.FindAllGrouped(includeHidden)
	if err != nil {
		return nil, fmt.Errorf("get singers grouped: %w", err)
	}

	counts, err := s.membersOnlyCounts(includeOperational)
	if err != nil {
		return nil, err
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
		last.Singers = append(last.Singers, s.toSingerResponseFor(singer, includeOperational, counts))
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

// SetMembersOnlyPolicy は会限セットリストの公開可否を設定する。
//
// **チャンネル単位なのは、配信主に訊いたときの答えがそうだから。** 「会限の歌単を
// 公開してよいか」への答えはほぼ「全部いい」か「全部だめ」で、配信ごとではない。
// 配信単位の restriction_override は、その方針からの例外を書くために残してある。
func (s *SingerService) SetMembersOnlyPolicy(id, policy string) (bool, error) {
	switch policy {
	case "", MembersOnlyAllow, MembersOnlyDeny:
	default:
		return false, fmt.Errorf("%w: %s", ErrInvalidMembersOnlyPolicy, policy)
	}
	return s.singerRepo.SetMembersOnlyPolicy(id, policy)
}

// Search は歌手を検索する。
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

// GetByID は歌手の詳細を取得する。
// includeOperational を立てると、会限の方針など運用の内部情報も載せる（content:edit 用）。
func (s *SingerService) GetByID(id string, includeOperational bool) (*dto.SingerDetailResponse, error) {
	singer, err := s.singerRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if singer == nil {
		return nil, nil
	}

	// 統計データを取得する
	streamCount, _ := s.singerRepo.GetStreamCount(id)
	performanceCount, _ := s.singerRepo.GetPerformanceCount(id)

	singerResp := s.toSingerResponse(*singer)
	if includeOperational {
		singerResp = s.toSingerResponseForEditor(*singer)
	}

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

// GetStreams は歌手が参加した歌枠を取得する（絞り込み対応）。
func (s *SingerService) GetStreams(singerID string, page, limit int, processedFilter, hiddenFilter *bool) (*dto.StreamListResponse, error) {
	offset := (page - 1) * limit

	// 絞り込み条件を組み立てる
	filter := &repository.StreamFilter{
		ProcessedOnly: processedFilter,
		HiddenFilter:  hiddenFilter,
	}

	streams, total, err := s.streamRepo.FindBySingerID(singerID, limit, offset, filter)
	if err != nil {
		return nil, fmt.Errorf("get streams: %w", err)
	}

	// DTO に変換する
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

// GetPerformances は歌手のすべての歌唱記録を取得する。
func (s *SingerService) GetPerformances(singerID string, page, limit int, sort, dir string) (*dto.SingerPerformanceListResponse, error) {
	offset := (page - 1) * limit

	// 先に歌手情報を取得する
	singer, err := s.singerRepo.FindByID(singerID)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if singer == nil {
		return nil, nil
	}

	// 歌手ページは発見面。秘匿された配信の歌唱は出さない。
	performances, total, err := s.perfRepo.FindBySingerID(singerID, limit, offset, sort, dir, repository.PublicAccess)
	if err != nil {
		return nil, fmt.Errorf("get performances: %w", err)
	}

	// DTO に変換する
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

// toSingerResponse は Model を DTO に変換する。
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

	// **方針は載せない。** 「配信主に訊いたか」「断られたか」は運用の内部情報で、
	// Singer の GET は未認証で通る。載せると第三者が一覧をページングして
	// 「どのチャンネルに訊いて断られたか」を集められる。
	// 編集画面へ返すのは toSingerResponseForEditor。
	return resp
}

// toSingerResponseForEditor は content:edit 向け。運用の内部情報を足す。
func (s *SingerService) toSingerResponseForEditor(singer models.Singer) dto.SingerResponse {
	return s.toSingerResponseFor(singer, true, nil)
}

// toSingerResponseFor は権限に応じて運用の内部情報を足す。
// counts が nil でも方針は載る（詳細のように 1 件だけ返す経路で使う）。
func (s *SingerService) toSingerResponseFor(singer models.Singer, includeOperational bool, counts map[string]int) dto.SingerResponse {
	resp := s.toSingerResponse(singer)
	if !includeOperational {
		return resp
	}
	if singer.MembersOnlyPolicy.Valid {
		p := singer.MembersOnlyPolicy.String
		resp.MembersOnlyPolicy = &p
	}
	resp.MembersOnlyStreamCount = counts[singer.ID]
	return resp
}

// membersOnlyCounts は所有者ごとの会限本数を引く（権限が無ければ引かない）。
// **権限が無いときにクエリごと省く**のは、応答に載らない値のために
// 未認証のリクエストで毎回 1 クエリ走らせないため。
func (s *SingerService) membersOnlyCounts(includeOperational bool) (map[string]int, error) {
	if !includeOperational {
		return nil, nil
	}
	counts, err := s.singerRepo.CountMembersOnlyByOwner()
	if err != nil {
		return nil, fmt.Errorf("count members only streams: %w", err)
	}
	return counts, nil
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

// toStreamResponse は Model を DTO に変換する。
func (s *SingerService) toStreamResponse(stream models.Stream, tags []models.StreamTag, participants []models.Singer) dto.StreamResponse {
	resp := dto.StreamResponse{
		ID:           stream.ID,
		Title:        stream.Title,
		StreamDate:   stream.StreamDate.Format(time.RFC3339),
		IsProcessed:  stream.IsProcessed,
		IsHidden:     stream.IsHidden,
		IsRestricted: stream.IsRestrictedEffective,
		CreatedAt:    stream.CreatedAt,
		UpdatedAt:    stream.UpdatedAt,
	}

	if stream.DurationSeconds.Valid {
		resp.DurationSeconds = &stream.DurationSeconds.Int32
	}
	if stream.ThumbnailURL.Valid {
		resp.ThumbnailURL = &stream.ThumbnailURL.String
	}

	// タグを変換する
	resp.Tags = make([]dto.StreamTagResponse, len(tags))
	for i, tag := range tags {
		resp.Tags[i] = dto.StreamTagResponse{
			ID:          tag.ID,
			DisplayName: tag.DisplayName,
			Color:       tag.Color,
		}
	}

	// 参加者を変換する
	resp.Participants = make([]dto.SingerResponse, len(participants))
	for i, singer := range participants {
		resp.Participants[i] = s.toSingerResponse(singer)
	}

	return resp
}

// toPerformanceResponse は歌唱を DTO に変換する。
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
		resp.Singers[i] = s.toSingerResponse(singer)
	}

	return resp
}
