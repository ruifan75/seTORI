package service

import (
	"strings"
	"testing"
)

func TestSanitizeActivityPathDropsSensitiveParts(t *testing.T) {
	cases := map[string]string{
		"/search?q=secret":        "/search",
		"/login/oauth?code=token": "/login/oauth",
		"/songs/abc#player":       "/songs/abc",
		"not-a-path":              "/",
		"":                        "/",
	}
	for input, want := range cases {
		if got := sanitizeActivityPath(input); got != want {
			t.Errorf("sanitizeActivityPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSanitizeActivityPathLimitsLength(t *testing.T) {
	got := sanitizeActivityPath("/" + strings.Repeat("歌", 600))
	if len([]rune(got)) != maxActivityPathLength {
		t.Fatalf("length = %d, want %d", len([]rune(got)), maxActivityPathLength)
	}
}

func TestNormalizeActivityQuery(t *testing.T) {
	days, page, limit := normalizeActivityQuery(999, 0, 999, 30)
	if days != 30 || page != 1 || limit != 100 {
		t.Fatalf("got (%d,%d,%d), want (30,1,100)", days, page, limit)
	}
}
