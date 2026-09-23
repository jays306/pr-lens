package cli

import (
	"strings"
	"testing"

	"github.com/pr-lens/backend/analysis"
)

func TestFormatReport(t *testing.T) {
	events := []analysis.StreamEvent{
		{Type: "risk", Data: map[string]any{"score": 72.0, "level": "high", "label": "Needs review"}},
		{Type: "summary", Data: map[string]any{"text": "Template fetch bypasses generated parsing."}},
		{Type: "category", Data: map[string]any{
			"id": "logic", "label": "Logic", "icon": "⚙", "riskLevel": "high",
			"summary": "GetTemplate can return a zero value.",
			"reviewQuestions": []any{
				map[string]any{"text": "Missing template key returns zero Template.", "file": "svc.go", "lineStart": 254.0},
			},
		}},
		{Type: "recommendation", Data: map[string]any{"action": "request_changes", "reason": "Handle a missing template key."}},
	}

	out := FormatReport("acme/api#9", events)
	for _, want := range []string{
		"acme/api#9",
		"72",
		"high",
		"Template fetch bypasses generated parsing.",
		"Logic",
		"svc.go:254",
		"Missing template key returns zero Template.",
		"request_changes",
		"Handle a missing template key.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q\n%s", want, out)
		}
	}
}

func TestFormatReport_Empty(t *testing.T) {
	out := FormatReport("x", nil)
	if !strings.Contains(out, "No analysis events") {
		t.Fatalf("got %q", out)
	}
}
