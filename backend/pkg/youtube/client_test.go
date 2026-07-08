package youtube

import "testing"

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
