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
	ErrChannelMetadataManagedByHolodex = errors.New("Holodex 登録済みチャンネルは手動編集できません")
	ErrChannelNameRequired             = errors.New("チャンネル名は必須です")
	// ErrInvalidMembersOnlyPolicy は方針の値が不正。DB エラーと区別して 400 を返すために要る。
	ErrInvalidMembersOnlyPolicy = errors.New("不明な方針です")
)

type ChannelService struct {
	channelRepo *repository.ChannelRepository
	streamRepo  *repository.StreamRepository
	perfRepo    *repository.PerformanceRepository
}

func NewChannelService(
	channelRepo *repository.ChannelRepository,
	streamRepo *repository.StreamRepository,
	perfRepo *repository.PerformanceRepository,
) *ChannelService {
	return &ChannelService{
		channelRepo: channelRepo,
		streamRepo:  streamRepo,
		perfRepo:    perfRepo,
	}
}

// GetAll はすべての歌手を取得する。includeHidden は content:edit を持つ場合のみ true を渡す。
func (s *ChannelService) GetAll(page, limit int, sort, dir string, includeHidden, includeOperational bool) (*dto.ChannelListResponse, error) {
	offset := (page - 1) * limit

	channels, total, hidden, err := s.channelRepo.FindAll(limit, offset, sort, dir, includeHidden)
	if err != nil {
		return nil, fmt.Errorf("get singers: %w", err)
	}

	// DTO に変換する
	counts, err := s.membersOnlyCounts(includeOperational)
	if err != nil {
		return nil, err
	}
	channelResponses := make([]dto.ChannelResponse, len(channels))
	for i, channel := range channels {
		channelResponses[i] = s.toChannelResponseFor(channel, includeOperational, counts)
	}

	totalPages := (total + limit - 1) / limit

	resp := &dto.ChannelListResponse{
		Channels: channelResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}
	if includeHidden {
		resp.HiddenTotal = &hidden
	}
	return resp, nil
}

// GetGrouped は事務所別のチャンネル一覧を返す（ページングなし）。
// 所属なしのチャンネルは最後の「所属なし」グループにまとめる。
func (s *ChannelService) GetGrouped(includeHidden, includeOperational bool) (*dto.ChannelGroupListResponse, error) {
	channels, err := s.channelRepo.FindAllGrouped()
	if err != nil {
		return nil, fmt.Errorf("get singers grouped: %w", err)
	}
	// 非表示は事務所の組へ混ぜず、別の区として名前順で返す（issue #65）。
	// **権限が無ければ引かない** ── viewer には非表示の行がそもそも届かないこと。
	var hiddenChannels []models.Channel
	if includeHidden {
		if hiddenChannels, err = s.channelRepo.FindHiddenByName(); err != nil {
			return nil, fmt.Errorf("get hidden singers: %w", err)
		}
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
	groups := []dto.ChannelGroupResponse{}
	for _, channel := range channels {
		org, display := "", ""
		if eff := channel.EffectiveOrganization(); eff.Valid && !channel.OrganizationUnaffil {
			org = strings.TrimSpace(eff.String)
			display = org // organizations に行が無い場合の保険
			if channel.OrganizationName.Valid {
				display = channel.OrganizationName.String
			}
		}
		if len(groups) == 0 || groups[len(groups)-1].Organization != org {
			groups = append(groups, dto.ChannelGroupResponse{Organization: org, DisplayName: display})
		}
		last := &groups[len(groups)-1]
		last.Channels = append(last.Channels, s.toChannelResponseFor(channel, includeOperational, counts))
	}

	resp := &dto.ChannelGroupListResponse{Groups: groups, Total: len(channels) + len(hiddenChannels)}
	if includeHidden {
		hiddenResponses := make([]dto.ChannelResponse, len(hiddenChannels))
		for i, channel := range hiddenChannels {
			hiddenResponses[i] = s.toChannelResponseFor(channel, includeOperational, counts)
		}
		resp.Hidden = &hiddenResponses
	}
	return resp, nil
}

// SetOrganizationOverride は Holodex の分類を手動で上書きする（空文字で解除）。
// Holodex の値は残るので、解除すれば最新の同期結果に戻る。
// 見つからなければ (false, nil) を返す。
func (s *ChannelService) SetOrganizationOverride(id, org string) (bool, error) {
	found, err := s.channelRepo.SetOrganizationOverride(id, org)
	if err != nil {
		return false, fmt.Errorf("set organization override: %w", err)
	}
	return found, nil
}

// SetHidden はチャンネル一覧での表示/非表示を切り替える。
// 見つからなければ (false, nil) を返す。
func (s *ChannelService) SetHidden(id string, hidden bool) (bool, error) {
	found, err := s.channelRepo.SetHidden(id, hidden)
	if err != nil {
		return false, fmt.Errorf("set singer hidden: %w", err)
	}
	return found, nil
}

// SetAutoFill は自動処理の対象かを切り替える。戻り値は対象が存在したか。
func (s *ChannelService) SetAutoFill(id string, enabled bool) (bool, error) {
	return s.channelRepo.SetAutoFill(id, enabled)
}

// ListAutoFillTargets は自動処理が有効なチャンネルを返す（運用の一覧用）。
func (s *ChannelService) ListAutoFillTargets() ([]dto.ChannelResponse, error) {
	channels, err := s.channelRepo.FindAutoFillTargets()
	if err != nil {
		return nil, fmt.Errorf("list auto fill targets: %w", err)
	}
	counts, err := s.membersOnlyCounts(true)
	if err != nil {
		return nil, err
	}
	out := make([]dto.ChannelResponse, len(channels))
	for i, sg := range channels {
		out[i] = s.toChannelResponseFor(sg, true, counts)
	}
	return out, nil
}

// SetMembersOnlyPolicy は会限セットリストの公開可否を設定する。
//
// **チャンネル単位なのは、配信主に訊いたときの答えがそうだから。** 「会限の歌単を
// 公開してよいか」への答えはほぼ「全部いい」か「全部だめ」で、配信ごとではない。
// 配信単位の restriction_override は、その方針からの例外を書くために残してある。
func (s *ChannelService) SetMembersOnlyPolicy(id, policy string) (bool, error) {
	switch policy {
	case "", MembersOnlyAllow, MembersOnlyDeny:
	default:
		return false, fmt.Errorf("%w: %s", ErrInvalidMembersOnlyPolicy, policy)
	}
	return s.channelRepo.SetMembersOnlyPolicy(id, policy)
}

// Search は歌手を検索する。
func (s *ChannelService) Search(query string, limit int) ([]dto.ChannelResponse, error) {
	if limit <= 0 {
		limit = 10
	}

	channels, err := s.channelRepo.Search(query, limit)
	if err != nil {
		return nil, fmt.Errorf("search singers: %w", err)
	}

	channelResponses := make([]dto.ChannelResponse, len(channels))
	for i, channel := range channels {
		channelResponses[i] = s.toChannelResponse(channel)
	}

	return channelResponses, nil
}

// GetByID は歌手の詳細を取得する。
// includeOperational を立てると、会限の方針など運用の内部情報も載せる（content:edit 用）。
func (s *ChannelService) GetByID(id string, includeOperational bool, access repository.ViewerAccess) (*dto.ChannelDetailResponse, error) {
	channel, err := s.channelRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if channel == nil {
		return nil, nil
	}

	// 統計データを取得する
	streamCount, _ := s.channelRepo.GetStreamCount(id)
	performanceCount, _ := s.channelRepo.GetPerformanceCount(id, access)

	// **本数もここで引く。** 詳細だけ counts を渡さずにいたため、方針を設定する
	// Picker の表示条件（会限を 1 本以上持つ）が常に偽になり、画面から設定できなかった。
	counts, err := s.membersOnlyCounts(includeOperational, id)
	if err != nil {
		return nil, err
	}
	channelResp := s.toChannelResponseFor(*channel, includeOperational, counts)

	return &dto.ChannelDetailResponse{
		ChannelResponse:  channelResp,
		StreamCount:      streamCount,
		PerformanceCount: performanceCount,
	}, nil
}

// UpdateManualMetadata updates metadata for channels that are not managed by Holodex.
func (s *ChannelService) UpdateManualMetadata(id string, req *dto.UpdateChannelRequest) (*dto.ChannelResponse, error) {
	channel, err := s.channelRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if channel == nil {
		return nil, nil
	}
	if channel.MetadataSource == "" {
		channel.MetadataSource = "holodex"
	}
	if channel.MetadataSource == "holodex" {
		return nil, ErrChannelMetadataManagedByHolodex
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrChannelNameRequired
	}

	channel.Name = name
	channel.EnglishName = nullableTrimmedString(req.EnglishName)
	channel.PhotoURL = nullableTrimmedString(req.PhotoURL)
	// 事務所はここでは扱わない。PUT /api/channels/{id}/organization（上書き）が唯一の窓口。

	if err := s.channelRepo.UpdateManualMetadata(channel); err != nil {
		return nil, fmt.Errorf("update singer metadata: %w", err)
	}

	resp := s.toChannelResponse(*channel)
	return &resp, nil
}

// GetStreams は歌手が参加した歌枠を取得する（絞り込み対応）。
func (s *ChannelService) GetStreams(channelID string, page, limit int, processedFilter, hiddenFilter *bool, isEditor bool) (*dto.StreamListResponse, error) {
	offset := (page - 1) * limit

	// 絞り込み条件を組み立てる
	filter := &repository.StreamFilter{
		ProcessedOnly: processedFilter,
		HiddenFilter:  hiddenFilter,
	}

	streams, total, err := s.streamRepo.FindByChannelID(channelID, limit, offset, filter)
	if err != nil {
		return nil, fmt.Errorf("get streams: %w", err)
	}

	// 配信一覧と同じ一括取得を使い、件数に比例したタグ・参加者の問い合わせを避ける。
	streamIDs := make([]string, len(streams))
	for i, stream := range streams {
		streamIDs[i] = stream.ID
	}
	tags, err := s.streamRepo.GetTagsForStreams(streamIDs)
	if err != nil {
		return nil, fmt.Errorf("get stream tags: %w", err)
	}
	participants, _, err := s.streamRepo.GetChannelsForStreams(streamIDs)
	if err != nil {
		return nil, fmt.Errorf("get stream singers: %w", err)
	}
	streamResponses := make([]dto.StreamResponse, len(streams))
	for i, stream := range streams {
		streamResponses[i] = s.toStreamResponse(stream, tags[stream.ID], participants[stream.ID], isEditor)
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
func (s *ChannelService) GetPerformances(channelID string, page, limit int, sort, dir string, access repository.ViewerAccess) (*dto.ChannelPerformanceListResponse, error) {
	offset := (page - 1) * limit

	// 先に歌手情報を取得する
	channel, err := s.channelRepo.FindByID(channelID)
	if err != nil {
		return nil, fmt.Errorf("get singer: %w", err)
	}
	if channel == nil {
		return nil, nil
	}

	// 歌手ページは発見面。秘匿された配信の歌唱は出さない。
	performances, total, err := s.perfRepo.FindBySingerID(channelID, limit, offset, sort, dir, access)
	if err != nil {
		return nil, fmt.Errorf("get performances: %w", err)
	}

	// DTO に変換する
	perfResponses := make([]dto.SongPerformanceResponse, len(performances))
	for i, perf := range performances {
		perfResponses[i] = s.toPerformanceResponse(perf)
	}

	totalPages := (total + limit - 1) / limit

	return &dto.ChannelPerformanceListResponse{
		Channel:      s.toChannelResponse(*channel),
		Performances: perfResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// toChannelResponse は Model を DTO に変換する。
func (s *ChannelService) toChannelResponse(channel models.Channel) dto.ChannelResponse {
	resp := dto.ChannelResponse{
		ID:              channel.ID,
		Name:            channel.Name,
		MetadataSource:  channel.MetadataSource,
		CanEditMetadata: channel.MetadataSource != "holodex",
		IsHidden:        channel.IsHidden,
		CreatedAt:       channel.CreatedAt,
		UpdatedAt:       channel.UpdatedAt,
	}
	if resp.MetadataSource == "" {
		resp.MetadataSource = "holodex"
		resp.CanEditMetadata = false
	}

	if channel.EnglishName.Valid {
		resp.EnglishName = &channel.EnglishName.String
	}
	if channel.PhotoURL.Valid {
		resp.PhotoURL = &channel.PhotoURL.String
	}
	if channel.Organization.Valid {
		resp.OrganizationHolodex = &channel.Organization.String
	}
	if channel.OrganizationOverride.Valid {
		resp.OrganizationOverride = &channel.OrganizationOverride.String
	}

	if eff := channel.EffectiveOrganization(); eff.Valid {
		key := eff.String
		resp.Organization = &key
		// 「所属なし」を意味する分類（Independents など）は事務所名として出さない。
		// バッジに出すと、見出しが「所属なし」なのにバッジは別名という矛盾になる。
		if !channel.OrganizationUnaffil {
			// 表示名は organizations 側。取り込み直後などで行が無い場合は key を出す
			// （空欄にすると「所属なし」に見えてしまうため）。
			name := key
			if channel.OrganizationName.Valid {
				name = channel.OrganizationName.String
			}
			resp.OrganizationName = &name
		}
	}

	// **方針は載せない。** 「配信主に訊いたか」「断られたか」は運用の内部情報で、
	// Channel の GET は未認証で通る。載せると第三者が一覧をページングして
	// 「どのチャンネルに訊いて断られたか」を集められる。
	// 編集画面へ返すのは toChannelResponseFor（includeOperational=true）。
	return resp
}

// toChannelResponseFor は権限に応じて運用の内部情報を足す。
//
// **counts を省略しないこと。** nil map の読み取りは 0 を返し、0 は omitempty で
// 応答から消えるので、「会限を持たないチャンネル」と区別が付かない。実際それで
// 詳細の Picker が出なくなっていた。
func (s *ChannelService) toChannelResponseFor(channel models.Channel, includeOperational bool, counts map[string]int) dto.ChannelResponse {
	resp := s.toChannelResponse(channel)
	if !includeOperational {
		return resp
	}
	if channel.MembersOnlyPolicy.Valid {
		p := channel.MembersOnlyPolicy.String
		resp.MembersOnlyPolicy = &p
	}
	resp.MembersOnlyStreamCount = counts[channel.ID]
	enabled := channel.AutoFillEnabled
	resp.AutoFillEnabled = &enabled
	return resp
}

// membersOnlyCounts は所有者ごとの会限本数を引く（権限が無ければ引かない）。
// **権限が無いときにクエリごと省く**のは、応答に載らない値のために
// 未認証のリクエストで毎回 1 クエリ走らせないため。
func (s *ChannelService) membersOnlyCounts(includeOperational bool, onlyIDs ...string) (map[string]int, error) {
	if !includeOperational {
		return nil, nil
	}
	counts, err := s.channelRepo.CountMembersOnlyByOwner(onlyIDs...)
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
// toStreamResponse は歌手ページ用の変換。**StreamService にも同名の関数がある** ──
// あちらは解析結果とチャンネル所有者まで組み立てるが、両方が同じ DTO を返すので
// 載せる／載せないの判断は 2 か所に要る。片方だけ直すと権限の穴になる。
//
// isEditor は処理済みフラグを載せるか（content:edit のときだけ）。
func (s *ChannelService) toStreamResponse(stream models.Stream, tags []models.StreamTag, participants []models.Channel, isEditor bool) dto.StreamResponse {
	var processed *bool
	if isEditor {
		processed = &stream.IsProcessed
	}
	resp := dto.StreamResponse{
		ID:           stream.ID,
		Title:        stream.Title,
		StreamDate:   stream.StreamDate.Format(time.RFC3339),
		IsProcessed:  processed,
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
	resp.Participants = make([]dto.ChannelResponse, len(participants))
	for i, channel := range participants {
		resp.Participants[i] = s.toChannelResponse(channel)
	}

	return resp
}

// toPerformanceResponse は歌唱を DTO に変換する。
func (s *ChannelService) toPerformanceResponse(perf repository.PerformanceWithDetails) dto.SongPerformanceResponse {
	resp := dto.SongPerformanceResponse{
		IsRestricted:   perf.IsRestricted,
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
	resp.Singers = make([]dto.ChannelResponse, len(perf.Singers))
	for i, singer := range perf.Singers {
		resp.Singers[i] = s.toChannelResponse(singer)
	}

	return resp
}
