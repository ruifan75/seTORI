package service

import (
	"testing"

	"github.com/ruifan75/setori/pkg/comment"
)

type fakeCommentAI struct{ response string }

func (f fakeCommentAI) SimpleChat(_, _ string) (string, error) { return f.response, nil }

func TestParseCommentsAIZeroSongsNoRegexFallback(t *testing.T) {
	svc := &CommentService{
		aiClient: fakeCommentAI{response: `[{"line":1,"is_song":false,"start_ts":"1:00","name":"","artist":""}]`},
	}

	comments := []string{"1:00 ちゃんと靴までみました!"}
	got := svc.parseComments(comments)
	if len(got) != 0 {
		t.Fatalf("got %d songs, want 0 (AI said not a song; must not regex-fallback): %+v", len(got), got)
	}

	// regex-only would incorrectly extract a song name from the timestamp line
	regexGot := comment.ParseComments(comments)
	if len(regexGot) != 1 {
		t.Fatalf("regex baseline: got %d songs, want 1", len(regexGot))
	}
}

func TestHashStoredCommentsDoesNotCacheEmptyValues(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("null"), []byte("[]"), []byte("not-json")} {
		if got := hashStoredComments(raw); got != "" {
			t.Errorf("hashStoredComments(%q) = %q, want empty", raw, got)
		}
	}
}

func TestHashStoredCommentsNormalizesJSONFormatting(t *testing.T) {
	compact := hashStoredComments([]byte(`["12:23 言って。","19:34 ソラニン"]`))
	formatted := hashStoredComments([]byte(`["12:23 言って。", "19:34 ソラニン"]`))
	if compact == "" {
		t.Fatal("hashStoredComments() returned empty for non-empty comments")
	}
	if compact != formatted {
		t.Fatalf("hash differs by JSON formatting: %q != %q", compact, formatted)
	}
}
