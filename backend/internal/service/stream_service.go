package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

type StreamService struct {
	streamRepo *repository.StreamRepository
	resolver   *NormalizationService // 読み取り時の照合（保存はしない）
	perfRepo   *repository.PerformanceRepository
}

func NewStreamService(streamRepo *repository.StreamRepository, perfRepo *repository.PerformanceRepository) *StreamService {
	return &StreamService{
		streamRepo: streamRepo,
		perfRepo:   perfRepo,
	}
}

// SetResolver は照合を読み取り時に行うための依存を渡す。
// 照合結果は保存しないので、画面に出す直前にここで計算する。
func (s *StreamService) SetResolver(n *NormalizationService) { s.resolver = n }

// ApplyTagRulesToAll は全配信にタイトル自動タグ付けルールを適用し、追加されたタグ数を返す。
func (s *StreamService) ApplyTagRulesToAll() (int64, error) {
	return s.streamRepo.ApplyTagRulesToAll()
}

// SearchByTitle はタイトル部分一致で配信を検索する（グローバル検索用）。
func (s *StreamService) SearchByTitle(query string, limit int) ([]dto.SearchStreamItem, error) {
	streams, err := s.streamRepo.SearchByTitle(query, limit)
	if err != nil {
		return nil, err
	}
	items := make([]dto.SearchStreamItem, 0, len(streams))
	for _, st := range streams {
		item := dto.SearchStreamItem{
			ID:          st.ID,
			Title:       st.Title,
			StreamDate:  st.StreamDate,
			IsProcessed: st.IsProcessed,
			IsHidden:    st.IsHidden,
		}
		if st.ThumbnailURL.Valid {
			item.ThumbnailURL = &st.ThumbnailURL.String
		}
		items = append(items, item)
	}
	return items, nil
}

// Exists は配信が登録済みかを返す（グローバル検索の video ID 照合用）。
func (s *StreamService) Exists(id string) (bool, error) {
	st, err := s.streamRepo.FindByID(id)
	if err != nil {
		return false, err
	}
	return st != nil, nil
}

// GetAll 取得歌回列表（預設不顯示隱藏的）
func (s *StreamService) GetAll(page, limit int, sort, dir string) (*dto.StreamListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	// 預設不顯示隱藏的歌回
	streams, total, err := s.streamRepo.FindAll(limit, offset, false, sort, dir)
	if err != nil {
		return nil, fmt.Errorf("get streams: %w", err)
	}

	// 批次取得標籤與參與者，避免 N+1
	streamIDs := make([]string, len(streams))
	for i, stream := range streams {
		streamIDs[i] = stream.ID
	}
	tagsMap, err := s.streamRepo.GetTagsForStreams(streamIDs)
	if err != nil {
		return nil, fmt.Errorf("get stream tags: %w", err)
	}
	participantsMap, ownersMap, err := s.streamRepo.GetSingersForStreams(streamIDs)
	if err != nil {
		return nil, fmt.Errorf("get stream singers: %w", err)
	}

	// 轉換為 DTO
	streamResponses := make([]dto.StreamResponse, len(streams))
	for i, stream := range streams {
		streamResponses[i] = s.toStreamResponse(stream, tagsMap[stream.ID], participantsMap[stream.ID], ownersMap[stream.ID], false)
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

// composeStreamList は stream 群にタグ・参加者をバッチで補完し、ページング付きレスポンスを組み立てる。
func (s *StreamService) composeStreamList(streams []models.Stream, total, page, limit int) (*dto.StreamListResponse, error) {
	streamIDs := make([]string, len(streams))
	for i, stream := range streams {
		streamIDs[i] = stream.ID
	}
	tagsMap, err := s.streamRepo.GetTagsForStreams(streamIDs)
	if err != nil {
		return nil, fmt.Errorf("get stream tags: %w", err)
	}
	participantsMap, ownersMap, err := s.streamRepo.GetSingersForStreams(streamIDs)
	if err != nil {
		return nil, fmt.Errorf("get stream singers: %w", err)
	}

	streamResponses := make([]dto.StreamResponse, len(streams))
	for i, stream := range streams {
		streamResponses[i] = s.toStreamResponse(stream, tagsMap[stream.ID], participantsMap[stream.ID], ownersMap[stream.ID], false)
	}

	return &dto.StreamListResponse{
		Streams: streamResponses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: (total + limit - 1) / limit,
		},
	}, nil
}

// GetByTag は指定タグが付いた配信一覧を返す（タグ検索ページ用）。
func (s *StreamService) GetByTag(tagID string, page, limit int) (*dto.StreamListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	streams, total, err := s.streamRepo.FindByTagID(tagID, limit, (page-1)*limit)
	if err != nil {
		return nil, fmt.Errorf("get streams by tag: %w", err)
	}
	return s.composeStreamList(streams, total, page, limit)
}

// SearchStreams は非表示を含め、配信元・参加者・ボーカル・タグを組み合わせて配信を検索する。
func (s *StreamService) SearchStreams(filters models.StreamSearchFilters, page, limit int) (*dto.StreamListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	streams, total, err := s.streamRepo.SearchStreams(filters, limit, (page-1)*limit)
	if err != nil {
		return nil, fmt.Errorf("search streams: %w", err)
	}
	return s.composeStreamList(streams, total, page, limit)
}

// GetPerformancesByTag は指定の演出タグが付いた演出一覧を返す（タグ検索ページ用）。
func (s *StreamService) GetPerformancesByTag(tagID string, page, limit int) (*dto.TagPerformanceListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	performances, total, err := s.perfRepo.FindByTagID(tagID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("get performances by tag: %w", err)
	}

	responses := make([]dto.PerformanceResponse, len(performances))
	for i, perf := range performances {
		resp := s.toPerformanceResponse(perf)
		// タグ検索は配信横断の一覧なので、どの配信かの文脈を補う
		resp.StreamTitle = perf.StreamTitle
		resp.StreamDate = perf.StreamDate
		if perf.ThumbnailURL.Valid {
			resp.ThumbnailURL = &perf.ThumbnailURL.String
		}
		responses[i] = resp
	}

	return &dto.TagPerformanceListResponse{
		Performances: responses,
		Pagination: dto.PaginationResponse{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: (total + limit - 1) / limit,
		},
	}, nil
}

// GetByID 取得歌回詳情（含歌單）
func (s *StreamService) GetByID(id string) (*dto.StreamDetailResponse, error) {
	stream, err := s.streamRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("get stream: %w", err)
	}
	if stream == nil {
		return nil, nil
	}

	tags, _ := s.streamRepo.GetTags(stream.ID)
	participants, _ := s.streamRepo.GetSingers(stream.ID)
	channelOwner, _ := s.streamRepo.GetChannelOwner(stream.ID)
	streamResp := s.toStreamResponse(*stream, tags, participants, channelOwner, true)

	// 取得演出清單
	performances, err := s.perfRepo.FindByStreamID(id)
	if err != nil {
		return nil, fmt.Errorf("get performances: %w", err)
	}

	perfResponses := make([]dto.PerformanceResponse, len(performances))
	for i, perf := range performances {
		perfResponses[i] = s.toPerformanceResponse(perf)
	}

	return &dto.StreamDetailResponse{
		StreamResponse: streamResp,
		Performances:   perfResponses,
	}, nil
}

// toStreamResponse 轉換 Model 到 DTO
// withMatch=true のときだけ DB 照合を当てる。一覧では当てない ── 配信ごとに
// 曲数ぶんの問い合わせが増えるうえ、一覧は照合結果を使っていない。
func (s *StreamService) toStreamResponse(stream models.Stream, tags []models.StreamTag, participants []models.Singer, channelOwner *models.Singer, withMatch bool) dto.StreamResponse {
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
		d := stream.DurationSeconds.Int32
		resp.DurationSeconds = &d
	}
	if stream.ThumbnailURL.Valid {
		resp.ThumbnailURL = &stream.ThumbnailURL.String
	}
	if stream.VisibilityOverride.Valid {
		override := stream.VisibilityOverride.Bool
		resp.VisibilityOverride = &override
	}

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
		resp.Participants[i] = dto.SingerResponse{
			ID:        singer.ID,
			Name:      singer.Name,
			CreatedAt: singer.CreatedAt,
			UpdatedAt: singer.UpdatedAt,
		}
		if singer.EnglishName.Valid {
			resp.Participants[i].EnglishName = &singer.EnglishName.String
		}
		if singer.PhotoURL.Valid {
			resp.Participants[i].PhotoURL = &singer.PhotoURL.String
		}
		if singer.Organization.Valid {
			resp.Participants[i].Organization = &singer.Organization.String
		}
	}

	// 轉換頻道擁有者
	if channelOwner != nil {
		ownerResp := dto.SingerResponse{
			ID:        channelOwner.ID,
			Name:      channelOwner.Name,
			CreatedAt: channelOwner.CreatedAt,
			UpdatedAt: channelOwner.UpdatedAt,
		}
		if channelOwner.EnglishName.Valid {
			ownerResp.EnglishName = &channelOwner.EnglishName.String
		}
		if channelOwner.PhotoURL.Valid {
			ownerResp.PhotoURL = &channelOwner.PhotoURL.String
		}
		if channelOwner.Organization.Valid {
			ownerResp.Organization = &channelOwner.Organization.String
		}
		resp.ChannelOwner = &ownerResp
	}

	// 解析並加入 Holodex timeline 資料（從完整的 Video JSON 中提取 songs）
	if len(stream.HolodexData) > 0 {
		// HolodexData 存的是完整的 holodex.Video 物件
		var video struct {
			Songs []struct {
				Name           string `json:"name"`
				OriginalArtist string `json:"original_artist"`
				ArtURL         string `json:"art"`
				ITunesID       int64  `json:"itunesid"`
				Start          int    `json:"start"`
				End            int    `json:"end"`
			} `json:"songs"`
		}
		if err := json.Unmarshal(stream.HolodexData, &video); err == nil && len(video.Songs) > 0 {
			holodexSongs := make([]dto.SongSuggestion, len(video.Songs))
			for i, song := range video.Songs {
				holodexSongs[i] = dto.SongSuggestion{
					Name:           song.Name,
					OriginalArtist: song.OriginalArtist,
					StartSeconds:   song.Start,
					EndSeconds:     song.End,
					Tags:           []string{},
					SingerIDs:      []string{},
				}
				if song.ArtURL != "" {
					holodexSongs[i].ArtURL = &song.ArtURL
				}
				if song.ITunesID > 0 {
					itunesID := song.ITunesID
					holodexSongs[i].ItunesID = &itunesID
				}
				// 如果沒有結束時間，使用下一首的開始時間
				if holodexSongs[i].EndSeconds == 0 && i+1 < len(video.Songs) {
					holodexSongs[i].EndSeconds = video.Songs[i+1].Start
				}
			}
			resp.HolodexTimelineSongs = holodexSongs
		}
	}

	// 從 comment_songs 載入已分析的快取結果
	if len(stream.CommentSongs) > 0 {
		var commentSongs []dto.CommentSong
		if err := json.Unmarshal(stream.CommentSongs, &commentSongs); err == nil && len(commentSongs) > 0 {
			if withMatch {
				// 照合は保存していないので、ここで今の DB に対して計算する
				s.resolver.ResolveForDisplay(commentSongs)
			}
			resp.CommentTimelineSongs = commentSongs
		}
	}

	if stream.CommentSongsAnalyzedAt.Valid {
		t := stream.CommentSongsAnalyzedAt.Time.Format(time.RFC3339)
		resp.CommentSongsAnalyzedAt = &t
	}

	// comment_raw に留言があれば分析ボタンを有効化できる（comment_songs 未生成でも分析可能）
	resp.HasCommentRaw = len(stream.CommentRaw) > 0 && string(stream.CommentRaw) != "null" && string(stream.CommentRaw) != "[]"

	return resp
}

// toPerformanceResponse 轉換演出到 DTO
func (s *StreamService) toPerformanceResponse(perf repository.PerformanceWithDetails) dto.PerformanceResponse {
	resp := dto.PerformanceResponse{
		ID:             perf.ID,
		StreamID:       perf.StreamID,
		SongID:         perf.SongID,
		SongName:       perf.SongName,
		OriginalArtist: perf.OriginalArtist,
		Artists:        toArtistReferences(perf.Artists),
		StartSeconds:   perf.StartSeconds,
		EndSeconds:     perf.EndSeconds,
		OrderIndex:     perf.OrderIndex,
		YouTubeURL:     fmt.Sprintf("https://www.youtube.com/watch?v=%s&t=%d", perf.StreamID, perf.StartSeconds),
		CreatedAt:      perf.CreatedAt,
		EndSource:      perf.EndSource,
		EndConfirmed:   perf.EndConfirmed,
	}

	if perf.Arts.Valid {
		resp.Arts = &perf.Arts.String
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

// Update 更新歌回資訊
func (s *StreamService) Update(id string, req *dto.UpdateStreamRequest) (*dto.StreamDetailResponse, error) {
	stream, err := s.streamRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, nil
	}

	// visibility_mode が新しい三態 API。古いクライアントの is_hidden は
	// 「手動固定」として互換性を保つ。両方あれば visibility_mode を優先する。
	visibilityRequested := false
	var visibilityOverride *bool
	if req.VisibilityMode != nil {
		visibilityRequested = true
		switch strings.ToLower(strings.TrimSpace(*req.VisibilityMode)) {
		case "auto":
			visibilityOverride = nil
		case "visible":
			visible := false
			visibilityOverride = &visible
		case "hidden":
			hidden := true
			visibilityOverride = &hidden
		default:
			return nil, fmt.Errorf("invalid visibility_mode: must be auto, visible, or hidden")
		}
	} else if req.IsHidden != nil {
		visibilityRequested = true
		override := *req.IsHidden
		visibilityOverride = &override
	}

	// 更新標題
	if req.Title != nil && *req.Title != "" {
		stream.Title = *req.Title
	}

	// 更新日期
	if req.StreamDate != nil && *req.StreamDate != "" {
		// 嘗試解析 RFC3339 格式（完整時間），失敗則嘗試日期格式
		date, err := time.Parse(time.RFC3339, *req.StreamDate)
		if err != nil {
			// 如果不是 RFC3339，嘗試解析為日期格式
			date, err = time.Parse("2006-01-02", *req.StreamDate)
			if err != nil {
				return nil, fmt.Errorf("parse stream date: %w", err)
			}
		}
		stream.StreamDate = date
	}

	// 更新處理狀態
	if req.IsProcessed != nil {
		stream.IsProcessed = *req.IsProcessed
	}

	// 更新 Stream metadata（只更新可變欄位，不重寫大型 JSONB）
	if err := s.streamRepo.UpdateMetadata(id, stream.Title, stream.StreamDate, stream.IsProcessed); err != nil {
		return nil, fmt.Errorf("update stream: %w", err)
	}

	// 更新標籤
	if req.TagIDs != nil {
		if err := s.streamRepo.SetTags(id, req.TagIDs); err != nil {
			return nil, fmt.Errorf("set tags: %w", err)
		}
	}

	// タグ更新後の状態から自動判定を作る。手動固定がある場合は repository 側で
	// 常に override が優先されるため、同期や別の編集で人工修正は戻らない。
	tags, err := s.streamRepo.GetTags(id)
	if err != nil {
		return nil, fmt.Errorf("get tags for stream visibility: %w", err)
	}
	autoHidden := automaticStreamHidden(*stream, tags)
	if visibilityRequested {
		if err := s.streamRepo.SetVisibilityOverride(id, visibilityOverride, autoHidden); err != nil {
			return nil, err
		}
	} else if err := s.streamRepo.ApplyAutomaticVisibility(id, autoHidden); err != nil {
		return nil, err
	}

	// 更新參與者
	if req.ParticipantIDs != nil {
		// 找出頻道擁有者（第一個參與者通常是擁有者）
		ownerID := ""
		if len(req.ParticipantIDs) > 0 {
			ownerID = req.ParticipantIDs[0]
		}
		if err := s.streamRepo.SetSingers(id, req.ParticipantIDs, ownerID); err != nil {
			return nil, fmt.Errorf("set participants: %w", err)
		}
	}

	// 返回更新後的資料
	return s.GetByID(id)
}

// ========== 首頁（ランダム再生） ==========

// ComposePerformanceList は配信横断の歌唱一覧をレスポンスへ変換する（配信の文脈付き）。
// プレイリストなど他サービスからも同じ形で返せるよう公開している。
func (s *StreamService) ComposePerformanceList(perfs []repository.PerformanceWithDetails) *dto.PerformanceListResponse {
	responses := make([]dto.PerformanceResponse, len(perfs))
	for i, perf := range perfs {
		resp := s.toPerformanceResponse(perf)
		resp.StreamTitle = perf.StreamTitle
		resp.StreamDate = perf.StreamDate
		if perf.ThumbnailURL.Valid {
			resp.ThumbnailURL = &perf.ThumbnailURL.String
		}
		responses[i] = resp
	}
	return &dto.PerformanceListResponse{Performances: responses}
}

// GetRandomPerformances は既出曲を除外した、曲単位で重複しないランダムな歌唱一覧を返す。
func (s *StreamService) GetRandomPerformances(limit int, excludedSongIDs []string) (*dto.PerformanceListResponse, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	perfs, err := s.perfRepo.FindRandom(limit, excludedSongIDs)
	if err != nil {
		return nil, fmt.Errorf("get random performances: %w", err)
	}
	return s.ComposePerformanceList(perfs), nil
}
