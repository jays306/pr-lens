package cli

import (
	"strings"

	"github.com/pr-lens/backend/analysis"
	"github.com/pr-lens/backend/github"
)

// ReviewComments collects inline findings from category events.
func ReviewComments(events []analysis.StreamEvent) []github.ReviewComment {
	var out []github.ReviewComment
	for _, ev := range events {
		if ev.Type != "category" {
			continue
		}
		for _, q := range objectSlice(ev.Data["reviewQuestions"]) {
			text, _ := q["text"].(string)
			file, _ := q["file"].(string)
			line := asInt(q["lineStart"])
			side, _ := q["side"].(string)
			if strings.TrimSpace(text) == "" || file == "" || line <= 0 {
				continue
			}
			out = append(out, github.ReviewComment{
				Path: file,
				Line: line,
				Body: text,
				Side: side,
			})
		}
	}
	return out
}

// Recommendation returns the model action and reason, if present.
func Recommendation(events []analysis.StreamEvent) (action, reason string) {
	for _, ev := range events {
		if ev.Type != "recommendation" {
			continue
		}
		action, _ = ev.Data["action"].(string)
		reason, _ = ev.Data["reason"].(string)
	}
	return action, reason
}

// GitHubEvent maps a model recommendation onto a Reviews API event.
func GitHubEvent(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approve":
		return "APPROVE"
	case "request_changes":
		return "REQUEST_CHANGES"
	default:
		return "COMMENT"
	}
}
