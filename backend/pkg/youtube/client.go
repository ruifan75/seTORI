package youtube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

// ChannelLookup YouTube チャンネル検索条件
type ChannelLookup struct {
	ID     string
	Handle string
}

// ParseChannelLookup Channel ID、@handle、YouTube URL を検索条件に変換する
func ParseChannelLookup(input string) ChannelLookup {
	raw := strings.TrimSpace(input)
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ChannelLookup{}
	}

	normalizedURL := raw
	if strings.Contains(raw, "youtube.com/") && !strings.Contains(raw, "://") {
		normalizedURL = "https://" + raw
	}

	if parsed, err := url.Parse(normalizedURL); err == nil && parsed.Host != "" && isYouTubeHost(parsed.Host) {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) > 0 {
			if len(parts) >= 2 && parts[0] == "channel" && parts[1] != "" {
				return ChannelLookup{ID: parts[1]}
			}
			if strings.HasPrefix(parts[0], "@") {
				return ChannelLookup{Handle: normalizeHandle(parts[0])}
			}
		}
	}

	if strings.HasPrefix(raw, "@") {
		return ChannelLookup{Handle: normalizeHandle(raw)}
	}
	if looksLikeChannelID(raw) {
		return ChannelLookup{ID: raw}
	}

	return ChannelLookup{Handle: normalizeHandle(raw)}
}

func isYouTubeHost(host string) bool {
	host = strings.ToLower(host)
	return host == "youtube.com" || host == "www.youtube.com" || strings.HasSuffix(host, ".youtube.com")
}

func looksLikeChannelID(value string) bool {
	return strings.HasPrefix(value, "UC") && len(value) >= 20
}

func normalizeHandle(handle string) string {
	handle = strings.TrimSpace(handle)
	handle = strings.Trim(handle, "/")
	if handle == "" {
		return ""
	}
	if strings.HasPrefix(handle, "@") {
		return handle
	}
	return "@" + handle
}

// FindChannel Channel ID または @handle からチャンネル情報を取得
func (c *Client) FindChannel(input string) (*Channel, error) {
	lookup := ParseChannelLookup(input)
	if lookup.ID != "" {
		return c.GetChannel(lookup.ID)
	}
	if lookup.Handle != "" {
		return c.GetChannelByHandle(lookup.Handle)
	}
	return nil, fmt.Errorf("channel input is required")
}

// GetChannel チャンネルIDでチャンネル情報を取得
func (c *Client) GetChannel(channelID string) (*Channel, error) {
	return c.getChannelByFilter("id", strings.TrimSpace(channelID))
}

// GetChannelByHandle @handle でチャンネル情報を取得
func (c *Client) GetChannelByHandle(handle string) (*Channel, error) {
	handle = normalizeHandle(handle)
	if handle == "" {
		return nil, fmt.Errorf("channel handle is required")
	}

	return c.getChannelByFilter("forHandle", handle)
}

func (c *Client) getChannelByFilter(filterName string, filterValue string) (*Channel, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("YouTube API key not configured")
	}
	if strings.TrimSpace(filterValue) == "" {
		return nil, fmt.Errorf("channel %s is required", filterName)
	}

	params := url.Values{}
	params.Set("key", c.apiKey)
	params.Set("part", "snippet")
	params.Set(filterName, filterValue)

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
		return nil, fmt.Errorf("channel not found for %s: %s", filterName, filterValue)
	}

	return &result.Items[0], nil
}

// BestThumbnailURL チャンネルアバター URL を高解像度優先で取得
func BestThumbnailURL(channel *Channel) string {
	if channel == nil {
		return ""
	}

	if channel.Snippet.Thumbnails.High.URL != "" {
		return channel.Snippet.Thumbnails.High.URL
	}
	if channel.Snippet.Thumbnails.Medium.URL != "" {
		return channel.Snippet.Thumbnails.Medium.URL
	}
	if channel.Snippet.Thumbnails.Default.URL != "" {
		return channel.Snippet.Thumbnails.Default.URL
	}

	return ""
}

// GetChannelPhoto チャンネルアバター URL を取得（高解像度を優先）
func (c *Client) GetChannelPhoto(channelID string) (string, error) {
	channel, err := c.GetChannel(channelID)
	if err != nil {
		return "", err
	}

	thumbnailURL := BestThumbnailURL(channel)
	if thumbnailURL != "" {
		return thumbnailURL, nil
	}

	return "", fmt.Errorf("no thumbnail found for channel: %s", channelID)
}

// IsConfigured API Key が設定されているかを確認
func (c *Client) IsConfigured() bool {
	return c != nil && c.apiKey != ""
}
