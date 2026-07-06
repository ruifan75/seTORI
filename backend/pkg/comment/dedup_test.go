package comment

import "testing"

func TestDeduplicateSongsPrefersStartFromCommentWithExplicitEnd(t *testing.T) {
	songs := []ParsedSong{
		{
			Start:              600,
			End:                0,
			Name:               "Stand By You",
			OriginalArtist:     "Official髭男dism",
			OriginalComment:    "10:00 Stand By You/Official髭男dism",
			IsEndTimeEstimated: true,
		},
		{
			Start:              605,
			End:                848,
			Name:               "Stand By You",
			OriginalArtist:     "Official髭男dism",
			OriginalComment:    "10:05 14:08 Stand By You/Official髭男dism",
			IsEndTimeEstimated: false,
		},
	}

	got := DeduplicateSongs(songs)
	if len(got) != 1 {
		t.Fatalf("len(DeduplicateSongs) = %d, want 1: %+v", len(got), got)
	}
	if got[0].Start != 605 {
		t.Fatalf("start = %d, want explicit-end comment start 605", got[0].Start)
	}
	if got[0].End != 848 {
		t.Fatalf("end = %d, want 848", got[0].End)
	}
	if got[0].IsEndTimeEstimated {
		t.Fatalf("IsEndTimeEstimated = true, want false")
	}
}

func TestDeduplicateSongsKeepsExistingStartWhenExistingHasExplicitEnd(t *testing.T) {
	songs := []ParsedSong{
		{
			Start:              605,
			End:                848,
			Name:               "Stand By You",
			OriginalArtist:     "Official髭男dism",
			OriginalComment:    "10:05 14:08 Stand By You/Official髭男dism",
			IsEndTimeEstimated: false,
		},
		{
			Start:              600,
			End:                0,
			Name:               "Stand By You",
			OriginalArtist:     "Official髭男dism",
			OriginalComment:    "10:00 Stand By You/Official髭男dism",
			IsEndTimeEstimated: true,
		},
	}

	got := DeduplicateSongs(songs)
	if len(got) != 1 {
		t.Fatalf("len(DeduplicateSongs) = %d, want 1: %+v", len(got), got)
	}
	if got[0].Start != 605 {
		t.Fatalf("start = %d, want existing explicit-end comment start 605", got[0].Start)
	}
	if got[0].End != 848 {
		t.Fatalf("end = %d, want 848", got[0].End)
	}
}
