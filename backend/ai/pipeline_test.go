package ai

import (
	"strings"
	"testing"

	ghclient "github.com/just-pr/backend/github"
)

func TestAboveThreshold_BelowBoth(t *testing.T) {
	files := make([]ghclient.PRFile, 5)
	diff := strings.Repeat("a\n", 100)
	if aboveThreshold(files, diff) {
		t.Error("expected below threshold for 5 files and 100 diff lines")
	}
}

func TestAboveThreshold_ByFileCount(t *testing.T) {
	files := make([]ghclient.PRFile, 10)
	diff := strings.Repeat("a\n", 100)
	if !aboveThreshold(files, diff) {
		t.Error("expected above threshold for 10 files")
	}
}

func TestAboveThreshold_ByLineCount(t *testing.T) {
	files := make([]ghclient.PRFile, 3)
	diff := strings.Repeat("a\n", 500)
	if !aboveThreshold(files, diff) {
		t.Error("expected above threshold for 500 diff lines")
	}
}

func TestFirst2000Lines_Short(t *testing.T) {
	s := "line1\nline2\nline3\n"
	if first2000Lines(s) != s {
		t.Error("expected short string returned unchanged")
	}
}

func TestFirst2000Lines_Long(t *testing.T) {
	// Build a string with 2100 lines
	var b strings.Builder
	for i := 0; i < 2100; i++ {
		b.WriteString("line\n")
	}
	result := first2000Lines(b.String())
	lineCount := strings.Count(result, "\n")
	if lineCount > 2001 { // 2000 lines + possible truncation marker line
		t.Errorf("expected at most 2001 newlines, got %d", lineCount)
	}
	if !strings.Contains(result, "truncated") {
		t.Error("expected truncation marker in long output")
	}
}
