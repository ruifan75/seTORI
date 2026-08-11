package service

import "testing"

func TestInitialStreamHidden(t *testing.T) {
	tests := []struct {
		name          string
		topicID       string
		duration      int
		durationKnown bool
		tagIDs        []string
		wantHidden    bool
	}{
		{
			name:          "short original song stays hidden",
			topicID:       "shorts",
			duration:      25,
			durationKnown: true,
			tagIDs:        []string{"original_song", "shorts"},
			wantHidden:    true,
		},
		{
			name:          "long singing stream with shorts tag stays visible",
			topicID:       "singing",
			duration:      5773,
			durationKnown: true,
			tagIDs:        []string{"shorts", "singing"},
			wantHidden:    false,
		},
		{
			name:          "long singing stream survives wrong shorts topic",
			topicID:       "shorts",
			duration:      5773,
			durationKnown: true,
			tagIDs:        []string{"shorts", "singing"},
			wantHidden:    false,
		},
		{
			name:          "short clip with singing title tag stays hidden",
			topicID:       "shorts",
			duration:      30,
			durationKnown: true,
			tagIDs:        []string{"shorts", "singing"},
			wantHidden:    true,
		},
		{
			name:          "short full MV without shorts signal stays visible",
			topicID:       "music_video",
			duration:      179,
			durationKnown: true,
			tagIDs:        []string{"mv"},
			wantHidden:    false,
		},
		{
			name:          "full original song stays visible",
			topicID:       "original_song",
			duration:      240,
			durationKnown: true,
			tagIDs:        []string{"original_song"},
			wantHidden:    false,
		},
		{
			name:          "unknown non-music video stays hidden",
			topicID:       "talking",
			duration:      3600,
			durationKnown: true,
			tagIDs:        nil,
			wantHidden:    true,
		},
		{
			name:          "shorts with unknown duration is conservative",
			topicID:       "shorts",
			durationKnown: false,
			tagIDs:        []string{"original_song", "shorts"},
			wantHidden:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := initialStreamHidden(tt.topicID, tt.duration, tt.durationKnown, tt.tagIDs)
			if got != tt.wantHidden {
				t.Fatalf("initialStreamHidden() = %v, want %v", got, tt.wantHidden)
			}
		})
	}
}
