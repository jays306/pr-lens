package cli

import (
	"testing"

	"github.com/pr-lens/backend/analysis"
)

func TestReviewCommentsFromEvents(t *testing.T) {
	events := []analysis.StreamEvent{
		{Type: "category", Data: map[string]any{
			"reviewQuestions": []any{
				map[string]any{"text": "Check nil user.", "file": "a.go", "lineStart": 10.0, "side": "RIGHT"},
				map[string]any{"text": "", "file": "a.go", "lineStart": 11.0},
				map[string]any{"text": "No file.", "lineStart": 12.0},
			},
		}},
	}
	got := ReviewComments(events)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Path != "a.go" || got[0].Line != 10 || got[0].Body != "Check nil user." || got[0].Side != "RIGHT" {
		t.Fatalf("got %+v", got[0])
	}
}

func TestRecommendationAndEvent(t *testing.T) {
	events := []analysis.StreamEvent{
		{Type: "recommendation", Data: map[string]any{"action": "request_changes", "reason": "Fix the nil path."}},
	}
	action, reason := Recommendation(events)
	if action != "request_changes" || reason != "Fix the nil path." {
		t.Fatalf("%s %s", action, reason)
	}
	if GitHubEvent("approve") != "APPROVE" {
		t.Fatal(GitHubEvent("approve"))
	}
	if GitHubEvent("request_changes") != "REQUEST_CHANGES" {
		t.Fatal(GitHubEvent("request_changes"))
	}
	if GitHubEvent("needs_review") != "COMMENT" {
		t.Fatal(GitHubEvent("needs_review"))
	}
}
