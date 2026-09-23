package analysis

import (
	"strings"
	"testing"
)

func TestAboveThreshold_BelowBoth(t *testing.T) {
	files := make([]PRFile, 1)
	diff := strings.Repeat("a\n", 10)
	if aboveThreshold(files, diff) {
		t.Error("expected below threshold for 1 file and 10 diff lines")
	}
}

func TestAboveThreshold_ByFileCount(t *testing.T) {
	files := make([]PRFile, 10)
	diff := strings.Repeat("a\n", 10)
	if !aboveThreshold(files, diff) {
		t.Error("expected above threshold for 10 files")
	}
}

func TestAboveThreshold_ByLineCount(t *testing.T) {
	files := make([]PRFile, 1)
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
	var b strings.Builder
	for range 2100 {
		b.WriteString("line\n")
	}
	result := first2000Lines(b.String())
	if strings.Count(result, "\n") > 2001 {
		t.Errorf("expected at most 2001 newlines, got %d", strings.Count(result, "\n"))
	}
	if !strings.Contains(result, "truncated") {
		t.Error("expected truncation marker in long output")
	}
}

func TestRecommendationPrompt_OmitsSettledIntentQuestions(t *testing.T) {
	events := []StreamEvent{{
		Type: "category",
		Data: map[string]any{
			"id":        "logic",
			"riskLevel": "medium",
			"summary":   "Year regex widened; tests updated. ErrConflict still returns 500.",
			"reviewQuestions": []any{
				map[string]any{"text": "The year regex now matches 2015 — intended?"},
				map[string]any{"text": "Should ErrConflict also return 422?"},
			},
		},
	}}
	got := recommendationPrompt(events)
	if strings.Contains(got, "intended?") {
		t.Fatalf("settled intent question reached the recommendation prompt:\n%s", got)
	}
	if !strings.Contains(got, "Should ErrConflict also return 422?") {
		t.Fatalf("real mismatch was dropped:\n%s", got)
	}
	if !strings.Contains(got, "test-updated") {
		t.Fatal("recommendation prompt should say test-updated changes are settled")
	}
}
