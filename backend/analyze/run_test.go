package analyze

import (
	"errors"
	"testing"
)

func TestLoad_InvalidURL(t *testing.T) {
	_, err := Load(t.Context(), "https://gitlab.com/x/y/pull/1", "")
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("got %v", err)
	}
}
