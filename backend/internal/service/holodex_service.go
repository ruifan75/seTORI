package service

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ruifan75/setori/internal/dto"
	"github.com/ruifan75/setori/internal/logger"
	"github.com/ruifan75/setori/internal/models"
	"github.com/ruifan75/setori/internal/repository"
	"github.com/ruifan75/setori/pkg/ai"
	"github.com/ruifan75/setori/pkg/holodex"
	"github.com/ruifan75/setori/pkg/itunes"
	"github.com/ruifan75/setori/pkg/util"
	"github.com/ruifan75/setori/pkg/youtube"
)

type HolodexService struct {
	client               *holodex.Client
	youtubeClient        *youtube.Client
	itunesClient         *itunes.Client
	streamRepo           *repository.StreamRepository
	singerRepo           *repository.SingerRepository
	perfRepo             *repository.PerformanceRepository
	songRepo             *repository.SongRepository
	songItunesRepo       *repository.SongItunesRepository
	editorMu             sync.RWMutex // editorToken は管理画面から実行中に差し替えられる
	editorToken          string
	aiClient             *ai.Client
	normalizationService *NormalizationService
	chatEndService       *ChatEndService
}

// SetEditorToken は Holodex への書き込みに使うトークンを差し替える。
func (s *HolodexService) SetEditorToken(token string) {
	s.editorMu.Lock()
	s.editorToken = token
	s.editorMu.Unlock()
}

func (s *HolodexService) editor() string {
	s.editorMu.RLock()
	defer s.editorMu.RUnlock()
	return s.editorToken
}

// SetAnalysisServices 注入正規化・拍手 end 偵測服務（AnalyzeHolodexSongs 用）。
func (s *HolodexService) SetAnalysisServices(norm *NormalizationService, chatEnd *ChatEndService) {
	s.normalizationService = norm
	s.chatEndService = chatEnd
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

// ApplyKeys は Holodex / YouTube / 編集トークンをまとめて差し替える。
// 設定画面での変更を再起動なしに反映するため、SettingsService から呼ばれる。
func (s *HolodexService) ApplyKeys(holodexKey, youtubeKey, editorToken string) {
	s.client.SetAPIKey(holodexKey)
	s.youtubeClient.SetAPIKey(youtubeKey)
	s.SetEditorToken(editorToken)
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
		logger.Warnf("YouTube photo fetch failed for %s: %v, falling back to Holodex", channelID, err)
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

// Holodex の topic_id と seTORI の stream_tags.id は同じ語でも表記が一致しない。
// 特に Original_Song / Music_Cover はそのまま FK へ入れるとタグが付かないため、
// seTORI 側の安定した ID へ明示的に寄せる。
var holodexTopicTagAliases = map[string]string{
	"concert":       "concert",
	"karaoke":       "karaoke",
	"live":          "concert",
	"music_cover":   "music_cover",
	"music_video":   "mv",
	"mv":            "mv",
	"original_song": "original_song",
	"singing":       "singing",
}

func streamTagIDForHolodexTopic(topicID string) (string, bool) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return "", false
	}
	tagID, mapped := holodexTopicTagAliases[strings.ToLower(topicID)]
	if mapped {
		return tagID, true
	}
	// shorts など、Holodex と seTORI で同じ ID を使っている既存 topic は
	// 従来どおりそのまま試す。対応する tag が無い topic の FK エラーは無視する。
	return topicID, false
}

// SyncChannelInfo 只同步頻道資訊，不同步直播
func (s *HolodexService) SyncChannelInfo(channelInput string) (*models.Singer, error) {
	lookup := youtube.ParseChannelLookup(channelInput)
	var holodexErr error
	if lookup.ID != "" {
		singer, err := s.syncHolodexChannelInfo(lookup.ID)
		if err == nil {
			return singer, nil
		}
		if !holodex.IsNotFound(err) {
			return nil, err
		}

		holodexErr = err
		logger.Warnf("Holodex channel fetch failed for %s: %v; trying YouTube fallback", lookup.ID, err)
	} else {
		channel, err := s.getYouTubeChannel(channelInput)
		if err != nil {
			return nil, fmt.Errorf("get channel from YouTube: %w", err)
		}

		singer, err := s.syncHolodexChannelInfo(channel.ID)
		if err == nil {
			return singer, nil
		}
		if !holodex.IsNotFound(err) {
			return nil, err
		}

		holodexErr = err
		logger.Warnf("Holodex channel fetch failed for resolved channel %s: %v; using YouTube fallback", channel.ID, err)
		return s.syncYouTubeChannelInfoFromChannel(channel)
	}

	singer, err := s.syncYouTubeChannelInfo(channelInput)
	if err != nil {
		if holodexErr != nil {
			return nil, fmt.Errorf("get channel from Holodex: %v; YouTube fallback: %w", holodexErr, err)
		}
		return nil, fmt.Errorf("get channel from YouTube: %w", err)
	}

	return singer, nil
}

func (s *HolodexService) syncHolodexChannelInfo(channelID string) (*models.Singer, error) {
	channel, err := s.client.GetChannel(channelID)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}

	// Upsert Singer
	singer := s.singerFromHolodexChannel(channel)
	if err := s.singerRepo.Upsert(singer); err != nil {
		return nil, fmt.Errorf("upsert singer: %w", err)
	}

	return singer, nil
}

func (s *HolodexService) syncYouTubeChannelInfo(channelInput string) (*models.Singer, error) {
	channel, err := s.getYouTubeChannel(channelInput)
	if err != nil {
		return nil, err
	}

	return s.syncYouTubeChannelInfoFromChannel(channel)
}

func (s *HolodexService) getYouTubeChannel(channelInput string) (*youtube.Channel, error) {
	if !s.youtubeClient.IsConfigured() {
		return nil, fmt.Errorf("YouTube API key not configured")
	}

	channel, err := s.youtubeClient.FindChannel(channelInput)
	if err != nil {
		return nil, err
	}
	if channel.ID == "" {
		return nil, fmt.Errorf("YouTube channel response did not include an ID")
	}

	return channel, nil
}

func (s *HolodexService) syncYouTubeChannelInfoFromChannel(channel *youtube.Channel) (*models.Singer, error) {
	name := strings.TrimSpace(channel.Snippet.Title)
	if name == "" {
		name = channel.ID
	}

	singer := &models.Singer{
		ID:             channel.ID,
		Name:           name,
		MetadataSource: "youtube",
	}
	if photoURL := youtube.BestThumbnailURL(channel); photoURL != "" {
		singer.PhotoURL = sql.NullString{String: photoURL, Valid: true}
	}
	if existing, err := s.singerRepo.FindByID(channel.ID); err != nil {
		return nil, fmt.Errorf("find existing singer: %w", err)
	} else if existing != nil {
		singer.EnglishName = existing.EnglishName
		singer.Organization = existing.Organization
		if !singer.PhotoURL.Valid {
			singer.PhotoURL = existing.PhotoURL
		}
	}

	if err := s.singerRepo.Upsert(singer); err != nil {
		return nil, fmt.Errorf("upsert singer: %w", err)
	}

	logger.Infof("channel info synced from YouTube fallback: %s (%s)", singer.ID, singer.Name)
	return singer, nil
}

func (s *HolodexService) singerFromHolodexChannel(channel *holodex.Channel) *models.Singer {
	singer := &models.Singer{
		ID:             channel.ID,
		Name:           channel.Name,
		MetadataSource: "holodex",
	}
	if channel.EnglishName != "" {
		singer.EnglishName = sql.NullString{String: channel.EnglishName, Valid: true}
	}
	// 優先使用 YouTube 大頭貼
	photoURL := s.getChannelPhotoURL(channel.ID, channel.Photo)
	if photoURL != "" {
		singer.PhotoURL = sql.NullString{String: photoURL, Valid: true}
	}
	if org := strings.TrimSpace(channel.Org); org != "" {
		singer.Organization = sql.NullString{String: org, Valid: true}
	}

	return singer
}

// SyncChannel 同步頻道的所有直播
func (s *HolodexService) SyncChannel(channelID string, limit int, forceUpdate bool) (*dto.SyncHolodexResponse, error) {
	logger.Infof("チャンネル同期開始: %s", channelID)

	// 先同步頻道資訊
	channel, err := s.client.GetChannel(channelID)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}

	// Upsert Singer
	singer := s.singerFromHolodexChannel(channel)
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

	// 1) 自家頻道が投稿した歌枠（channel_id フィルタ）
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
		s.applySyncedVideos(videos, channelID, forceUpdate, result)

		// 如果返回的數量少於 pageSize，表示已經是最後一頁
		if len(videos) < pageSize {
			break
		}

		offset += pageSize
	}

	// 2) 他チャンネルの枠にゲスト参加した歌枠（mentioned_channel_id）。
	//    別チャンネル投稿なので上の channel_id 取得では拾えない。
	//    取得失敗は致命的ではないため、ここまでの結果を保持して続行する。
	collabOffset := 0
	for {
		collabs, err := s.client.GetChannelCollabs(channelID, pageSize, collabOffset)
		if err != nil {
			logger.Warnf("コラボ取得失敗 (channel: %s): %v", channelID, err)
			break
		}

		if len(collabs) == 0 {
			break
		}

		totalVideos += len(collabs)
		result.TotalStreams = totalVideos
		s.applySyncedVideos(collabs, channelID, forceUpdate, result)

		if len(collabs) < pageSize {
			break
		}

		collabOffset += pageSize
	}

	result.InProgress = false
	result.Message = fmt.Sprintf("同期完了: %d 件新規, %d 件更新, %d 件スキップ",
		len(result.NewStreams), len(result.Updated), len(result.Skipped))
	logger.Infof("チャンネル同期完了: %s - %s", channelID, result.Message)

	return result, nil
}

// applySyncedVideos 取得済みの動画群を syncVideo で処理し、結果を集計する。
// channelID は擁有者が video から取れない場合の後備。自家投稿・コラボ両方で共用する。
func (s *HolodexService) applySyncedVideos(videos []holodex.Video, channelID string, forceUpdate bool, result *dto.SyncHolodexResponse) {
	for _, video := range videos {
		result.Processed++
		logger.Infof("処理中 [%d/%d]: %s - %s", result.Processed, result.TotalStreams, video.ID, video.Title)

		syncStatus, err := s.syncVideo(video, channelID, forceUpdate)
		if err != nil {
			// 記錄錯誤但繼續處理
			logger.Warnf("同期失敗 (video: %s): %v", video.ID, err)
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

	// topic だけから作る暫定値。タグ付け後に音楽系タグを見て可視へ戻す。
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
	logger.Infof("[holodex] upserted stream %s (existing=%v, force=%v)", video.ID, existing != nil, forceUpdate)

	// Holodex topic_id -> seTORI stream tag。両者の ID は必ずしも同じではない。
	if tagID, mapped := streamTagIDForHolodexTopic(video.TopicID); tagID != "" {
		if err := s.streamRepo.AddTag(video.ID, tagID); err != nil && mapped {
			return "", fmt.Errorf("add mapped Holodex topic tag (topic=%s, tag=%s): %w",
				video.TopicID, tagID, err)
		}
	}

	// タイトルの文字列マッチによる自動タグ付け（Holodex topic で拾えない種別を補完・追加のみ）
	if _, err := s.streamRepo.ApplyTagRulesToStream(video.ID); err != nil {
		return "", fmt.Errorf("apply tag rules: %w", err)
	}

	// 表示状態の自動判定は初回登録時だけ。既存配信は force sync でも触らず、
	// 以後は画面の「非表示」チェックで人が決めた値をそのまま保つ。
	if existing == nil {
		tags, err := s.streamRepo.GetTags(video.ID)
		if err != nil {
			return "", fmt.Errorf("get stream tags for initial visibility: %w", err)
		}
		tagIDs := make([]string, 0, len(tags))
		for _, tag := range tags {
			tagIDs = append(tagIDs, tag.ID)
		}
		initialHidden := initialStreamHidden(video.TopicID, video.Duration, video.Duration > 0, tagIDs)
		if err := s.streamRepo.SetInitialVisibility(video.ID, initialHidden); err != nil {
			return "", fmt.Errorf("set initial stream visibility: %w", err)
		}
	}

	// 影片擁有者（上傳頻道）。collab 影片的擁有者是主辦頻道，不是被同步的頻道，
	// 因此優先從 video 取得，取不到時才退回傳入的 channelID。
	ownerID := channelID
	if video.Channel != nil && video.Channel.ID != "" {
		ownerID = video.Channel.ID
	} else if video.ChannelID != "" {
		ownerID = video.ChannelID
	}
	// 擁有者與被同步的頻道不同（＝collab，主辦頻道是別人）時，
	// stream_singers 有指向 singers 的外鍵，需先 upsert 擁有者頻道。
	// （同步自家頻道時擁有者已在 SyncChannel/SyncVideo 先 upsert，故略過以免用較少的清單資料覆蓋）
	if ownerID != channelID && video.Channel != nil && video.Channel.ID == ownerID {
		s.singerRepo.Upsert(s.singerFromHolodexChannel(video.Channel))
	}

	// 處理 mentions -> 同步參與者
	// 先同步所有被提及的頻道，然後設定為此直播的參與者
	singerIDs := []string{ownerID} // 頻道擁有者一定要包含

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
		if org := strings.TrimSpace(mention.Org); org != "" {
			singer.Organization = sql.NullString{String: org, Valid: true}
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
	if err := s.streamRepo.SetSingers(video.ID, singerIDs, ownerID); err != nil {
		// 記錄錯誤但不中斷同步
		fmt.Printf("set stream singers error: %v\n", err)
	}

	// 同步時 HolodexData 已經包含完整的 video 資料（包括 songs）
	// 不需要額外儲存，stream_service 會在需要時從 HolodexData 解析

	// 同步時自動分析並儲存 Comment 資料
	if existing == nil || forceUpdate {
		s.loadAndSaveComments(video.ID)
		logger.Infof("[holodex] triggered comment analysis for %s", video.ID)
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
		if org := strings.TrimSpace(video.Channel.Org); org != "" {
			singer.Organization = sql.NullString{String: org, Valid: true}
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
		if org := strings.TrimSpace(video.Channel.Org); org != "" {
			channelOwner.Organization = &org
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
		if org := strings.TrimSpace(mention.Org); org != "" {
			participant.Organization = &org
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

// GetVideoComments 取得影片的公開留言（用於 Comment 分析）。
// YouTube 是權威來源；未設定、請求失敗或沒有留言時，再嘗試 Holodex。
func (s *HolodexService) GetVideoComments(videoID string) ([]string, error) {
	var youtubeErr error
	youtubeSucceeded := false
	if s.youtubeClient != nil && s.youtubeClient.IsConfigured() {
		comments, err := s.GetYouTubeVideoComments(videoID)
		if err == nil {
			youtubeSucceeded = true
			if len(comments) > 0 {
				return comments, nil
			}
			logger.Infof("[youtube] no comments returned for %s; trying Holodex fallback", videoID)
		} else {
			youtubeErr = err
			logger.Warnf("[youtube] comment fetch failed for %s: %v; trying Holodex fallback", videoID, err)
		}
	}

	video, err := s.client.GetVideo(videoID)
	if err != nil {
		// YouTube が正常に空結果を返したなら、Holodex の障害を全体の失敗にはしない。
		if youtubeSucceeded {
			return []string{}, nil
		}
		if youtubeErr != nil {
			return nil, fmt.Errorf("get comments from YouTube: %v; get video from Holodex: %w", youtubeErr, err)
		}
		return nil, fmt.Errorf("get video: %w", err)
	}

	comments := make([]string, len(video.Comments))
	for i, c := range video.Comments {
		comments[i] = c.Message
	}
	logger.Infof("[holodex] fetched %d comments for %s", len(comments), videoID)

	return comments, nil
}

// GetYouTubeVideoComments は Holodex に fallback せず、YouTube Data API だけから
// 公開トップレベルコメントを取得する。手動同期など、取得元を保証したい場合に使う。
func (s *HolodexService) GetYouTubeVideoComments(videoID string) ([]string, error) {
	if s.youtubeClient == nil || !s.youtubeClient.IsConfigured() {
		return nil, fmt.Errorf("YouTube API key not configured")
	}
	comments, err := s.youtubeClient.ListVideoComments(videoID)
	if err != nil {
		return nil, fmt.Errorf("get comments from YouTube: %w", err)
	}
	logger.Infof("[youtube] fetched %d comments for %s", len(comments), videoID)
	return comments, nil
}

// loadAndSaveComments 同步時只抓取並儲存「原始留言」（YouTube 優先、Holodex fallback），
// 準備給之後的分析使用。
// AI 抽取／正規化／拍手 end 偵測都不在同步時跑（避免大量同步時打爆 AI/yt-dlp），
// 改在編輯頁手動觸發分析時才執行並快取（見 CommentService.AnalyzeComments）。
func (s *HolodexService) loadAndSaveComments(videoID string) {
	comments, err := s.GetVideoComments(videoID)
	if err != nil {
		logger.Warnf("get comments error (video: %s): %v", videoID, err)
		return
	}

	commentRawJSON, err := json.Marshal(comments)
	if err != nil {
		logger.Warnf("marshal comment raw error (video: %s): %v", videoID, err)
		return
	}

	if err := s.streamRepo.SaveCommentRaw(videoID, util.SanitizeJSONB(commentRawJSON)); err != nil {
		logger.Warnf("save comment raw error (video: %s): %v", videoID, err)
	} else {
		logger.Infof("[holodex] saved %d raw comments for %s", len(comments), videoID)
	}
}

// holodexDataSong holodex_data JSONB に埋め込まれた song の最小形。
type holodexDataSong struct {
	Name           string `json:"name"`
	OriginalArtist string `json:"original_artist"`
	ArtURL         string `json:"art"`
	ITunesID       int64  `json:"itunesid"`
	Start          int    `json:"start"`
	End            int    `json:"end"`
}

// AnalyzeHolodexSongs 從 stored holodex_data 解析歌曲，正規化＋DB 照合＋拍手 end 補完，並持久化。
// 以既有の holodex_hash をキャッシュキーとし、Holodex 資料が変わっていなければ AI を再実行しない。
// Holodex が明示的に end を持つ曲はそれを優先（人力キュレーションのため最も正確）。
func (s *HolodexService) AnalyzeHolodexSongs(videoID string, force bool) ([]dto.SongSuggestion, error) {
	stream, err := s.streamRepo.FindByID(videoID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", videoID)
	}

	holodexHash := ""
	if stream.HolodexHash.Valid {
		holodexHash = stream.HolodexHash.String
	}

	// キャッシュ命中：holodex_data が変わっていない → chat 比較だけ付け直して返す（AI なし）
	if !force && holodexHash != "" {
		cached, cachedHash, _ := s.streamRepo.GetHolodexSongsCache(videoID)
		if cachedHash.Valid && cachedHash.String == holodexHash && len(cached) > 0 {
			var songs []dto.SongSuggestion
			if err := json.Unmarshal(cached, &songs); err == nil && len(songs) > 0 {
				// 照合は保存していないので、今の DB に対して計算して返す
				s.normalizationService.ResolveSuggestionsForDisplay(songs)
				s.adjudicateSuggestions(songs)
				s.attachChatComparison(stream, songs)
				return songs, nil
			}
		}
	}

	// 1. stored holodex_data から songs を解析
	var parsed struct {
		Songs []holodexDataSong `json:"songs"`
	}
	if len(stream.HolodexData) == 0 || json.Unmarshal(stream.HolodexData, &parsed) != nil || len(parsed.Songs) == 0 {
		return []dto.SongSuggestion{}, nil
	}

	songs := make([]dto.SongSuggestion, len(parsed.Songs))
	hasExplicitEnd := make([]bool, len(parsed.Songs))
	for i, sg := range parsed.Songs {
		songs[i] = dto.SongSuggestion{
			Name:           sg.Name,
			OriginalArtist: sg.OriginalArtist,
			StartSeconds:   sg.Start,
			EndSeconds:     sg.End,
			Tags:           []string{},
			SingerIDs:      []string{},
		}
		hasExplicitEnd[i] = sg.End > 0 // Holodex 明示 end（人力）
		if sg.ArtURL != "" {
			art := sg.ArtURL
			songs[i].ArtURL = &art
		}
		if sg.ITunesID > 0 {
			id := sg.ITunesID
			songs[i].ItunesID = &id
		}
	}

	// 2. 正規化（AI）を折り込む
	s.normalizeHolodexInto(songs)

	// 3. 照合はキャッシュ命中時と同じ関数で当て直す。
	//    normalizeHolodexInto は BatchAINormalization 経由で照合も埋めるが、
	//    そちらは変更履歴（changes）を作らない。2 つの経路で違うものを返さないよう、
	//    照合は必ずここを通す（数ミリ秒なので二度引く分は問題にならない）。
	s.normalizationService.ResolveSuggestionsForDisplay(songs)
	s.adjudicateSuggestions(songs)

	// 3. 拍手 end：明示 end が無い曲は補完。明示 end がある曲も chat 値を検出し、
	//    ChatEnd/EndDiff として付与する（Holodex 側の誤りをユーザーが確認できるように）。
	if s.chatEndService != nil {
		var duration int
		if stream.DurationSeconds.Valid {
			duration = int(stream.DurationSeconds.Int32)
		}
		starts := make([]int, len(songs))
		for i := range songs {
			starts[i] = songs[i].StartSeconds
		}
		if endByStart := s.chatEndService.DetectEnds(videoID, duration, starts); len(endByStart) > 0 {
			for i := range songs {
				chatEnd, ok := endByStart[songs[i].StartSeconds]
				if !ok {
					continue
				}
				songs[i].ChatEnd = chatEnd
				if hasExplicitEnd[i] {
					songs[i].EndDiff = absInt(songs[i].EndSeconds - chatEnd)
				} else {
					songs[i].EndSeconds = chatEnd
				}
			}
		}
	}

	// 4. まだ end が無い曲は次曲の start をフォールバックに使う
	for i := range songs {
		if songs[i].EndSeconds == 0 && i+1 < len(songs) {
			songs[i].EndSeconds = songs[i+1].StartSeconds
		}
	}

	// 5. 永続化（holodex_songs_normalized + holodex_hash）→ 次回はキャッシュを直接読む
	//    照合の結果は保存しない（読み取り時に計算する。コメント経路と同じ約束）
	if holodexHash != "" {
		if b, mErr := json.Marshal(stripMatchFromSuggestions(songs)); mErr == nil {
			if err := s.streamRepo.SaveHolodexSongs(videoID, b, holodexHash); err != nil {
				logger.Warnf("[holodex] save normalized songs failed (%s): %v", videoID, err)
			}
		}
	}

	return songs, nil
}

// adjudicateSuggestions は照合が決着しなかった曲を AI に判定させ、決まったぶんだけ照合し直す。
//
// 呼ぶのは利用者が「Holodex から読み込む」を押したときだけ。
// 配信詳細を開いただけの GET からは呼ばない（見ているだけで AI を呼ぶことになる）。
// 判定は保存されるので、一度誰かが読み込めば以後の閲覧は無料でその結果を受け取る。
func (s *HolodexService) adjudicateSuggestions(songs []dto.SongSuggestion) {
	if s.normalizationService == nil {
		return
	}
	s.normalizationService.AdjudicateSuggestions(songs)
}

// normalizeHolodexInto SongSuggestion に AI 正規化＋DB 照合結果を埋め込む（in-place）。
func (s *HolodexService) normalizeHolodexInto(songs []dto.SongSuggestion) {
	if s.normalizationService == nil || len(songs) == 0 {
		return
	}
	items := make([]dto.AINormalizationItem, len(songs))
	for i, sg := range songs {
		items[i] = dto.AINormalizationItem{
			Name:           sg.Name,
			OriginalArtist: sg.OriginalArtist,
			ItunesID:       sg.ItunesID,
			ArtURL:         sg.ArtURL,
		}
	}
	resp, err := s.normalizationService.BatchAINormalization(items)
	if err != nil {
		logger.Warnf("[holodex] normalization failed, keeping raw: %v", err)
		return
	}
	for _, sug := range resp.Suggestions {
		if sug.Index < 0 || sug.Index >= len(songs) {
			continue
		}
		songs[sug.Index].NormalizedName = sug.NormalizedName
		songs[sug.Index].NormalizedNameReading = sug.NormalizedNameReading
		songs[sug.Index].NormalizedArtist = sug.OriginalArtist
		songs[sug.Index].NormalizedArtistReading = sug.OriginalArtistReading
		songs[sug.Index].Tags = sug.Tags
		songs[sug.Index].Confidence = sug.Confidence
		songs[sug.Index].MatchedSongID = sug.MatchedSongID
		songs[sug.Index].MatchedSongName = sug.MatchedSongName
		songs[sug.Index].MatchedSongNameReading = sug.MatchedSongNameReading
		songs[sug.Index].MatchedSongArtist = sug.MatchedSongArtist
		songs[sug.Index].MatchedSongArtistReading = sug.MatchedSongArtistReading
		songs[sug.Index].MatchedSongArtURL = sug.MatchedSongArtURL
		songs[sug.Index].MatchedSongItunesID = sug.MatchedSongItunesID
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
	if tok := s.editor(); tok == "" || tok == "your-holodex-editor-token-here" {
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
				logger.Debugf("Found existing song in Holodex: iTunes ID %d (%s)", song.ITunesID, song.Name)
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
									logger.Debugf("⊘ Skipped: %s (iTunes: %d) - already exists in Holodex", song.Name, primaryItunesID)
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
						req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.editor()))
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
								logger.Infof("✓ Synced: %s (iTunes: %d)", song.Name, primaryItunesID)
							} else {
								logger.Infof("✓ Synced: %s (no iTunes ID)", song.Name)
							}
							syncedCount++
						} else {
							errMsg := fmt.Sprintf("%s: API error %d: %s", song.Name, resp.StatusCode, string(body))
							logger.Warnf("✗ %s", errMsg)
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
		logger.Warnf("Errors during sync: %v", errors)
	}

	return &dto.SyncHolodexResponse{
		NewStreams:  []string{},
		Updated:     []string{streamID},
		Skipped:     []string{},
		SyncedCount: syncedCount,
		Message:     message,
	}, nil
}

// attachChatComparison はキャッシュ済みの Holodex 曲に chat 拍手 end の比較値を付け直す。
// live chat はローカルキャッシュ済みなので安価。end 自体は変更しない（比較表示用）。
func (s *HolodexService) attachChatComparison(stream *models.Stream, songs []dto.SongSuggestion) {
	if s.chatEndService == nil || len(songs) == 0 {
		return
	}
	var duration int
	if stream.DurationSeconds.Valid {
		duration = int(stream.DurationSeconds.Int32)
	}
	starts := make([]int, len(songs))
	for i := range songs {
		starts[i] = songs[i].StartSeconds
	}
	endByStart := s.chatEndService.DetectEnds(stream.ID, duration, starts)
	for i := range songs {
		if chatEnd, ok := endByStart[songs[i].StartSeconds]; ok {
			songs[i].ChatEnd = chatEnd
			if songs[i].EndSeconds > 0 {
				songs[i].EndDiff = absInt(songs[i].EndSeconds - chatEnd)
			}
		}
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
