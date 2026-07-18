package youtube

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestParseChannelLookup(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID string
		wantAt string
	}{
		{
			name:   "channel id",
			input:  "UCIkst7OxFkITPZ9yodgmujA",
			wantID: "UCIkst7OxFkITPZ9yodgmujA",
		},
		{
			name:   "channel url",
			input:  "https://www.youtube.com/channel/UCIkst7OxFkITPZ9yodgmujA",
			wantID: "UCIkst7OxFkITPZ9yodgmujA",
		},
		{
			name:   "channel url without scheme",
			input:  "youtube.com/channel/UCIkst7OxFkITPZ9yodgmujA",
			wantID: "UCIkst7OxFkITPZ9yodgmujA",
		},
		{
			name:   "handle",
			input:  "@yosumi",
			wantAt: "@yosumi",
		},
		{
			name:   "handle without at sign",
			input:  "yosumi",
			wantAt: "@yosumi",
		},
		{
			name:   "handle url",
			input:  "https://www.youtube.com/@yosumi/videos",
			wantAt: "@yosumi",
		},
		{
			name:   "handle url without scheme",
			input:  "youtube.com/@yosumi",
			wantAt: "@yosumi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseChannelLookup(tt.input)
			if got.ID != tt.wantID {
				t.Fatalf("ID = %q, want %q", got.ID, tt.wantID)
			}
			if got.Handle != tt.wantAt {
				t.Fatalf("Handle = %q, want %q", got.Handle, tt.wantAt)
			}
		})
	}
}

func TestBestThumbnailURL(t *testing.T) {
	channel := &Channel{}
	channel.Snippet.Thumbnails.Default.URL = "default.jpg"
	channel.Snippet.Thumbnails.Medium.URL = "medium.jpg"
	channel.Snippet.Thumbnails.High.URL = "high.jpg"

	if got := BestThumbnailURL(channel); got != "high.jpg" {
		t.Fatalf("BestThumbnailURL() = %q, want high.jpg", got)
	}

	channel.Snippet.Thumbnails.High.URL = ""
	if got := BestThumbnailURL(channel); got != "medium.jpg" {
		t.Fatalf("BestThumbnailURL() = %q, want medium.jpg", got)
	}

	channel.Snippet.Thumbnails.Medium.URL = ""
	if got := BestThumbnailURL(channel); got != "default.jpg" {
		t.Fatalf("BestThumbnailURL() = %q, want default.jpg", got)
	}
}

func TestListVideoCommentsPaginatesAndUsesPlainText(t *testing.T) {
	requestCount := 0
	client := NewClient("test-key")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		query := req.URL.Query()
		if req.URL.Path != "/youtube/v3/commentThreads" {
			t.Fatalf("path = %q, want /youtube/v3/commentThreads", req.URL.Path)
		}
		if query.Get("videoId") != "6Pjm0GvbWsw" {
			t.Fatalf("videoId = %q", query.Get("videoId"))
		}
		if query.Get("part") != "snippet" || query.Get("maxResults") != "100" {
			t.Fatalf("unexpected list params: %s", req.URL.RawQuery)
		}
		if query.Get("textFormat") != "plainText" || query.Get("order") != "relevance" {
			t.Fatalf("unexpected text/order params: %s", req.URL.RawQuery)
		}

		switch query.Get("pageToken") {
		case "":
			return jsonResponse(http.StatusOK, `{
				"nextPageToken":"page-2",
				"items":[
					{"snippet":{"topLevelComment":{"snippet":{"textOriginal":"12:23 言って。","textDisplay":"ignored"}}}},
					{"snippet":{"topLevelComment":{"snippet":{"textDisplay":"19:34 ソラニン"}}}}
				]
			}`), nil
		case "page-2":
			return jsonResponse(http.StatusOK, `{
				"items":[{"snippet":{"topLevelComment":{"snippet":{"textOriginal":"29:06 恋愛サーキュレーション"}}}}]
			}`), nil
		default:
			t.Fatalf("unexpected page token %q", query.Get("pageToken"))
			return nil, nil
		}
	})}

	got, err := client.ListVideoComments("6Pjm0GvbWsw")
	if err != nil {
		t.Fatalf("ListVideoComments() error = %v", err)
	}
	want := []string{"12:23 言って。", "19:34 ソラニン", "29:06 恋愛サーキュレーション"}
	if len(got) != len(want) {
		t.Fatalf("got %d comments, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("comment[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
}

func TestListVideoCommentsReturnsAPIError(t *testing.T) {
	client := NewClient("test-key")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusForbidden, `{"error":{"message":"quota exceeded"}}`), nil
	})}

	_, err := client.ListVideoComments("6Pjm0GvbWsw")
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("error = %v, want status 403", err)
	}
}

func TestListVideoCommentsRequiresAPIKey(t *testing.T) {
	_, err := NewClient("").ListVideoComments("6Pjm0GvbWsw")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error = %v, want not configured", err)
	}
}
