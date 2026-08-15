package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
)

type StreamService struct {
	streamRepo *repository.StreamRepository
	perfRepo   *repository.PerformanceRepository
}

func NewStreamService(streamRepo *repository.StreamRepository, perfRepo *repository.PerformanceRepository) *StreamService {
	return &StreamService{
		streamRepo: streamRepo,
		perfRepo:   perfRepo,
	}
}

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

// GetAll は歌枠一覧を取得する（既定では非表示を除外）。
func (s *StreamService) GetAll(page, limit int, sort, dir string) (*dto.StreamListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	// 既定では非表示の歌枠を除外する
	streams, total, err := s.streamRepo.FindAll(limit, offset, false, sort, dir)
	if err != nil {
		return nil, fmt.Errorf("get streams: %w", err)
	}

	// タグと参加者を一括取得し、N+1 を避ける
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

	// DTO に変換する
	streamResponses := make([]dto.StreamResponse, len(streams))
	for i, stream := range streams {
		streamResponses[i] = s.toStreamResponse(stream, tagsMap[stream.ID], participantsMap[stream.ID], ownersMap[stream.ID])
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
		streamResponses[i] = s.toStreamResponse(stream, tagsMap[stream.ID], participantsMap[stream.ID], ownersMap[stream.ID])
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

// GetByID は歌枠の詳細（セットリストを含む）を取得する。
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
	streamResp := s.toStreamResponse(*stream, tags, participants, channelOwner)

	// 歌唱一覧を取得する
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

// toStreamResponse は Model を DTO に変換する。**照合はしない**（理由は下の comment_songs の節）。
func (s *StreamService) toStreamResponse(stream models.Stream, tags []models.StreamTag, participants []models.Singer, channelOwner *models.Singer) dto.StreamResponse {
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

	// チャンネル所有者を変換する
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

	// Holodex の timeline データを解析して追加する（完全な Video JSON から songs を抽出）
	if len(stream.HolodexData) > 0 {
		// HolodexData には完全な holodex.Video オブジェクトが保存されている
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
				// 終了時刻がなければ次の曲の開始時刻を使う
				if holodexSongs[i].EndSeconds == 0 && i+1 < len(video.Songs) {
					holodexSongs[i].EndSeconds = video.Songs[i+1].Start
				}
			}
			resp.HolodexTimelineSongs = holodexSongs
		}
	}

	// comment_songs から分析済みのキャッシュ結果を読み込む。
	//
	// **ここでは照合しない。** 照合するのは「どの入力元から取り込むか」を決めて
	// 編集フォームへ読み込む操作（POST /comments/analyze、/holodex-songs/analyze）
	// だけで、配信を開いただけの読み取りでは何も引かない。Holodex 側も同じ扱い
	// （このメソッドは holodex_data をそのまま組み立てるだけ）。
	//
	// 保存済みの JSON に照合の痕跡が残っている配信（read-time 照合に移す前に
	// backfill-matches が書いたもの）があるので、落としてから返す。
	if len(stream.CommentSongs) > 0 {
		var commentSongs []dto.CommentSong
		if err := json.Unmarshal(stream.CommentSongs, &commentSongs); err == nil && len(commentSongs) > 0 {
			resp.CommentTimelineSongs = stripMatchForStorage(commentSongs)
		}
	}

	if stream.CommentSongsAnalyzedAt.Valid {
		t := stream.CommentSongsAnalyzedAt.Time.Format(time.RFC3339)
		resp.CommentSongsAnalyzedAt = &t
	}

	// comment_raw にコメントがあれば分析ボタンを有効化できる（comment_songs が未生成でも分析可能）
	resp.HasCommentRaw = len(stream.CommentRaw) > 0 && string(stream.CommentRaw) != "null" && string(stream.CommentRaw) != "[]"

	return resp
}

// toPerformanceResponse は歌唱を DTO に変換する。
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
	if perf.ItunesID.Valid {
		resp.ItunesID = &perf.ItunesID.Int64
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

// Update は歌枠の情報を更新する。
func (s *StreamService) Update(id string, req *dto.UpdateStreamRequest) (*dto.StreamDetailResponse, error) {
	stream, err := s.streamRepo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, nil
	}

	// タイトルを更新する
	if req.Title != nil && *req.Title != "" {
		stream.Title = *req.Title
	}

	// 日付を更新する
	if req.StreamDate != nil && *req.StreamDate != "" {
		// RFC3339 形式（完全な日時）を試し、失敗したら日付形式を試す
		date, err := time.Parse(time.RFC3339, *req.StreamDate)
		if err != nil {
			// RFC3339 でなければ日付形式として解析する
			date, err = time.Parse("2006-01-02", *req.StreamDate)
			if err != nil {
				return nil, fmt.Errorf("parse stream date: %w", err)
			}
		}
		stream.StreamDate = date
	}

	// 処理状態を更新する
	if req.IsProcessed != nil {
		stream.IsProcessed = *req.IsProcessed
	}

	// 表示状態は初回登録後は完全に手動。同期やタグ編集から再判定しない。
	if req.IsHidden != nil {
		stream.IsHidden = *req.IsHidden
	}

	// 配信の metadata を更新する（変更可能なフィールドだけを更新し、大きな JSONB は書き戻さない）
	if err := s.streamRepo.UpdateMetadata(id, stream.Title, stream.StreamDate, stream.IsProcessed, stream.IsHidden); err != nil {
		return nil, fmt.Errorf("update stream: %w", err)
	}

	// タグを更新する
	if req.TagIDs != nil {
		if err := s.streamRepo.SetTags(id, req.TagIDs); err != nil {
			return nil, fmt.Errorf("set tags: %w", err)
		}
	}

	// 参加者を更新する
	if req.ParticipantIDs != nil {
		// チャンネル所有者を探す（通常は最初の参加者）
		ownerID := ""
		if len(req.ParticipantIDs) > 0 {
			ownerID = req.ParticipantIDs[0]
		}
		if err := s.streamRepo.SetSingers(id, req.ParticipantIDs, ownerID); err != nil {
			return nil, fmt.Errorf("set participants: %w", err)
		}
	}

	// 更新後のデータを返す
	return s.GetByID(id)
}

// ========== ホーム（ランダム再生） ==========

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
