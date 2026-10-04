package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ruifan75/setori/pkg/comment"
)

// benchmark が実際に使う後処理に入力し、期待する残存曲をリテラルで固定する。
func TestBenchPostprocessScope(t *testing.T) {
	parsed := []comment.ParsedSong{
		{Start: 10, Name: "Lemon", OriginalComment: "0:10 Lemon"},
		{Start: 100, Name: "Week End", OriginalComment: "1:40 Week End"},
		{Start: 200, Name: "1", OriginalComment: "3:20 1 / original"},
		{Start: 300, Name: "📸", OriginalComment: "5:00 📸 / original"},
		{Start: 400, Name: "Happy End", OriginalComment: "6:40 Happy End / original"},
	}
	for _, tt := range []struct {
		path string
		want []string
	}{
		{"grouped", []string{"Lemon", "Week End", "Happy End"}},
		{"two_stage", []string{"Lemon", "Week End", "Happy End"}},
		{"combined", []string{"Lemon", "Week End", "Happy End"}},
		{"stored", []string{"Lemon", "Week End", "Happy End"}},
		{"regex", []string{"Lemon", "1", "Happy End"}},
	} {
		t.Run(tt.path, func(t *testing.T) {
			got := postprocess(parsed, tt.path, []string{"end"}, []string{"original"}, true, false)
			assertSongNames(t, got, tt.want)
		})
	}
}

func TestBenchFilterFlags(t *testing.T) {
	parsed := []comment.ParsedSong{
		{Start: 10, Name: "Lemon"},
		{Start: 10, Name: "Lemon"}, // -nofilter でも重複排除は通る
		{Start: 100, Name: "Week End"},
		{Start: 200, Name: "📸"},
		{Start: -100, Name: "Invalid"}, // -nofilter でも妥当性検証は通る
	}
	for _, tt := range []struct {
		name, path           string
		structural, noFilter bool
		want                 []string
	}{
		{"ai_struct_off", "grouped", false, false, []string{"Lemon", "Week End", "📸"}},
		{"regex_struct_off", "regex", false, false, []string{"Lemon", "📸"}},
		{"ai_nofilter", "grouped", true, true, []string{"Lemon", "Week End", "📸"}},
		{"regex_nofilter", "regex", true, true, []string{"Lemon", "Week End", "📸"}},
		{"stored_nofilter", "stored", true, true, []string{"Lemon", "Week End", "📸"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := postprocess(parsed, tt.path, []string{"end"}, nil, tt.structural, tt.noFilter)
			assertSongNames(t, got, tt.want)
		})
	}
}

type benchAI struct {
	response string
	err      error
	calls    int
}

func (s *benchAI) SimpleChat(_, _ string) (string, error) {
	s.calls++
	return s.response, s.err
}

func TestBenchExtractionAndFiltering(t *testing.T) {
	comments := []string{"1:40 Week End / 星野源\n10:00 Lemon / 米津玄師"}
	for _, tt := range []struct {
		mode, path, response string
	}{
		{"grouped", "grouped", `[{"src":[1],"ts":"1:40","nv":"Week End","av":"星野源"},{"src":[2],"ts":"10:00","nv":"Lemon","av":"米津玄師"}]`},
		{"ai", "two_stage", `[{"line":1,"is_song":true,"start_ts":"1:40","name":"Week End","artist":"星野源"},{"line":2,"is_song":true,"start_ts":"10:00","name":"Lemon","artist":"米津玄師"}]`},
		{"combined", "combined", `[{"l":1,"s":true,"ts":"1:40","nv":"Week End","av":"星野源"},{"l":2,"s":true,"ts":"10:00","nv":"Lemon","av":"米津玄師"}]`},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			for _, fail := range []bool{false, true} {
				name := "success"
				stub := &benchAI{response: tt.response}
				wantPath := tt.path
				wantNames := []string{"Week End", "Lemon"}
				if fail {
					name = "regex_fallback"
					stub.err = errors.New("AI unavailable")
					wantPath = "regex"
					wantNames = []string{"Lemon"}
				}
				t.Run(name, func(t *testing.T) {
					cache := map[string]cachedExtraction{}
					parsed, path := extract(tt.mode, "stream", comments, stub, cache)
					if stub.calls != 1 || path != wantPath {
						t.Fatalf("AI calls = %d, path = %q; want 1, %q", stub.calls, path, wantPath)
					}
					assertSongNames(t, postprocess(parsed, path, []string{"end"}, nil, true, false), wantNames)

					// ディスクから読み戻した結果も、成功／退避の経路を維持する。
					cachePath := filepath.Join(t.TempDir(), "cache.json")
					saveCache(cachePath, cache)
					loaded := loadCache(cachePath)
					cachedSongs, cachedPath := extract(tt.mode, "stream", comments, stub, loaded)
					if stub.calls != 1 || cachedPath != wantPath {
						t.Fatalf("cached: AI calls = %d, path = %q; want 1, %q", stub.calls, cachedPath, wantPath)
					}
					assertSongNames(t, postprocess(cachedSongs, cachedPath, []string{"end"}, nil, true, false), wantNames)
				})
			}
		})
	}

	t.Run("regex", func(t *testing.T) {
		stub := &benchAI{err: errors.New("must not call AI")}
		parsed, path := extract("regex", "stream", comments, stub, nil)
		if stub.calls != 0 || path != "regex" {
			t.Fatalf("AI calls = %d, path = %q; want 0, regex", stub.calls, path)
		}
		assertSongNames(t, postprocess(parsed, path, []string{"end"}, nil, true, false), []string{"Lemon"})
	})
}

func TestBenchCacheModeIsolation(t *testing.T) {
	cache := map[string]cachedExtraction{
		"stream": {Mode: "grouped", Path: "grouped", Songs: []comment.ParsedSong{{Name: "Wrong mode"}}},
	}
	stub := &benchAI{response: `[{"line":1,"is_song":true,"start_ts":"0:10","name":"Lemon"}]`}
	songs, path := extract("ai", "stream", []string{"0:10 Lemon"}, stub, cache)
	if stub.calls != 1 || path != "two_stage" {
		t.Fatalf("AI calls = %d, path = %q; want 1, two_stage", stub.calls, path)
	}
	assertSongNames(t, songs, []string{"Lemon"})
}

func TestBenchLegacyCacheRequiresReextraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(path, []byte(`{"stream":[{"name":"Week End","start":100}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := loadCache(path); len(got) != 0 {
		t.Fatalf("cache without extraction path accepted: %+v", got)
	}
}

func assertSongNames(t *testing.T, songs []comment.ParsedSong, want []string) {
	t.Helper()
	var got []string
	for _, song := range songs {
		got = append(got, song.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("songs = %q, want %q", got, want)
	}
}
