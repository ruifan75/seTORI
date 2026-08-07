package service

import (
	"errors"
	"strings"
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
	got, warning := svc.parseComments(comments)
	if len(got) != 0 {
		t.Fatalf("got %d songs, want 0 (AI said not a song; must not regex-fallback): %+v", len(got), got)
	}
	if warning != "" {
		t.Errorf("AI は成功しているので警告は空のはず: %q", warning)
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

// failingCommentAI は常に失敗する Chatter。
type failingCommentAI struct{ err error }

func (f failingCommentAI) SimpleChat(_, _ string) (string, error) { return "", f.err }

// AI が失敗したとき、正規表現へ退避するだけでなく「劣化した」ことを伝えなければならない。
//
// これを黙って返すと、AI 障害が「この配信には曲が無い」という見た目で通り、
// さらにその空の結果がキャッシュへ保存されて既存の分析結果を上書きしてしまう。
// 実際に 2026-08-07、モデル差し替え時の 400 エラーでこれが起き、
// 10 曲あった配信の comment_songs が 0 件になった。
func TestParseCommentsReportsAIFailure(t *testing.T) {
	svc := &CommentService{
		aiClient: failingCommentAI{err: errors.New("status=400 unsupported parameter")},
	}

	comments := []string{"1:00 ちゃんと靴までみました!"}
	got, warning := svc.parseComments(comments)

	if warning == "" {
		t.Fatal("AI が失敗したのに警告が空。呼び出し側が劣化に気づけない")
	}
	if !strings.Contains(warning, "400") {
		t.Errorf("原因が警告に含まれていない: %q", warning)
	}
	// 退避自体は行われる（何も返さないよりはマシなので）
	if len(got) != 1 {
		t.Errorf("正規表現への退避が行われていない: %d 件", len(got))
	}
}

// AI client が未設定の場合は「劣化」ではないので警告を出さない。
func TestParseCommentsNoAIClientIsNotADegradation(t *testing.T) {
	svc := &CommentService{}
	_, warning := svc.parseComments([]string{"1:00 テスト"})
	if warning != "" {
		t.Errorf("AI 未設定は劣化ではないので警告不要: %q", warning)
	}
}

// タイムスタンプ行が 1 つも無いのは「AI の失敗」ではなく、単に解析対象が無いだけ。
// これを劣化として扱うと、毎回警告が出るうえキャッシュも書かれず、
// そういう配信を何度も無駄に再解析し続けることになる。
func TestParseCommentsNoTimestampLinesIsNotADegradation(t *testing.T) {
	svc := &CommentService{
		aiClient: fakeCommentAI{response: `[]`}, // ここまで到達しない
	}

	got, warning := svc.parseComments([]string{"今日も楽しかったです", "かわいい"})

	if warning != "" {
		t.Errorf("解析対象が無いだけなので警告は不要: %q", warning)
	}
	if len(got) != 0 {
		t.Errorf("抽出結果は空のはず: %+v", got)
	}
}
