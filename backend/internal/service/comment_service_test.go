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
