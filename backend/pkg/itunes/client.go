package itunes

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// SearchResult iTunes 搜尋結果
type SearchResult struct {
	ItunesID       int64  `json:"itunes_id"`
	CollectionName string `json:"collection_name"`
	TrackName      string `json:"track_name"`
	ArtistName     string `json:"artist_name"`
	ArtworkURL     string `json:"artwork_url"`
	Country        string `json:"country"`
}

// SearchResponse iTunes 搜尋回應
type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

// QueryResponse iTunes 查詢回應
type QueryResponse struct {
	ItunesID       int64  `json:"itunes_id"`
	CollectionName string `json:"collection_name"`
	TrackName      string `json:"track_name"`
	ArtistName     string `json:"artist_name"`
	ArtworkURL     string `json:"artwork_url"`
	TrackViewURL   string `json:"track_view_url"`
	Country        string `json:"country"`
}

// itunesSearchResponse Apple Music API 搜尋回應結構
type itunesSearchResponse struct {
	Results []itunesResult `json:"results"`
}

type itunesResult struct {
	TrackID          int64  `json:"trackId"`
	TrackName        string `json:"trackName"`
	ArtistName       string `json:"artistName"`
	CollectionName   string `json:"collectionName"`
	TrackViewUrl     string `json:"trackViewUrl"`
	ArtworkUrl100    string `json:"artworkUrl100"`
	ArtworkUrl30     string `json:"artworkUrl30"`
	ArtworkUrl60     string `json:"artworkUrl60"`
	ArtworkUrl600    string `json:"artworkUrl600"`
	Country          string `json:"country"`
	PrimaryGenreName string `json:"primaryGenreName"`
}

// Client iTunes API 客戶端
type Client struct {
	httpClient *http.Client
}

// NewClient 建立新的 iTunes 客戶端
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{},
	}
}

// Search 搜尋 iTunes
func (c *Client) Search(term string) (*SearchResponse, error) {
	// 構建 URL 並正確編碼查詢參數
	params := url.Values{}
	params.Add("term", term)
	params.Add("entity", "song")
	params.Add("limit", "10")
	params.Add("country", "JP")

	searchURL := "https://itunes.apple.com/search?" + params.Encode()

	resp, err := c.httpClient.Get(searchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to search iTunes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("iTunes API returned status %d: %s", resp.StatusCode, string(body))
	}

	var itunesResp itunesSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&itunesResp); err != nil {
		return nil, fmt.Errorf("failed to decode iTunes response: %w", err)
	}

	// 轉換結果格式
	results := make([]SearchResult, len(itunesResp.Results))
	for i, r := range itunesResp.Results {
		// 優先使用 600x600 的封面，降級為 100x100
		artworkURL := r.ArtworkUrl600
		if artworkURL == "" {
			artworkURL = r.ArtworkUrl100
		}

		results[i] = SearchResult{
			ItunesID:       r.TrackID,
			CollectionName: r.CollectionName,
			TrackName:      r.TrackName,
			ArtistName:     r.ArtistName,
			ArtworkURL:     artworkURL,
			Country:        r.Country,
		}
	}

	return &SearchResponse{
		Results: results,
	}, nil
}

// QueryByID 通過 iTunes ID 查詢詳細資訊
func (c *Client) QueryByID(itunesID int64) (*QueryResponse, error) {
	params := url.Values{}
	params.Add("id", fmt.Sprintf("%d", itunesID))
	params.Add("entity", "song")
	params.Add("country", "JP")
	params.Add("lang", "ja_jp")

	queryURL := "https://itunes.apple.com/lookup?" + params.Encode()

	resp, err := c.httpClient.Get(queryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query iTunes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("iTunes API returned status %d: %s", resp.StatusCode, string(body))
	}

	var itunesResp itunesSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&itunesResp); err != nil {
		return nil, fmt.Errorf("failed to decode iTunes response: %w", err)
	}

	if len(itunesResp.Results) == 0 {
		return nil, fmt.Errorf("iTunes ID %d not found", itunesID)
	}

	r := itunesResp.Results[0]

	// 優先使用 600x600 的封面
	artworkURL := r.ArtworkUrl600
	if artworkURL == "" {
		artworkURL = r.ArtworkUrl100
	}

	return &QueryResponse{
		ItunesID:       r.TrackID,
		CollectionName: r.CollectionName,
		TrackName:      r.TrackName,
		ArtistName:     r.ArtistName,
		ArtworkURL:     artworkURL,
		TrackViewURL:   r.TrackViewUrl,
		Country:        r.Country,
	}, nil
}
