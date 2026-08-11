package service

import "testing"

func TestStreamTagIDForHolodexTopic(t *testing.T) {
	tests := []struct {
		name       string
		topic      string
		wantTag    string
		wantMapped bool
	}{
		{name: "original song", topic: "Original_Song", wantTag: "original_song", wantMapped: true},
		{name: "music cover", topic: "Music_Cover", wantTag: "music_cover", wantMapped: true},
		{name: "singing", topic: "singing", wantTag: "singing", wantMapped: true},
		{name: "karaoke case insensitive", topic: "Karaoke", wantTag: "karaoke", wantMapped: true},
		{name: "live", topic: "Live", wantTag: "concert", wantMapped: true},
		{name: "music video", topic: "Music_Video", wantTag: "mv", wantMapped: true},
		{name: "existing identical topic", topic: "shorts", wantTag: "shorts", wantMapped: false},
		{name: "unknown topic", topic: "Outfit_Reveal", wantTag: "Outfit_Reveal", wantMapped: false},
		{name: "trim spaces", topic: "  Original_Song  ", wantTag: "original_song", wantMapped: true},
		{name: "empty", topic: "  ", wantTag: "", wantMapped: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTag, gotMapped := streamTagIDForHolodexTopic(tt.topic)
			if gotTag != tt.wantTag || gotMapped != tt.wantMapped {
				t.Fatalf("streamTagIDForHolodexTopic(%q) = (%q, %v), want (%q, %v)",
					tt.topic, gotTag, gotMapped, tt.wantTag, tt.wantMapped)
			}
		})
	}
}

func TestVisibleMusicStreamTagIDs(t *testing.T) {
	want := map[string]bool{
		"concert":       true,
		"karaoke":       true,
		"music_cover":   true,
		"mv":            true,
		"original_song": true,
		"singing":       true,
	}
	if len(visibleMusicStreamTagIDs) != len(want) {
		t.Fatalf("visibleMusicStreamTagIDs has %d values, want %d", len(visibleMusicStreamTagIDs), len(want))
	}
	for _, tagID := range visibleMusicStreamTagIDs {
		if !want[tagID] {
			t.Errorf("unexpected visible music tag %q", tagID)
		}
		delete(want, tagID)
	}
	for tagID := range want {
		t.Errorf("missing visible music tag %q", tagID)
	}
}
