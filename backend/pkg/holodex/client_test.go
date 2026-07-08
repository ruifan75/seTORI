package holodex

import (
	"fmt"
	"net/http"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	err := fmt.Errorf("get channel: %w", &APIError{StatusCode: http.StatusNotFound, Body: "missing"})
	if !IsNotFound(err) {
		t.Fatal("IsNotFound() = false, want true")
	}

	err = fmt.Errorf("get channel: %w", &APIError{StatusCode: http.StatusInternalServerError, Body: "error"})
	if IsNotFound(err) {
		t.Fatal("IsNotFound() = true, want false")
	}
}
