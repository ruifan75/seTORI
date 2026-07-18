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

func TestParseIDQueryParamsCombinesNewAndLegacyValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/streams/search?participant_ids=a%2Cb&participant_id=b&singer_id=c", nil)

	got := parseIDQueryParams(req, "participant_ids", "participant_id", "singer_id")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseIDQueryParams() = %#v, want %#v", got, want)
	}
}
