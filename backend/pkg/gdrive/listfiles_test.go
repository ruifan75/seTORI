package gdrive

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// assertListQuery は**要求した条件**を検査する。
//
// 偽の応答だけを見ると、要求の側を壊しても通ってしまう ── 実際、
// `fields` から `nextPageToken` を外す（実 API が次ページを返さなくなる）／
// `orderBy` の `desc` を外す（**新しいものから消す**順序になる）のどちらも
// 素通りした（issue #64 のレビューで実証）。
func assertListQuery(t *testing.T, r *http.Request, folderID string) {
	t.Helper()
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	// **完全一致で見る。** 「悪い形」を列挙して弾く方式では追いつかない ──
	// `in parents and` を `in parents or` にする（フォルダ外も並ぶ）、
	// `name` を `originalFilename` にする（`name` を部分文字列として含むので
	// 素通りするのに、応答に名前が来なくなる）など、通る形がいくらでも作れる。
	// 期待する文字列を丸ごと固定すれば、変えた人は必ずここを通る。
	if want := "'" + folderID + "' in parents and trashed = false"; q.Get("q") != want {
		t.Errorf("q = %q, want %q", q.Get("q"), want)
	}
	if want := "nextPageToken,files(id,name,size,createdTime,mimeType)"; q.Get("fields") != want {
		t.Errorf("fields = %q, want %q", q.Get("fields"), want)
	}
	// **降順であること。** 昇順だと世代整理が「古いものを残して新しいものを消す」
	// になり、最新のバックアップから失われる。
	if want := "createdTime desc"; q.Get("orderBy") != want {
		t.Errorf("orderBy = %q, want %q", q.Get("orderBy"), want)
	}
}

// rtFunc は RoundTripper をその場で作るための小道具。
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResp(v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(string(b))),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// **全ページを辿ること。** 1 ページ（100 件）で打ち切ると、フォルダを複数環境で
// 共有しているときに自分の古いファイルが範囲外へ押し出され、世代整理が
// 永久に効かなくなる（issue #64 のレビューで判明）。
func TestListFilesFollowsAllPages(t *testing.T) {
	c := NewClient("id", "secret")
	pages := 0
	c.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		assertListQuery(t, r, "folder")
		q, _ := url.ParseQuery(r.URL.RawQuery)
		switch q.Get("pageToken") {
		case "":
			pages++
			return jsonResp(map[string]any{
				"files":         []File{{ID: "a"}, {ID: "b"}},
				"nextPageToken": "p2",
			}), nil
		case "p2":
			pages++
			return jsonResp(map[string]any{"files": []File{{ID: "c"}}}), nil
		}
		t.Errorf("想定外の pageToken: %q", q.Get("pageToken"))
		return jsonResp(map[string]any{}), nil
	}))

	files, err := c.ListFiles("tok", "folder")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if pages != 2 {
		t.Errorf("辿ったページ数 = %d, want 2", pages)
	}
	if len(files) != 3 {
		t.Fatalf("件数 = %d, want 3（後続ページを捨てている）", len(files))
	}
	for i, want := range []string{"a", "b", "c"} {
		if files[i].ID != want {
			t.Errorf("files[%d].ID = %q, want %q（順序が壊れている）", i, files[i].ID, want)
		}
	}
}

// **黙って打ち切らない。** 途中までの一覧を返すと、世代整理が「自分のは
// これで全部」と誤解して古いものを消さないまま終わる。
func TestListFilesFailsInsteadOfTruncating(t *testing.T) {
	c := NewClient("id", "secret")
	c.SetTransport(rtFunc(func(r *http.Request) (*http.Response, error) {
		assertListQuery(t, r, "folder")
		// 常に次のページがあると言い続ける。
		return jsonResp(map[string]any{"files": []File{{ID: "x"}}, "nextPageToken": "next"}), nil
	}))
	if _, err := c.ListFiles("tok", "folder"); err == nil {
		t.Error("上限に当たっても err を返していない（途中までの一覧を全部だと誤解させる）")
	}
}
