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
	"github.com/ruifan75/setori/pkg/perftag"
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

// SetAnalysisServices は正規化・拍手 end 検出サービスを注入する（AnalyzeHolodexSongs 用）。
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

// SetRepositories は追加の repository を設定する（SyncSetoriToHolodex 用）。
func (s *HolodexService) SetRepositoriesWithSongItunes(perfRepo *repository.PerformanceRepository, songRepo *repository.SongRepository, songItunesRepo *repository.SongItunesRepository) {
	s.perfRepo = perfRepo
	s.songRepo = songRepo
	s.songItunesRepo = songItunesRepo
}

// getChannelPhotoURL は YouTube からチャンネルのアバター取得を試し、失敗したら Holodex、最後に Holodex の静的画像を使う。
func (s *HolodexService) getChannelPhotoURL(channelID string, holodexPhoto string) string {
	// 1. YouTube API が設定済みなら優先して試す
	if s.youtubeClient.IsConfigured() {
		photo, err := s.youtubeClient.GetChannelPhoto(channelID)
		if err == nil && photo != "" {
			return photo
		}
		// YouTube からの取得に失敗したら記録し、Holodex を試す
		logger.Warnf("YouTube photo fetch failed for %s: %v, falling back to Holodex", channelID, err)
	}

	// 2. Holodex API が提供する photo URL を使う
	if holodexPhoto != "" {
		return holodexPhoto
	}

	// 3. 最後に Holodex の静的画像 URL へフォールバックする
	return fmt.Sprintf("https://holodex.net/statics/channelImg/%s/50.png", channelID)
}

// SyncResult は同期結果。
type SyncResult struct {
	SyncedCount int
	NewStreams  []string
	Updated     []string
	Skipped     []string
}

// Holodex の topic_id と seTORI の stream_tags.id は同じ語でも表記が一致しない。
// 特に Original_Song / Music_Cover はそのまま FK へ入れるとタグが付かないため、
// seTORI 側の安定した ID へ明示的に寄せる。
// キーは **小文字**（引く前に ToLower している）。Holodex は `Music_Cover` のように
// 大文字混じりで返すので、そのまま tag_id にすると FK に当たらない。
//
// **seTORI にある 17 タグのうち、Holodex が topic で表せるものは全部ここに書く。**
// 書き漏らすと「タグが付かない」としか現れない ── 実際 `membersonly` が
// 抜けていて、Holodex が会限と言っている 86 本の**すべて**で members_only タグが
// 付いていなかった（タイトルに「メン限」と書いてある 84 本だけがキーワード規則で
// 救われていた。2026-08-29 に実測）。
var holodexTopicTagAliases = map[string]string{
	"concert":       "concert",
	"karaoke":       "karaoke",
	"live":          "concert",
	"music_cover":   "music_cover",
	"music_video":   "mv",
	"mv":            "mv",
	"original_song": "original_song",
	"singing":       "singing",
	"shorts":        "shorts",
	// 以下は 2026-08-29 に追加。それまでは「表に無いので topic をそのまま試す」経路へ
	// 落ちて FK に当たらず、静かに捨てられていた。
	"membersonly": "members_only",
	"3d_stream":   "3d",
	"birthday":    "birthday",
	"anniversary": "anniversary",
}

// 対応する seTORI の配信タグ ID を返す。**表に無い topic は空を返す。**
//
// 以前は「同じ ID かもしれない」と原文のまま AddTag へ渡し、FK エラーを
// 握りつぶしていた。Holodex の topic はゲーム名がそのまま来る
// （Persona / genshin / Splatoon …）ので失敗が常態で、**その握りつぶしが
// `membersonly` の取りこぼしを隠していた**。対応させたいものは
// 同名でも上の表に明記する（`shorts`）。
func streamTagIDForHolodexTopic(topicID string) string {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return ""
	}
	return holodexTopicTagAliases[strings.ToLower(topicID)]
}

// SyncChannelInfo はチャンネル情報だけを同期し、配信は同期しない。
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

	// 人がチャンネルを名指しして追加した経路（POST /api/singers）なので、新規でも一覧に出す。
	singer := s.singerFromHolodexChannel(channel)
	if err := s.singerRepo.Upsert(singer, repository.SingerRequested); err != nil {
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

	// Holodex に無いチャンネルの退避経路。入口は同じ POST /api/singers なので人の意図がある。
	if err := s.singerRepo.Upsert(singer, repository.SingerRequested); err != nil {
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
	// YouTube のアバターを優先する
	photoURL := s.getChannelPhotoURL(channel.ID, channel.Photo)
	if photoURL != "" {
		singer.PhotoURL = sql.NullString{String: photoURL, Valid: true}
	}
	if org := strings.TrimSpace(channel.Org); org != "" {
		singer.Organization = sql.NullString{String: org, Valid: true}
	}

	return singer
}

// SyncChannel はチャンネルのすべての配信を同期する。
func (s *HolodexService) SyncChannel(channelID string, limit int, forceUpdate bool) (*dto.SyncHolodexResponse, error) {
	logger.Infof("チャンネル同期開始: %s", channelID)

	// 先にチャンネル情報を同期する
	channel, err := s.client.GetChannel(channelID)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}

	// 同期対象として名指しされたチャンネル本人なので、新規でも一覧に出す。
	singer := s.singerFromHolodexChannel(channel)
	if err := s.singerRepo.Upsert(singer, repository.SingerRequested); err != nil {
		return nil, fmt.Errorf("upsert singer: %w", err)
	}

	result := &dto.SyncHolodexResponse{
		NewStreams: []string{},
		Updated:    []string{},
		Skipped:    []string{},
		InProgress: true,
	}

	// ページングしてすべての歌枠を取得する
	const pageSize = 50
	offset := 0
	totalVideos := 0

	// 1) 自チャンネルが投稿した歌枠（channel_id フィルタ）
	for {
		videos, err := s.client.GetAllStreams(channelID, pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("get all streams: %w", err)
		}

		// 続きのデータがなければ終了する
		if len(videos) == 0 {
			break
		}

		totalVideos += len(videos)
		result.TotalStreams = totalVideos
		s.applySyncedVideos(videos, channelID, forceUpdate, result)

		// 返された件数が pageSize 未満なら最後のページ
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
			// エラーを記録して処理を続ける
			logger.Warnf("同期に失敗 (video: %s): %v", video.ID, err)
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

// syncVideo は動画を 1 件同期する。
func (s *HolodexService) syncVideo(video holodex.Video, channelID string, forceUpdate bool) (string, error) {
	// 既に存在するか確認する
	existing, err := s.streamRepo.FindByID(video.ID)
	if err != nil {
		return "", fmt.Errorf("find stream: %w", err)
	}

	// 既に存在し、強制更新モードでなければスキップする
	if existing != nil && !forceUpdate {
		return "skipped", nil
	}

	// Holodex データのハッシュを計算する
	holodexJSON, err := json.Marshal(video)
	if err != nil {
		return "", fmt.Errorf("marshal video: %w", err)
	}
	hash := sha256.Sum256(holodexJSON)
	hashStr := hex.EncodeToString(hash[:])

	// 日付を解析する
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

	// サムネイル URL を作成する
	thumbnailURL := fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", video.ID)
	stream.ThumbnailURL = sql.NullString{String: thumbnailURL, Valid: true}

	// Upsert Stream
	if err := s.streamRepo.Upsert(stream); err != nil {
		return "", fmt.Errorf("upsert stream: %w", err)
	}
	logger.Infof("[holodex] upserted stream %s (existing=%v, force=%v)", video.ID, existing != nil, forceUpdate)

	// Holodex topic_id -> seTORI stream tag。両者の ID は必ずしも同じではない。
	// 表に載っているものだけが来るので、ここでの失敗は異常（タグを画面から
	// 消した等）。**ただし同期は止めない** ── syncVideo の失敗は呼び出し元で
	// 「この動画は skip」になり、stream 行を作った直後なので中途半端に残る。
	// 見た目のタグ 1 つのために配信の取り込みを落とさない。名指しで警告する。
	if tagID := streamTagIDForHolodexTopic(video.TopicID); tagID != "" {
		if err := s.streamRepo.AddTag(video.ID, tagID); err != nil {
			logger.Warnf("[holodex] topic タグの付与に失敗 (video=%s, topic=%s, tag=%s): %v",
				video.ID, video.TopicID, tagID, err)
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

		// **会限は同期の時点で印を付けておく。** availability を取りに行くのは yt-dlp を
		// 呼んだときだけなので、それを待つ間このデータは公開側に置かれることになる。
		// 材料（Holodex の topic / タイトル規則で付く members_only タグ）はここで揃っている。
		if initialMembersOnlyCandidate(video.TopicID, tagIDs) {
			if err := s.streamRepo.MarkMembersOnly(video.ID); err != nil {
				return "", fmt.Errorf("mark members only: %w", err)
			}
		}
	}

	// 動画の所有者（アップロード元チャンネル）。コラボ動画の所有者は主催チャンネルであり、同期対象のチャンネルとは限らない。
	// そのため video からの取得を優先し、取れない場合だけ渡された channelID に戻る。
	ownerID := channelID
	if video.Channel != nil && video.Channel.ID != "" {
		ownerID = video.Channel.ID
	} else if video.ChannelID != "" {
		ownerID = video.ChannelID
	}
	// 所有者と同期対象のチャンネルが違う（＝コラボで、別のチャンネルが主催）場合、
	// stream_singers には singers への外部キーがあるため、先に所有者チャンネルを upsert する。
	// （自チャンネル同期では所有者を SyncChannel/SyncVideo で先に upsert 済み。情報量の少ない一覧データで上書きしないよう省略）
	// 同期を頼まれたのはこのチャンネルではなく、コラボの主催として付いてきただけなので、
	// 新規なら非表示で作る（stream_singers の FK を満たすのが目的）。
	if ownerID != channelID && video.Channel != nil && video.Channel.ID == ownerID {
		s.singerRepo.Upsert(s.singerFromHolodexChannel(video.Channel), repository.SingerDiscovered)
	}

	// mentions を処理して参加者を同期する
	// 言及されたチャンネルをすべて先に同期し、この配信の参加者に設定する
	singerIDs := []string{ownerID} // チャンネル所有者は必ず含める

	for _, mention := range video.Mentions {
		// 言及されたチャンネルを Singer として同期する
		singer := &models.Singer{
			ID:   mention.ID,
			Name: mention.Name,
		}
		if mention.EnglishName != "" {
			singer.EnglishName = sql.NullString{String: mention.EnglishName, Valid: true}
		}
		// YouTube のアバターを優先する
		mentionPhoto := s.getChannelPhotoURL(mention.ID, mention.Photo)
		if mentionPhoto != "" {
			singer.PhotoURL = sql.NullString{String: mentionPhoto, Valid: true}
		}
		if org := strings.TrimSpace(mention.Org); org != "" {
			singer.Organization = sql.NullString{String: org, Valid: true}
		}
		// mention は「配信に言及されていた」だけで、こちらが追いたいチャンネルとは限らない。
		// 新規なら非表示で作る（既存の表示設定は Upsert が触らない）。
		s.singerRepo.Upsert(singer, repository.SingerDiscovered)

		// 参加者一覧へ追加する（重複は避ける）
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

	// この配信の全参加者を設定する
	if err := s.streamRepo.SetSingers(video.ID, singerIDs, ownerID); err != nil {
		// エラーを記録するが同期は止めない
		fmt.Printf("set stream singers error: %v\n", err)
	}

	// 同期時点で HolodexData には songs を含む完全な video データが入っている
	// 別途保存する必要はなく、stream_service が必要に応じて HolodexData から解析する

	// 同期時にコメントデータを自動分析して保存する
	if existing == nil || forceUpdate {
		s.loadAndSaveComments(video.ID)
		logger.Infof("[holodex] triggered comment analysis for %s", video.ID)
	}

	if existing == nil {
		return "new", nil
	}
	return "updated", nil
}

// SyncVideo は動画を 1 件同期する（手動追加にも使用可能）。
func (s *HolodexService) SyncVideo(videoID string) (*dto.SyncHolodexResponse, error) {
	video, err := s.client.GetVideoWithSongs(videoID)
	if err != nil {
		return nil, fmt.Errorf("get video: %w", err)
	}

	channelID := video.ChannelID
	if video.Channel != nil {
		channelID = video.Channel.ID

		// チャンネル情報を同期する
		singer := &models.Singer{
			ID:   video.Channel.ID,
			Name: video.Channel.Name,
		}
		if video.Channel.EnglishName != "" {
			singer.EnglishName = sql.NullString{String: video.Channel.EnglishName, Valid: true}
		}
		// YouTube のアバターを優先する
		channelPhoto := s.getChannelPhotoURL(video.Channel.ID, video.Channel.Photo)
		if channelPhoto != "" {
			singer.PhotoURL = sql.NullString{String: channelPhoto, Valid: true}
		}
		if org := strings.TrimSpace(video.Channel.Org); org != "" {
			singer.Organization = sql.NullString{String: org, Valid: true}
		}
		// 名指しされたのは動画 1 本で、その所有者が誰かは結果として分かるだけ。
		// 自チャンネルなら既存行なので影響は無く、他人のチャンネルなら非表示で作る。
		s.singerRepo.Upsert(singer, repository.SingerDiscovered)
	}

	result := &dto.SyncHolodexResponse{
		NewStreams: []string{},
		Updated:    []string{},
		Skipped:    []string{},
	}

	// 動画 1 件の同期では常に強制更新する
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

// LoadHolodexSongs は Holodex から楽曲を読み込む（正規化キューには追加しない）。
func (s *HolodexService) LoadHolodexSongs(videoID string) (*dto.LoadHolodexSongsResponse, error) {
	video, err := s.client.GetVideoWithSongs(videoID)
	if err != nil {
		return nil, fmt.Errorf("get video: %w", err)
	}

	// チャンネル所有者の情報を取得する
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

	// すべての参加者を集める（チャンネル所有者 + mentions）
	participants := []dto.SingerResponse{channelOwner}
	allSingerIDs := []string{channelOwner.ID}

	for _, mention := range video.Mentions {
		// 重複を避ける
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

	// 楽曲データを変換する
	songs := make([]dto.SongSuggestion, len(video.Songs))
	for i, song := range video.Songs {
		songs[i] = dto.SongSuggestion{
			Name:           song.Name,
			OriginalArtist: song.OriginalArtist,
			StartSeconds:   song.Start,
			EndSeconds:     song.End,
			Tags:           []string{},
			SingerIDs:      allSingerIDs, // 既定はすべての参加者
		}
		if song.ArtURL != "" {
			songs[i].ArtURL = &song.ArtURL
		}
		// iTunes ID があれば追加する
		if song.ITunesID > 0 {
			itunesID := song.ITunesID
			songs[i].ItunesID = &itunesID
		}

		// 終了時刻がなければ次の曲の開始時刻を使う
		if songs[i].EndSeconds == 0 && i+1 < len(video.Songs) {
			songs[i].EndSeconds = video.Songs[i+1].Start
		}
	}

	// 注意：HolodexData には同期時に完全な Video JSON が保存されるため、ここでは DB に再保存しない
	// この API は主に都度読み込みとフロントエンドへの返却に使う

	return &dto.LoadHolodexSongsResponse{
		StreamID:     video.ID,
		StreamTitle:  video.Title,
		ChannelOwner: channelOwner,
		Participants: participants,
		Songs:        songs,
	}, nil
}

// GetVideoComments は動画の公開コメントを取得する（コメント分析用）。
// YouTube を正とし、未設定・リクエスト失敗・コメントなしの場合に Holodex を試す。
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

// loadAndSaveComments は同期時に「元コメント」だけを取得して保存する（YouTube 優先、Holodex へフォールバック）。
// 後続の分析で使えるよう準備する。
// AI 抽出／正規化／拍手 end 検出は同期時には実行しない（大量同期で AI/yt-dlp に負荷を集中させないため）。
// 編集ページで手動分析を実行したときだけ処理してキャッシュする（CommentService.AnalyzeComments を参照）。
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

// AnalyzeHolodexSongs は保存済み holodex_data から楽曲を解析し、正規化・DB 照合・拍手 end 補完を行って永続化する。
// 既存の holodex_hash をキャッシュキーとし、Holodex のデータが変わっていなければ
// **正規化の AI は**再実行しない。ただし未決着の照合 AI（adjudicateSuggestions）は
// キャッシュ命中でも走る ── 照合は保存していないため。
// Holodex が明示的に end を持つ曲はそれを優先（人力キュレーションのため最も正確）。
func (s *HolodexService) AnalyzeHolodexSongs(videoID string, force bool) ([]dto.SongSuggestion, error) {
	return s.analyzeHolodexSongs(videoID, force, true)
}

// AnalyzeHolodexSongsForBatch は一括セットリスト作成用。**AI 判定は行わない。**
// 一括は配信をまたいで 1 回にまとめて聞く（楽曲カタログを配信の数だけ送らないため。
// 実測で 58 回 → 9 回。詳細は docs/SETLIST_FLOW.md）。
func (s *HolodexService) AnalyzeHolodexSongsForBatch(videoID string, force bool) ([]dto.SongSuggestion, error) {
	return s.analyzeHolodexSongs(videoID, force, false)
}

func (s *HolodexService) analyzeHolodexSongs(videoID string, force, adjudicate bool) ([]dto.SongSuggestion, error) {
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

	// キャッシュ命中：holodex_data が変わっていない → 正規化はやり直さず chat 比較を付け直す。
	// **照合の AI 判定はこのあと走る**（未決着の行は保存されていないため）。
	if !force && holodexHash != "" {
		cached, cachedHash, _ := s.streamRepo.GetHolodexSongsCache(videoID)
		if cachedHash.Valid && cachedHash.String == holodexHash && len(cached) > 0 {
			var songs []dto.SongSuggestion
			if err := json.Unmarshal(cached, &songs); err == nil && len(songs) > 0 {
				// 曲名から導けるタグは AI を呼ばずに付け直す。
				// 規則を足したとき、holodex_data が変わらない限りキャッシュは
				// 命中し続けるので、ここで補わないと既存の配信は AI を
				// 回し直すまで直らない（純粋な文字列判定なので安い）
				for i := range songs {
					songs[i].Tags = perftag.Normalize(songs[i].Tags, songs[i].Name)
				}
				// 照合は保存していないので、今の DB に対して計算して返す
				s.normalizationService.ResolveSuggestionsForDisplay(songs)
				if adjudicate {
					s.adjudicateSuggestions(songs)
				}
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
		hasExplicitEnd[i] = sg.End > 0 // Holodex に明記された end（人手）
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
	if adjudicate {
		s.adjudicateSuggestions(songs)
	}

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
// **保存されるのは否定だけ**（song_identity_checks / artist_alias_checks）。肯定はその応答に
// 載るだけで、stripMatchFromSuggestions がキャッシュへ書く前に落とす。したがって
// 「一度読み込めば以後無料」ではなく、同じ配信をもう一度 analyze すれば再び AI に当たる。
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

// SyncSetoriToHolodex は seTORI のセットリストを Holodex へ書き戻す。
//
// **実際に外部へ書き込む**（`PUT https://holodex.net/api/v2/songs`）。運用者の
// 編集トークンの名義で向こうのデータベースに残り、seTORI からは取り消せない。
// 詳細とリスクは docs/EXTERNAL_APIS.md と CLAUDE.md §5。
//
// 秘匿された配信の中身は送らない（歌唱の取得が PublicAccess）。ただし
// **送ったあとに秘匿へ変わった場合、向こうのコピーは残ったまま**なので、
// 送信を試みた時刻を記録して編集画面に出す（migration 054）。撤回するかは人が決める。
//
// 記録は**送信の前**に書き、書けなければ送らない。記録が「送信済み」ではなく
// 「送信を試みた」を意味するのはこのため ── 実際に届いたかは Holodex 側でしか確かめられない。
func (s *HolodexService) SyncSetoriToHolodex(streamID string) (*dto.SyncHolodexResponse, error) {
	// 1. 配信情報を取得する
	stream, err := s.streamRepo.FindByID(streamID)
	if err != nil {
		return nil, fmt.Errorf("find stream: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("stream not found: %s", streamID)
	}

	// 2. editor token が設定済みか確認する
	if tok := s.editor(); tok == "" || tok == "your-holodex-editor-token-here" {
		return nil, fmt.Errorf("Holodex editor token not configured")
	}

	// 3. チャンネル情報を取得する（HolodexData から解析し、なければ最初の singer を予備として使う）
	var channelID, channelName, channelEnglishName string

	// まず HolodexData から channel ID の取得を試す
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

	// channel name がなければ DB から取得する
	if (channelName == "" || channelEnglishName == "") && channelID != "" {
		if s.singerRepo != nil {
			if singer, err := s.singerRepo.FindByID(channelID); err == nil && singer != nil {
				channelName = singer.Name
				channelEnglishName = singer.EnglishName.String
			}
		}
	}

	// 4. 先に Holodex から既存の楽曲一覧を取得する
	existingSongs := make(map[int64]bool) // iTunes ID をキーにする
	holodexVideo, err := s.client.GetVideoWithSongs(streamID)
	if err == nil && holodexVideo != nil {
		for _, song := range holodexVideo.Songs {
			if song.ITunesID > 0 {
				existingSongs[song.ITunesID] = true
				logger.Debugf("Found existing song in Holodex: iTunes ID %d (%s)", song.ITunesID, song.Name)
			}
		}
	}

	// 6. performance と対応する楽曲情報を取得する
	syncedCount := 0
	skippedCount := 0
	var errors []string

	if s.perfRepo != nil {
		// **Holodex への書き戻しは公開行為**（運用者の名義で外部に残る）。
		// 秘匿された配信の中身はここから出さない ── 公開してよいと決まってから、
		// 編集画面で秘匿を外したうえで実行する。
		perfs, err := s.perfRepo.FindByStreamID(streamID, repository.PublicAccess)
		if err == nil && len(perfs) > 0 {
			// **外部へ送る前に台帳を書く。書けなければ送らない。**
			// 外部 API と DB は同じ transaction に入れられないので、どちらかに倒すしかない。
			// 送ってから記録すると、PUT の直後に落ちた場合に「Holodex にはあるが記録は無い」
			// が残り、**警告が出ないことを安全の根拠にできなくなる**。
			// 逆に先に書けば、送信に失敗しても記録だけが残る（＝過剰な警告）で済む。
			if err := s.streamRepo.MarkHolodexUploadAttempt(streamID); err != nil {
				return nil, fmt.Errorf("送信の記録に失敗したため中止しました: %w", err)
			}
			for _, perf := range perfs {
				if s.songRepo != nil {
					song, err := s.songRepo.FindByID(perf.SongID)
					if err == nil && song != nil {
						// 基本のリクエストデータを組み立てる
						requestData := map[string]interface{}{
							"start":           perf.StartSeconds,
							"end":             perf.EndSeconds,
							"name":            song.Name,
							"original_artist": song.OriginalArtist,
							"video_id":        streamID,
							"channel_id":      channelID,
							"available_at":    stream.StreamDate.Format(time.RFC3339),
						}

						// チャンネル情報を追加する
						if channelName != "" {
							channelObj := map[string]interface{}{
								"name":         channelName,
								"english_name": channelEnglishName,
							}
							requestData["channel"] = channelObj
						}

						// iTunes 情報の取得を試す（primary iTunes ID だけを使う）
						var primaryItunesID int64 = 0
						if s.songItunesRepo != nil {
							itunesRecords, err := s.songItunesRepo.FindBySongID(perf.SongID)
							if err == nil && len(itunesRecords) > 0 {
								// primary iTunes ID だけを使う
								var primaryItunes models.SongITunes
								foundPrimary := false
								for _, record := range itunesRecords {
									if record.IsPrimary {
										primaryItunes = record
										foundPrimary = true
										break
									}
								}

								// primary の印がなければ最初のレコードを使う
								if !foundPrimary {
									primaryItunes = itunesRecords[0]
								}

								primaryItunesID = primaryItunes.ITunesID

								// iTunes ID がある場合だけ、この楽曲が Holodex に既に存在するか確認する
								if existingSongs[primaryItunesID] {
									logger.Debugf("⊘ Skipped: %s (iTunes: %d) - already exists in Holodex", song.Name, primaryItunesID)
									skippedCount++
									continue
								}

								// iTunes から完全な情報を取得する
								itunesInfo, err := s.itunesClient.QueryByID(primaryItunesID)

								// iTunes ID を設定する
								requestData["itunesid"] = primaryItunesID

								// iTunes 情報があれば完全な song オブジェクトと URL を追加する
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

						// iTunes ID がなくてもアップロードする（null にして Musicdex source を追加）
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

						// Holodex API へリクエストを送る
						// Holodex API へリクエストを送る
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

						// defer の蓄積による接続リークを避けるため、ループ内で都度閉じる
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

	message := fmt.Sprintf("同期完了：%d 曲を同期しました", syncedCount)
	if skippedCount > 0 {
		message = fmt.Sprintf("同期完了：%d 曲を同期し、%d 曲は登録済みでした", syncedCount, skippedCount)
	}
	if len(errors) > 0 {
		message = fmt.Sprintf("同期完了：%d 曲を同期、%d 曲は登録済み、%d 曲は失敗しました", syncedCount, skippedCount, len(errors))
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
