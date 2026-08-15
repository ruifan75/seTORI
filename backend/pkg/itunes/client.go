package itunes

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SearchResult は iTunes の検索結果。
type SearchResult struct {
	ItunesID       int64  `json:"itunes_id"`
	CollectionName string `json:"collection_name"`
	TrackName      string `json:"track_name"`
	ArtistName     string `json:"artist_name"`
	ArtworkURL     string `json:"artwork_url"`
	Country        string `json:"country"`
}

// SearchResponse は iTunes の検索レスポンス。
type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

// QueryResponse は iTunes の照会レスポンス。
type QueryResponse struct {
	ItunesID        int64  `json:"itunes_id"`
	CollectionName  string `json:"collection_name"`
	TrackName       string `json:"track_name"`
	ArtistName      string `json:"artist_name"`
	ArtworkURL      string `json:"artwork_url"`
	TrackViewURL    string `json:"track_view_url"`
	TrackTimeMillis int64  `json:"track_time_millis"`
	PreviewURL      string `json:"preview_url"`
	Country         string `json:"country"`
}

// itunesSearchResponse は Apple Music API の検索レスポンス構造。
type itunesSearchResponse struct {
	Results []itunesResult `json:"results"`
}

type itunesResult struct {
	TrackID          int64  `json:"trackId"`
	TrackName        string `json:"trackName"`
	ArtistName       string `json:"artistName"`
	CollectionName   string `json:"collectionName"`
	TrackViewUrl     string `json:"trackViewUrl"`
	TrackTimeMillis  int64  `json:"trackTimeMillis"`
	ArtworkUrl100    string `json:"artworkUrl100"`
	ArtworkUrl30     string `json:"artworkUrl30"`
	ArtworkUrl60     string `json:"artworkUrl60"`
	ArtworkUrl600    string `json:"artworkUrl600"`
	Country          string `json:"country"`
	PrimaryGenreName string `json:"primaryGenreName"`
	PreviewUrl       string `json:"previewUrl"`
}

// Client は iTunes API クライアント。
type Client struct {
	httpClient *http.Client
}

// NewClient は新しい iTunes クライアントを作成する。
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Search は iTunes を検索する。
func (c *Client) Search(term string) (*SearchResponse, error) {
	// URL を組み立て、クエリパラメータを正しくエンコードする
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

	// 結果の形式を変換する
	results := make([]SearchResult, len(itunesResp.Results))
	for i, r := range itunesResp.Results {
		// 600x600 のアートワークを優先し、なければ 100x100 にフォールバックする
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

// QueryByID は iTunes ID で詳細情報を照会する。
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

	// 600x600 のアートワークを優先する
	artworkURL := r.ArtworkUrl600
	if artworkURL == "" {
		artworkURL = r.ArtworkUrl100
	}

	return &QueryResponse{
		ItunesID:        r.TrackID,
		CollectionName:  r.CollectionName,
		TrackName:       r.TrackName,
		ArtistName:      r.ArtistName,
		ArtworkURL:      artworkURL,
		TrackViewURL:    r.TrackViewUrl,
		TrackTimeMillis: r.TrackTimeMillis,
		PreviewURL:      r.PreviewUrl,
		Country:         r.Country,
	}, nil
}
