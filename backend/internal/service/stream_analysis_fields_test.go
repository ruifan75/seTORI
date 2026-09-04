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
		// HolodexData を持たせないと holodex_timeline_songs を固定できず、
		// 将来この組み立てだけが早期 return より前へ動いても気付けない。
		HolodexData:            []byte(`{"id":"abc123","songs":[{"name":"Holodex の曲","start":30,"end":90}]}`),
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
	viewerKeys := []string{"id", "title", "stream_date", "tags", "participants", "is_hidden"}
	// 運用の状態。閲覧者には意味が無く、「まだ手を付けていない配信」の一覧を
	// 外から作れてしまうので content:edit のときだけ載せる。
	operationalKeys := []string{"is_processed"}

	keysOf := func(t *testing.T, view streamView) map[string]bool {
		t.Helper()
		resp := svc.toStreamResponse(stream, nil, nil, nil, view)
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

	t.Run("閲覧者には解析結果も運用の状態も載せない", func(t *testing.T) {
		got := keysOf(t, streamView{})
		for _, k := range append(append([]string{}, analysisKeys...), operationalKeys...) {
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

	t.Run("編集者の詳細には解析結果も運用の状態も載せる", func(t *testing.T) {
		got := keysOf(t, editorView(true))
		for _, k := range append(append([]string{}, analysisKeys...), operationalKeys...) {
			if !got[k] {
				t.Errorf("%s が編集者向けの応答に載っていない（編集画面が壊れる）", k)
			}
		}
	})

	// 一覧は「解析結果は常に載せない／運用の状態は権限があれば載せる」で、
	// 2 つの旗の効き方が違う。ここを取り違えると、未処理の絞り込みが
	// 編集画面で使えなくなるか、解析結果が一覧から漏れるかのどちらかになる。
	t.Run("一覧は権限があっても解析結果を載せないが、運用の状態は載せる", func(t *testing.T) {
		got := keysOf(t, listView(true))
		for _, k := range analysisKeys {
			if got[k] {
				t.Errorf("%s が一覧に載っている（編集者でも載せない）", k)
			}
		}
		for _, k := range operationalKeys {
			if !got[k] {
				t.Errorf("%s が編集者の一覧に載っていない（未処理の絞り込みが使えない）", k)
			}
		}
	})

	t.Run("権限の無い一覧には運用の状態を載せない", func(t *testing.T) {
		got := keysOf(t, listView(false))
		for _, k := range operationalKeys {
			if got[k] {
				t.Errorf("%s が未ログインの一覧に載っている", k)
			}
		}
	})

	// chapter_count は 3 態（-1 未調査 / 0 調べたが無い / 正の数）。
	// 伏せるときに 0 を返すと「調べたが無い」という別の事実を主張してしまうので、
	// ポインタにして省略する形になっている。
	t.Run("chapter_count の 3 態", func(t *testing.T) {
		cases := []struct {
			label string
			raw   []byte
			want  int
		}{
			{"まだ調べていない", nil, -1},
			{"調べたが章節が無い", []byte(`[]`), 0},
			{"章節がある", []byte(`[{"start_time":0,"title":"A"},{"start_time":9,"title":"B"}]`), 2},
		}
		for _, c := range cases {
			t.Run(c.label, func(t *testing.T) {
				s2 := stream
				s2.ChapterRaw = c.raw
				got := svc.toStreamResponse(s2, nil, nil, nil, editorView(true))
				if got.ChapterCount == nil || *got.ChapterCount != c.want {
					t.Errorf("編集者 = %v, want %d", got.ChapterCount, c.want)
				}
				// 伏せるときに 0 を返すと「調べたが章節が無い」という別の事実になる
				if v := svc.toStreamResponse(s2, nil, nil, nil, streamView{}); v.ChapterCount != nil {
					t.Errorf("閲覧者には省略すべき: %v", *v.ChapterCount)
				}
			})
		}
	})
}
