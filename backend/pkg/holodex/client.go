package holodex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ruifan75/setori/pkg/ratelimit"
)

const baseURL = "https://holodex.net/api/v2"

// Client Holodex API 客戶端
type Client struct {
	apiKey      string
	httpClient  *http.Client
	rateLimiter *ratelimit.RateLimiter
}

// NewClient 建立新的 Holodex 客戶端
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		// Holodex API 限制: 80 requests per 2 minutes
		rateLimiter: ratelimit.NewRateLimiter(75, 2*time.Minute), // 設定 75 避免邊界情況
	}
}

// Video 影片資料
type Video struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Type           string    `json:"type"`
	TopicID        string    `json:"topic_id"`
	PublishedAt    string    `json:"published_at"`
	AvailableAt    string    `json:"available_at"`
	Duration       int       `json:"duration"`
	Status         string    `json:"status"`
	StartScheduled string    `json:"start_scheduled"`
	StartActual    string    `json:"start_actual"`
	EndActual      string    `json:"end_actual"`
	LiveViewers    int       `json:"live_viewers"`
	Description    string    `json:"description"`
	SongCount      int       `json:"songcount"`
	ChannelID      string    `json:"channel_id"`
	Channel        *Channel  `json:"channel"`
	Songs          []Song    `json:"songs"`
	Comments       []Comment `json:"comments"`
	Mentions       []Channel `json:"mentions"` // 參與者（被提及的頻道）
}

// Channel 頻道資料
type Channel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	EnglishName string `json:"english_name"`
	Type        string `json:"type"`
	Photo       string `json:"photo"`
	Org         string `json:"org"`
	Suborg      string `json:"suborg"`
	VideoCount  int    `json:"video_count"`
	Subscriber  int    `json:"subscriber_count"`
	ClipCount   int    `json:"clip_count"`
	Description string `json:"description"`
}

// Song 歌曲資料（來自 Holodex）
type Song struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	OriginalArtist string `json:"original_artist"`
	ArtURL         string `json:"art"`
	ITunesID       int64  `json:"itunesid"`
	Start          int    `json:"start"`
	End            int    `json:"end"`
}

// iTunes 歌曲資料
type ITunesSong struct {
	TrackID         int64  `json:"trackId"`
	TrackTimeMillis int    `json:"trackTimeMillis"`
	CollectionName  string `json:"collectionName"`
	ReleaseDate     string `json:"releaseDate"`
	ArtistName      string `json:"artistName"`
	TrackName       string `json:"trackName"`
	ArtworkUrl100   string `json:"artworkUrl100"`
	TrackViewUrl    string `json:"trackViewUrl"`
}

// AddSongsRequest 新增歌曲到 Holodex 的請求
type AddSongsRequest struct {
	Song           *ITunesSong `json:"song"`
	ItunesID       int64       `json:"itunesid"`
	Start          int         `json:"start"`
	End            int         `json:"end"`
	Name           string      `json:"name"`
	OriginalArtist string      `json:"original_artist"`
	AmUrl          string      `json:"amUrl,omitempty"`
	Art            string      `json:"art,omitempty"`
	VideoID        string      `json:"video_id"`
	ChannelID      string      `json:"channel_id"`
	Channel        *Channel    `json:"channel,omitempty"`
	AvailableAt    string      `json:"available_at,omitempty"`
}

// Comment 評論資料
type Comment struct {
	CommentKey string `json:"comment_key"`
	Message    string `json:"message"`
}

// GetChannelVideos 取得頻道的影片列表
func (c *Client) GetChannelVideos(channelID string, videoType string, limit int, offset int) ([]Video, error) {
	params := url.Values{}
	params.Set("channel_id", channelID)
	params.Set("type", videoType)
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("include", "songs,mentions")

	var videos []Video
	err := c.get("/videos", params, &videos)
	if err != nil {
		return nil, fmt.Errorf("get channel videos: %w", err)
	}

	return videos, nil
}

// GetVideo 取得單一影片詳情
func (c *Client) GetVideo(videoID string) (*Video, error) {
	params := url.Values{}
	params.Set("c", "1") // 包含 comments

	var video Video
	err := c.get("/videos/"+videoID, params, &video)
	if err != nil {
		return nil, fmt.Errorf("get video: %w", err)
	}

	return &video, nil
}

// GetVideoWithSongs 取得影片及其歌曲清單
func (c *Client) GetVideoWithSongs(videoID string) (*Video, error) {
	var video Video
	err := c.get("/videos/"+videoID, nil, &video)
	if err != nil {
		return nil, fmt.Errorf("get video with songs: %w", err)
	}

	return &video, nil
}

// GetChannel 取得頻道資訊
func (c *Client) GetChannel(channelID string) (*Channel, error) {
	var channel Channel
	err := c.get("/channels/"+channelID, nil, &channel)
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}

	return &channel, nil
}

// SearchVideos 搜尋影片
func (c *Client) SearchVideos(query string, topic string, limit int) ([]Video, error) {
	params := url.Values{}
	if query != "" {
		params.Set("q", query)
	}
	if topic != "" {
		params.Set("topic", topic)
	}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("include", "songs,mentions")

	var videos []Video
	err := c.get("/search/videoSearch", params, &videos)
	if err != nil {
		return nil, fmt.Errorf("search videos: %w", err)
	}

	return videos, nil
}

// GetAllStreams 取得所有直播影片
func (c *Client) GetAllStreams(channelID string, limit int, offset int) ([]Video, error) {
	params := url.Values{}
	params.Set("channel_id", channelID)
	params.Set("type", "stream")
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("include", "songs,mentions")
	params.Set("status", "past")

	var videos []Video
	err := c.get("/videos", params, &videos)
	if err != nil {
		return nil, fmt.Errorf("get all streams: %w", err)
	}

	return videos, nil
}

// get 執行 GET 請求
func (c *Client) get(endpoint string, params url.Values, result interface{}) error {
	// 使用 rate limiter 等待直到可以發送請求
	c.rateLimiter.Wait()

	reqURL := baseURL + endpoint
	if params != nil {
		reqURL += "?" + params.Encode()
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-APIKEY", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: status=%d body=%s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	return nil
}

// AddSongs 新增歌曲到 Holodex
func (c *Client) AddSongs(songs []AddSongsRequest) error {
	if len(songs) == 0 {
		return nil
	}

	reqURL := baseURL + "/songs"

	// 逐個添加歌曲
	for _, song := range songs {
		// 使用 rate limiter 等待直到可以發送請求
		c.rateLimiter.Wait()

		body, err := json.Marshal(song)
		if err != nil {
			return fmt.Errorf("marshal song: %w", err)
		}

		req, err := http.NewRequest("PUT", reqURL, io.NopCloser(io.Reader(bytes.NewBuffer(body))))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("X-APIKEY", c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("do request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("add song API error: status=%d body=%s", resp.StatusCode, string(respBody))
		}
	}

	return nil
}
