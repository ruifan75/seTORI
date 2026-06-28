package service

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/holodex"
	"github.com/ruifan75/setori/pkg/itunes"
	"github.com/ruifan75/setori/pkg/youtube"
)

type HolodexService struct {
	client         *holodex.Client
	youtubeClient  *youtube.Client
	itunesClient   *itunes.Client
	streamRepo     *repository.StreamRepository
	singerRepo     *repository.SingerRepository
	perfRepo       *repository.PerformanceRepository
	songRepo       *repository.SongRepository
	songItunesRepo *repository.SongItunesRepository
	editorToken    string
	aiClient       *ai.Client
}

func NewHolodexService(
	holodexAPIKey string,
	youtubeAPIKey string,
	groqAPIKey string,
	streamRepo *repository.StreamRepository,
	singerRepo *repository.SingerRepository,
	editorToken string,
) *HolodexService {
	var aiClient *ai.Client
	if groqAPIKey != "" {
		aiClient = ai.NewClient(groqAPIKey)
	}

	return &HolodexService{
		client:        holodex.NewClient(holodexAPIKey),
		youtubeClient: youtube.NewClient(youtubeAPIKey),
		itunesClient:  itunes.NewClient(),
		streamRepo:    streamRepo,
		singerRepo:    singerRepo,
		editorToken:   editorToken,
		aiClient:      aiClient,
	}
}

// SetRepositories 設定額外的 repositories（用於 SyncSetoriToHolodex）
func (s *HolodexService) SetRepositoriesWithSongItunes(perfRepo *repository.PerformanceRepository, songRepo *repository.SongRepository, songItunesRepo *repository.SongItunesRepository) {
	s.perfRepo = perfRepo
	s.songRepo = songRepo
	s.songItunesRepo = songItunesRepo
}

// getChannelPhotoURL 嘗試從 YouTube 取得頻道大頭貼，若失敗則使用 Holodex，最後使用 Holodex 靜態圖片
func (s *HolodexService) getChannelPhotoURL(channelID string, holodexPhoto string) string {
	// 1. 如果 YouTube API 有設定，優先嘗試使用
	if s.youtubeClient.IsConfigured() {
		photo, err := s.youtubeClient.GetChannelPhoto(channelID)
		if err == nil && photo != "" {
			return photo
		}
		// YouTube 取得失敗，記錄並嘗試使用 Holodex
		log.Printf("YouTube photo fetch failed for %s: %v, falling back to Holodex", channelID, err)
	}

	// 2. 使用 Holodex API 提供的 photo URL
	if holodexPhoto != "" {
		return holodexPhoto
	}

	// 3. 最後使用 Holodex 靜態圖片 URL 作為 fallback
	return fmt.Sprintf("https://holodex.net/statics/channelImg/%s/50.png", channelID)
}

// SyncResult 同步結果
type SyncResult struct {
	SyncedCount int
	NewStreams  []string
	Updated     []string
	Skipped     []string
}

// SyncChannelInfo 只同步頻道資訊，不同步直播
func (s *HolodexService) SyncChannelInfo(channelID string) error {
	channel, err := s.client.GetChannel(channelID)
	if err != nil {
		return fmt.Errorf("get channel: %w", err)
	}

	// Upsert Singer
	singer := &models.Singer{
		ID:   channel.ID,
		Name: channel.Name,
	}
	if channel.EnglishName != "" {
		singer.EnglishName = sql.NullString{String: channel.EnglishName, Valid: true}
	}
	// 優先使用 YouTube 大頭貼
	photoURL := s.getChannelPhotoURL(channel.ID, channel.Photo)
	if photoURL != "" {
		singer.PhotoURL = sql.NullString{String: photoURL, Valid: true}
	}
	if channel.Org != "" {
		singer.Organization = sql.NullString{String: channel.Org, Valid: true}
	}

	if err := s.singerRepo.Upsert(singer); err != nil {
		return fmt.Errorf("upsert singer: %w", err)
	}

	return nil
}

// SyncChannel 同步頻道的所有直播
func (s *HolodexService) SyncChannel(channelID string, limit int, forceUpdate bool) (*dto.SyncHolodexResponse, error) {
	log.Printf("チャンネル同期開始: %s", channelID)

	// 先同步頻道資訊
	channel, err := s.client.GetChannel(channelID)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}

	// Upsert Singer
	singer := &models.Singer{
		ID:   channel.ID,
		Name: channel.Name,
	}
	if channel.EnglishName != "" {
		singer.EnglishName = sql.NullString{String: channel.EnglishName, Valid: true}
	}
	// 優先使用 YouTube 大頭貼
	photoURL := s.getChannelPhotoURL(channel.ID, channel.Photo)
	if photoURL != "" {
		singer.PhotoURL = sql.NullString{String: photoURL, Valid: true}
	}
	if channel.Org != "" {
		singer.Organization = sql.NullString{String: channel.Org, Valid: true}
	}

	if err := s.singerRepo.Upsert(singer); err != nil {
		return nil, fmt.Errorf("upsert singer: %w", err)
	}

	result := &dto.SyncHolodexResponse{
		NewStreams: []string{},
		Updated:    []string{},
		Skipped:    []string{},
		InProgress: true,
	}

	// 分頁取得所有歌回
	const pageSize = 50
	offset := 0
	totalVideos := 0

	for {
		videos, err := s.client.GetAllStreams(channelID, pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("get all streams: %w", err)
		}

		// 沒有更多資料時結束
		if len(videos) == 0 {
			break
		}

		totalVideos += len(videos)
		result.TotalStreams = totalVideos

		for i, video := range videos {
			result.Processed = offset + i + 1
			log.Printf("処理中 [%d/%d]: %s - %s", result.Processed, result.TotalStreams, video.ID, video.Title)

			syncStatus, err := s.syncVideo(video, channelID, forceUpdate)
			if err != nil {
				// 記錄錯誤但繼續處理
				log.Printf("同期失敗 (video: %s): %v", video.ID, err)
				result.Skipped = append(result.Skipped, video.ID)
				continue
			}

			switch syncStatus {
			case "new":
				result.NewStreams = append(result.NewStreams, video.ID)
				result.SyncedCount++
			case "updated":
				result.Updated = append(result.Updated, video.ID)
				result.SyncedCount++
			case "skipped":
				result.Skipped = append(result.Skipped, video.ID)
			}
		}

		// 如果返回的數量少於 pageSize，表示已經是最後一頁
		if len(videos) < pageSize {
			break
		}

		offset += pageSize
	}

	result.InProgress = false
	result.Message = fmt.Sprintf("同期完了: %d 件新規, %d 件更新, %d 件スキップ",
		len(result.NewStreams), len(result.Updated), len(result.Skipped))
	log.Printf("チャンネル同期完了: %s - %s", channelID, result.Message)

	return result, nil
}

// syncVideo 同步單一影片
func (s *HolodexService) syncVideo(video holodex.Video, channelID string, forceUpdate bool) (string, error) {
	// 檢查是否已存在
	existing, err := s.streamRepo.FindByID(video.ID)
	if err != nil {
		return "", fmt.Errorf("find stream: %w", err)
	}

	// 如果已存在且非強制更新模式，跳過
	if existing != nil && !forceUpdate {
		return "skipped", nil
	}

	// 計算 Holodex 資料的 hash
	holodexJSON, err := json.Marshal(video)
	if err != nil {
		return "", fmt.Errorf("marshal video: %w", err)
	}
	hash := sha256.Sum256(holodexJSON)
	hashStr := hex.EncodeToString(hash[:])

	// 解析日期
	var streamDate time.Time
	if video.AvailableAt != "" {
		streamDate, _ = time.Parse(time.RFC3339, video.AvailableAt)
	} else if video.PublishedAt != "" {
		streamDate, _ = time.Parse(time.RFC3339, video.PublishedAt)
	}

	// 非 singing 的影片預設為 hidden
	isHidden := video.TopicID != "singing"

	stream := &models.Stream{
		ID:          video.ID,
		Title:       video.Title,
		StreamDate:  streamDate,
		HolodexData: holodexJSON,
		HolodexHash: sql.NullString{String: hashStr, Valid: true},
		IsHidden:    isHidden,
	}

	if video.Duration > 0 {
		stream.DurationSeconds = sql.NullInt32{Int32: int32(video.Duration), Valid: true}
	}

	// 建立縮圖 URL
	thumbnailURL := fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", video.ID)
	stream.ThumbnailURL = sql.NullString{String: thumbnailURL, Valid: true}

	// Upsert Stream
	if err := s.streamRepo.Upsert(stream); err != nil {
		return "", fmt.Errorf("upsert stream: %w", err)
	}

	// 處理 topic_id -> 設定標籤
	if video.TopicID != "" {
		// 對應 topic_id 到 stream_tag（Holodex 的 topic_id 可以直接作為標籤使用）
		s.streamRepo.AddTag(video.ID, video.TopicID)
	}

	// 處理 mentions -> 同步參與者
	// 先同步所有被提及的頻道，然後設定為此直播的參與者
	singerIDs := []string{channelID} // 頻道擁有者一定要包含

	for _, mention := range video.Mentions {
		// 同步被提及的頻道為 Singer
		singer := &models.Singer{
			ID:   mention.ID,
			Name: mention.Name,
		}
		if mention.EnglishName != "" {
			singer.EnglishName = sql.NullString{String: mention.EnglishName, Valid: true}
		}
		// 優先使用 YouTube 大頭貼
		mentionPhoto := s.getChannelPhotoURL(mention.ID, mention.Photo)
		if mentionPhoto != "" {
			singer.PhotoURL = sql.NullString{String: mentionPhoto, Valid: true}
		}
		if mention.Org != "" {
			singer.Organization = sql.NullString{String: mention.Org, Valid: true}
		}
		s.singerRepo.Upsert(singer)

		// 加入參與者列表（避免重複）
		found := false
		for _, id := range singerIDs {
			if id == mention.ID {
				found = true
				break
			}
		}
		if !found {
			singerIDs = append(singerIDs, mention.ID)
		}
	}

	// 設定此直播的所有參與者
	if err := s.streamRepo.SetSingers(video.ID, singerIDs, channelID); err != nil {
		// 記錄錯誤但不中斷同步
		fmt.Printf("set stream singers error: %v\n", err)
	}

	// 同步時 HolodexData 已經包含完整的 video 資料（包括 songs）
	// 不需要額外儲存，stream_service 會在需要時從 HolodexData 解析

	// 同步時自動分析並儲存 Comment 資料
	if existing == nil || forceUpdate {
		s.loadAndSaveComments(video.ID)
	}

	if existing == nil {
		return "new", nil
	}
	return "updated", nil
}

// SyncVideo 同步單一影片（可用於手動添加）
func (s *HolodexService) SyncVideo(videoID string) (*dto.SyncHolodexResponse, error) {
	video, err := s.client.GetVideoWithSongs(videoID)
	if err != nil {
		return nil, fmt.Errorf("get video: %w", err)
	}

	channelID := video.ChannelID
	if video.Channel != nil {
		channelID = video.Channel.ID

		// 同步頻道資訊
		singer := &models.Singer{
			ID:   video.Channel.ID,
			Name: video.Channel.Name,
		}
		if video.Channel.EnglishName != "" {
			singer.EnglishName = sql.NullString{String: video.Channel.EnglishName, Valid: true}
		}
		// 優先使用 YouTube 大頭貼
		channelPhoto := s.getChannelPhotoURL(video.Channel.ID, video.Channel.Photo)
		if channelPhoto != "" {
			singer.PhotoURL = sql.NullString{String: channelPhoto, Valid: true}
		}
		if video.Channel.Org != "" {
			singer.Organization = sql.NullString{String: video.Channel.Org, Valid: true}
		}
		s.singerRepo.Upsert(singer)
	}

	result := &dto.SyncHolodexResponse{
		NewStreams: []string{},
		Updated:    []string{},
		Skipped:    []string{},
	}

	// 單一影片同步時，總是強制更新
	syncStatus, err := s.syncVideo(*video, channelID, true)
	if err != nil {
		return nil, fmt.Errorf("sync video: %w", err)
	}

	switch syncStatus {
	case "new":
		result.NewStreams = append(result.NewStreams, video.ID)
		result.SyncedCount = 1
	case "updated":
		result.Updated = append(result.Updated, video.ID)
		result.SyncedCount = 1
	case "skipped":
		result.Skipped = append(result.Skipped, video.ID)
	}

	return result, nil
}

// LoadHolodexSongs 從 Holodex 載入歌曲（不加入正規化佇列）
func (s *HolodexService) LoadHolodexSongs(videoID string) (*dto.LoadHolodexSongsResponse, error) {
	video, err := s.client.GetVideoWithSongs(videoID)
	if err != nil {
		return nil, fmt.Errorf("get video: %w", err)
	}

	// 取得頻道擁有者資訊
	var channelOwner dto.SingerResponse
	if video.Channel != nil {
		channelOwner = dto.SingerResponse{
			ID:   video.Channel.ID,
			Name: video.Channel.Name,
		}
		if video.Channel.EnglishName != "" {
			channelOwner.EnglishName = &video.Channel.EnglishName
		}
		if video.Channel.Photo != "" {
			channelOwner.PhotoURL = &video.Channel.Photo
		}
		if video.Channel.Org != "" {
			channelOwner.Organization = &video.Channel.Org
		}
	}

	// 收集所有參與者（頻道擁有者 + mentions）
	participants := []dto.SingerResponse{channelOwner}
	allSingerIDs := []string{channelOwner.ID}

	for _, mention := range video.Mentions {
		// 避免重複
		found := false
		for _, id := range allSingerIDs {
			if id == mention.ID {
				found = true
				break
			}
		}
		if found {
			continue
		}

		participant := dto.SingerResponse{
			ID:   mention.ID,
			Name: mention.Name,
		}
		if mention.EnglishName != "" {
			participant.EnglishName = &mention.EnglishName
		}
		if mention.Photo != "" {
			participant.PhotoURL = &mention.Photo
		}
		if mention.Org != "" {
			participant.Organization = &mention.Org
		}
		participants = append(participants, participant)
		allSingerIDs = append(allSingerIDs, mention.ID)
	}

	// 轉換歌曲資料
	songs := make([]dto.SongSuggestion, len(video.Songs))
	for i, song := range video.Songs {
		songs[i] = dto.SongSuggestion{
			Name:           song.Name,
			OriginalArtist: song.OriginalArtist,
			StartSeconds:   song.Start,
			EndSeconds:     song.End,
			Tags:           []string{},
			SingerIDs:      allSingerIDs, // 預設為所有參與者
		}
		if song.ArtURL != "" {
			songs[i].ArtURL = &song.ArtURL
		}
		// 加入 iTunes ID（如果有）
		if song.ITunesID > 0 {
			itunesID := song.ITunesID
			songs[i].ItunesID = &itunesID
		}

		// 如果沒有結束時間，使用下一首的開始時間
		if songs[i].EndSeconds == 0 && i+1 < len(video.Songs) {
			songs[i].EndSeconds = video.Songs[i+1].Start
		}
	}

	// 注意：不再儲存到資料庫，因為 HolodexData 已在 sync 時儲存完整的 Video JSON
	// 這個 API 主要用於即時載入和返回資料給前端

	return &dto.LoadHolodexSongsResponse{
		StreamID:     video.ID,
		StreamTitle:  video.Title,
		ChannelOwner: channelOwner,
		Participants: participants,
		Songs:        songs,
	}, nil
}

// GetVideoComments 取得影片的評論（用於 Comment 分析）
func (s *HolodexService) GetVideoComments(videoID string) ([]string, error) {
	video, err := s.client.GetVideo(videoID)
	if err != nil {
		return nil, fmt.Errorf("get video: %w", err)
	}

	comments := make([]string, len(video.Comments))
	for i, c := range video.Comments {
		comments[i] = c.Message
	}

	return comments, nil
}

// loadAndSaveComments 同步時只抓取並儲存「原始留言」，準備給之後的分析使用。
// AI 抽取／正規化／拍手 end 偵測都不在同步時跑（避免大量同步時打爆 AI/yt-dlp），
// 改在編輯頁手動觸發分析時才執行並快取（見 CommentService.AnalyzeComments）。
func (s *HolodexService) loadAndSaveComments(videoID string) {
	comments, err := s.GetVideoComments(videoID)
	if err != nil {
		log.Printf("get comments error (video: %s): %v", videoID, err)
		return
	}

	commentRawJSON, err := json.Marshal(comments)
	if err != nil {
		log.Printf("marshal comment raw error (video: %s): %v", videoID, err)
		return
	}

	if err := s.streamRepo.SaveCommentRaw(videoID, commentRawJSON); err != nil {
		log.Printf("save comment raw error (video: %s): %v", videoID, err)
	}
}

// SyncSetoriToHolodex 將 seTORI 的資料同步到 Holodex（當 Holodex 沒有資料時）
// 目前只印出 request 內容，不執行實際的 API 呼叫
func (s *HolodexService) SyncSetoriToHolodex(streamID string) (*dto.SyncHolodexResponse, error) {
	// 1. 取得 stream 資訊
	stream, err := s.streamRepo.FindByID(streamID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", streamID)
	}

	// 2. 檢查 editor token 是否配置
	if s.editorToken == "" || s.editorToken == "your-holodex-editor-token-here" {
		return nil, fmt.Errorf("Holodex editor token not configured")
	}

	// 3. 取得頻道資訊（從 HolodexData 中解析，或使用第一個 singer 作為備用）
	var channelID, channelName, channelEnglishName string

	// 首先嘗試從 HolodexData 取得 channel ID
	if len(stream.HolodexData) > 0 {
		var video struct {
			ChannelID string `json:"channel_id"`
			Channel   struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				EnglishName string `json:"english_name"`
			} `json:"channel"`
		}
		if err := json.Unmarshal(stream.HolodexData, &video); err == nil {
			if video.Channel.ID != "" {
				channelID = video.Channel.ID
				channelName = video.Channel.Name
				channelEnglishName = video.Channel.EnglishName
			} else if video.ChannelID != "" {
				channelID = video.ChannelID
			}
		}
	}

	// 如果沒有 channel name，從資料庫查詢
	if (channelName == "" || channelEnglishName == "") && channelID != "" {
		if s.singerRepo != nil {
			if singer, err := s.singerRepo.FindByID(channelID); err == nil && singer != nil {
				channelName = singer.Name
				channelEnglishName = singer.EnglishName.String
			}
		}
	}

	// 4. 先從 Holodex 取得已存在的歌曲列表
	existingSongs := make(map[int64]bool) // 用 iTunes ID 作為 key
	holodexVideo, err := s.client.GetVideoWithSongs(streamID)
	if err == nil && holodexVideo != nil {
		for _, song := range holodexVideo.Songs {
			if song.ITunesID > 0 {
				existingSongs[song.ITunesID] = true
				log.Printf("Found existing song in Holodex: iTunes ID %d (%s)", song.ITunesID, song.Name)
			}
		}
	}

	// 6. 取得 performances 和對應的 song 資訊
	syncedCount := 0
	skippedCount := 0
	var errors []string

	if s.perfRepo != nil {
		perfs, err := s.perfRepo.FindByStreamID(streamID)
		if err == nil && len(perfs) > 0 {
			for _, perf := range perfs {
				if s.songRepo != nil {
					song, err := s.songRepo.FindByID(perf.SongID)
					if err == nil && song != nil {
						// 構造基本的 request 資料
						requestData := map[string]interface{}{
							"start":           perf.StartSeconds,
							"end":             perf.EndSeconds,
							"name":            song.Name,
							"original_artist": song.OriginalArtist,
							"video_id":        streamID,
							"channel_id":      channelID,
							"available_at":    stream.StreamDate.Format(time.RFC3339),
						}

						// 添加 channel 資訊
						if channelName != "" {
							channelObj := map[string]interface{}{
								"name":         channelName,
								"english_name": channelEnglishName,
							}
							requestData["channel"] = channelObj
						}

						// 嘗試取得 iTunes 資訊（只使用 primary iTunes ID）
						var primaryItunesID int64 = 0
						if s.songItunesRepo != nil {
							itunesRecords, err := s.songItunesRepo.FindBySongID(perf.SongID)
							if err == nil && len(itunesRecords) > 0 {
								// 只使用 primary iTunes ID
								var primaryItunes models.SongITunes
								foundPrimary := false
								for _, record := range itunesRecords {
									if record.IsPrimary {
										primaryItunes = record
										foundPrimary = true
										break
									}
								}

								// 如果沒有標記為 primary 的，使用第一筆
								if !foundPrimary {
									primaryItunes = itunesRecords[0]
								}

								primaryItunesID = primaryItunes.ITunesID

								// 檢查這首歌是否已經存在於 Holodex（僅當有 iTunes ID 時）
								if existingSongs[primaryItunesID] {
									log.Printf("⊘ Skipped: %s (iTunes: %d) - already exists in Holodex", song.Name, primaryItunesID)
									skippedCount++
									continue
								}

								// 從 iTunes 取得完整資訊
								itunesInfo, err := s.itunesClient.QueryByID(primaryItunesID)

								// 設定 iTunes ID
								requestData["itunesid"] = primaryItunesID

								// 如果有 iTunes 資訊，添加完整的 song 物件和 URL
								if err == nil && itunesInfo != nil {
									songObj := map[string]interface{}{
										"trackId":         itunesInfo.ItunesID,
										"trackTimeMillis": itunesInfo.TrackTimeMillis,
										"collectionName":  itunesInfo.CollectionName,
										"artistName":      itunesInfo.ArtistName,
										"trackName":       itunesInfo.TrackName,
										"artworkUrl100":   itunesInfo.ArtworkURL,
										"trackViewUrl":    itunesInfo.TrackViewURL,
									}
									requestData["song"] = songObj
									requestData["amUrl"] = itunesInfo.TrackViewURL
									requestData["art"] = itunesInfo.ArtworkURL
								}
							}
						}

						// 如果沒有 iTunes ID，也要上傳（設定為 null 並添加 Musicdex source）
						if primaryItunesID == 0 {
							requestData["itunesid"] = nil
							requestData["song"] = map[string]interface{}{
								"trackId":         nil,
								"artistName":      song.OriginalArtist,
								"trackName":       song.Name,
								"trackTimeMillis": nil,
								"trackViewUrl":    nil,
								"artworkUrl100":   nil,
								"src":             "Musicdex",
							}
							requestData["amUrl"] = nil
							requestData["art"] = nil
						}

						// 發送請求到 Holodex API
						// 發送請求到 Holodex API
						requestJSON, err := json.Marshal(requestData)
						if err != nil {
							errors = append(errors, fmt.Sprintf("%s: marshal error: %v", song.Name, err))
							continue
						}

						req, err := http.NewRequest("PUT", "https://holodex.net/api/v2/songs", bytes.NewBuffer(requestJSON))
						if err != nil {
							errors = append(errors, fmt.Sprintf("%s: create request error: %v", song.Name, err))
							continue
						}

						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.editorToken))
						req.Header.Set("Origin", "https://holodex.net")

						client := &http.Client{Timeout: 30 * time.Second}
						resp, err := client.Do(req)
						if err != nil {
							errors = append(errors, fmt.Sprintf("%s: request error: %v", song.Name, err))
							continue
						}

						// 在迴圈內逐一關閉，避免 defer 累積導致連線洩漏
						body, _ := io.ReadAll(resp.Body)
						resp.Body.Close()
						if resp.StatusCode >= 200 && resp.StatusCode < 300 {
							if primaryItunesID > 0 {
								log.Printf("✓ Synced: %s (iTunes: %d)", song.Name, primaryItunesID)
							} else {
								log.Printf("✓ Synced: %s (no iTunes ID)", song.Name)
							}
							syncedCount++
						} else {
							errMsg := fmt.Sprintf("%s: API error %d: %s", song.Name, resp.StatusCode, string(body))
							log.Printf("✗ %s", errMsg)
							errors = append(errors, errMsg)
						}
					}
				}
			}
		}
	}

	message := fmt.Sprintf("同期完了: %d 曲成功", syncedCount)
	if skippedCount > 0 {
		message = fmt.Sprintf("同期完了: %d 曲成功、%d 曲既に存在", syncedCount, skippedCount)
	}
	if len(errors) > 0 {
		message = fmt.Sprintf("同期完了: %d 曲成功、%d 曲既に存在、%d 曲失敗", syncedCount, skippedCount, len(errors))
		log.Printf("Errors during sync: %v", errors)
	}

	return &dto.SyncHolodexResponse{
		NewStreams:  []string{},
		Updated:     []string{streamID},
		Skipped:     []string{},
		SyncedCount: syncedCount,
		Message:     message,
	}, nil
}
