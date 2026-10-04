package handler

import (
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseCSVQueryParam(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/streams/search?tags=singing%2C3d%2Csinging%2C%20%2Ccollaboration", nil)

	got := parseCSVQueryParam(req, "tags")
	want := []string{"singing", "3d", "collaboration"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCSVQueryParam() = %#v, want %#v", got, want)
	}
}

func TestParseCSVQueryParamEmpty(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/streams/search", nil)

	if got := parseCSVQueryParam(req, "tags"); got != nil {
		t.Fatalf("parseCSVQueryParam() = %#v, want nil", got)
	}
}

func TestParseUUIDCSVQueryParam(t *testing.T) {
	req := httptest.NewRequest(
		"GET",
		"/api/performances/random?exclude_song_ids=550e8400-e29b-41d4-a716-446655440000%2Cinvalid%2C550E8400-E29B-41D4-A716-446655440000%2C6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		nil,
	)

	got := parseUUIDCSVQueryParam(req, "exclude_song_ids")
	want := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseUUIDCSVQueryParam() = %#v, want %#v", got, want)
	}
}

func TestParseIDQueryParamsCombinesNewAndLegacyValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/streams/search?participant_ids=a%2Cb&participant_id=b&singer_id=c", nil)

	got := parseIDQueryParams(req, "participant_ids", "participant_id", "singer_id")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseIDQueryParams() = %#v, want %#v", got, want)
	}
}

// 配信検索の4条件とも同じIDを2回指定しても1条件になる。旧名との混在も保つ。
func TestStreamSearchDuplicateQueryIDs(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/streams/search?participant_ids=owner,guest,owner&participant_id=guest&singer_id=owner&vocalist_ids=voice,guest,voice&vocalist_id=guest&tags=singing,3d,singing&performance_tags=acoustic,piano,acoustic", nil)
	for _, tc := range []struct {
		name      string
		got, want []string
	}{
		{"participant", parseIDQueryParams(req, "participant_ids", "participant_id", "singer_id"), []string{"owner", "guest"}},
		{"vocalist", parseIDQueryParams(req, "vocalist_ids", "vocalist_id"), []string{"voice", "guest"}},
		{"stream-tag", parseCSVQueryParam(req, "tags"), []string{"singing", "3d"}},
		{"performance-tag", parseCSVQueryParam(req, "performance_tags"), []string{"acoustic", "piano"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Fatalf("%s: got=%v want=%v", tc.name, tc.got, tc.want)
			}
		})
	}
}
