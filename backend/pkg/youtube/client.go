package youtube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const baseURL = "https://www.googleapis.com/youtube/v3"

// Client YouTube API Client
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient 新しい YouTube クライアントを作成
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ChannelSnippet YouTube Channel Snippet
type ChannelSnippet struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Thumbnails  struct {
		Default struct {
			URL string `json:"url"`
		} `json:"default"`
		Medium struct {
			URL string `json:"url"`
		} `json:"medium"`
		High struct {
			URL string `json:"url"`
		} `json:"high"`
	} `json:"thumbnails"`
}

// Channel YouTube チャンネル情報
type Channel struct {
	ID      string         `json:"id"`
	Snippet ChannelSnippet `json:"snippet"`
}

// ChannelListResponse YouTube Channel List API レスポンス
type ChannelListResponse struct {
	Items []Channel `json:"items"`
}

// GetChannel チャンネル情報を取得
func (c *Client) GetChannel(channelID string) (*Channel, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("YouTube API key not configured")
	}

	params := url.Values{}
	params.Set("key", c.apiKey)
	params.Set("part", "snippet")
	params.Set("id", channelID)

	reqURL := fmt.Sprintf("%s/channels?%s", baseURL, params.Encode())

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result ChannelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("channel not found: %s", channelID)
	}

	return &result.Items[0], nil
}

// GetChannelPhoto チャンネルアバター URL を取得（高解像度を優先）
func (c *Client) GetChannelPhoto(channelID string) (string, error) {
	channel, err := c.GetChannel(channelID)
	if err != nil {
		return "", err
	}

	// 高解像度を優先使用
	if channel.Snippet.Thumbnails.High.URL != "" {
		return channel.Snippet.Thumbnails.High.URL, nil
	}
	if channel.Snippet.Thumbnails.Medium.URL != "" {
		return channel.Snippet.Thumbnails.Medium.URL, nil
	}
	if channel.Snippet.Thumbnails.Default.URL != "" {
		return channel.Snippet.Thumbnails.Default.URL, nil
	}

	return "", fmt.Errorf("no thumbnail found for channel: %s", channelID)
}

// IsConfigured API Key が設定されているかを確認
func (c *Client) IsConfigured() bool {
	return c.apiKey != ""
}
