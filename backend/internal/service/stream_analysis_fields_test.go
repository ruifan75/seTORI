package service

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/ruifan75/setori/internal/models"
)

// 解析結果（編集画面だけが読む中間生成物）が閲覧向けの応答から確実に消えること。
//
// **実際の変換を通す。** 空の DTO を marshal するだけのテストにしていた時期があるが、
// それだと toStreamResponse の分岐を消しても通ってしまい、何も守れない。
func TestToStreamResponseAnalysisFields(t *testing.T) {
	svc := &StreamService{}
	stream := models.Stream{
		ID:         "abc123",
		Title:      "テスト配信",
		StreamDate: time.Now(),
		// 解析結果を一通り持たせる（落ちるべきものが本当に落ちるかを見るため）
		CommentSongs:           []byte(`[{"start":10,"name":"曲","original_comment":"0:10 曲"}]`),
		ChapterSongs:           []byte(`[{"start":20,"name":"章節の曲"}]`),
		CommentRaw:             []byte(`["0:10 曲"]`),
		ChapterRaw:             []byte(`[{"start_time":0,"title":"START"}]`),
		CommentSongsAnalyzedAt: sql.NullTime{Time: time.Now(), Valid: true},
	}

	// 解析欄位（編集者だけ）と、閲覧に要る欄位
	analysisKeys := []string{
		"holodex_timeline_songs", "comment_timeline_songs", "chapter_timeline_songs",
		"comment_songs_analyzed_at", "has_comment_raw", "chapter_count",
	}
	viewerKeys := []string{"id", "title", "stream_date", "tags", "participants", "is_hidden", "is_processed"}

	keysOf := func(t *testing.T, includeAnalysis bool) map[string]bool {
		t.Helper()
		resp := svc.toStreamResponse(stream, nil, nil, nil, includeAnalysis)
		encoded, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &m); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for k := range m {
			out[k] = true
		}
		return out
	}

	t.Run("閲覧者には解析結果を一切載せない", func(t *testing.T) {
		got := keysOf(t, false)
		for _, k := range analysisKeys {
			if got[k] {
				t.Errorf("%s が閲覧向けの応答に載っている", k)
			}
		}
		for _, k := range viewerKeys {
			if !got[k] {
				t.Errorf("%s が閲覧向けの応答から消えている（落としすぎ）", k)
			}
		}
	})

	t.Run("編集者には載せる", func(t *testing.T) {
		got := keysOf(t, true)
		// holodex_timeline_songs は HolodexData を持たせていないので対象外
		for _, k := range []string{
			"comment_timeline_songs", "chapter_timeline_songs",
			"comment_songs_analyzed_at", "has_comment_raw", "chapter_count",
		} {
			if !got[k] {
				t.Errorf("%s が編集者向けの応答に載っていない（編集画面が壊れる）", k)
			}
		}
	})

	// chapter_count は 3 態（-1 未調査 / 0 調べたが無い / 正の数）。
	// 伏せるときに 0 を返すと「調べたが無い」という別の事実を主張してしまうので、
	// ポインタにして省略する形になっている。
	t.Run("未調査の章節を 0 と偽らない", func(t *testing.T) {
		s2 := stream
		s2.ChapterRaw = nil // 未調査
		resp := svc.toStreamResponse(s2, nil, nil, nil, true)
		if resp.ChapterCount == nil || *resp.ChapterCount != -1 {
			t.Errorf("編集者には -1（未調査）を返すべき: %v", resp.ChapterCount)
		}
		if got := svc.toStreamResponse(s2, nil, nil, nil, false); got.ChapterCount != nil {
			t.Errorf("閲覧者には省略すべき: %v", *got.ChapterCount)
		}
	})
}
