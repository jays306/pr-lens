package analysis

import (
	"strings"
	"testing"
)

func TestUserPrompt_IncludesPRIntent(t *testing.T) {
	pr := &PRInfo{Title: "fix: retry GetTemplate on not_found", Body: "Return 422 so Dropbox Sign retries delivery."}
	got := UserPrompt("diff --git a/x", nil, nil, pr)
	for _, want := range []string{
		"# Author intent",
		"fix: retry GetTemplate on not_found",
		"Return 422 so Dropbox Sign retries delivery.",
		"Treat this as the author's stated goal",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("UserPrompt missing %q\n%s", want, got)
		}
	}
}

func TestUserPrompt_OmitsIntentWhenEmpty(t *testing.T) {
	got := UserPrompt("diff", nil, nil, nil)
	if strings.Contains(got, "# Author intent") {
		t.Fatal("expected no author-intent section")
	}
}

func TestAccuracyRules_InAllPrompts(t *testing.T) {
	needles := []string{
		"Do not invent vendor",
		"Do not contradict yourself",
		"request_changes only",
		"Do not repeat a concern",
		"Do not ask a question the diff already answers",
		"do not ask whether it was intentional",
		"not a silent shrink",
	}
	if !strings.Contains(RecommendationSystemPrompt(), "test-updated behavior change") {
		t.Error("recommendation should ignore test-updated behavior changes")
	}
	for name, p := range map[string]string{
		"system":     SystemPrompt(),
		"specialist": SpecialistSystemPrompt("logic"),
		"summary":    SummarySystemPrompt(),
		"recommend":  RecommendationSystemPrompt(),
	} {
		for _, n := range needles {
			if !strings.Contains(p, n) {
				t.Errorf("%s prompt missing %q", name, n)
			}
		}
	}
	if !strings.Contains(SpecialistSystemPrompt("logic"), "1–2 short sentences") {
		t.Error("specialist should cap category summary length")
	}
	if strings.Contains(SystemPrompt(), "2-4 items") || strings.Contains(SpecialistSystemPrompt("logic"), "2-4 reviewQuestions") {
		t.Error("prompts must not force a minimum question count")
	}
	if !strings.Contains(SystemPrompt(), "go mod download") || !strings.Contains(SystemPrompt(), "read the installed source") {
		t.Error("system prompt must tell the harness to install and read dependencies")
	}
	if strings.Contains(SystemPrompt(), `"type":"dependency"`) || strings.Contains(SystemPrompt(), "type\":\"dependency") {
		t.Error("dependency request protocol must not appear in the prompt")
	}
}

func TestUserPrompt_SkipsResolvedAndBotComments(t *testing.T) {
	got := UserPrompt("diff", nil, []ExistingComment{
		{Author: "alice", Body: "Keep this open question about 422.", Resolved: false},
		{Author: "bob", Body: "Already done.", Resolved: true},
		{Author: "github-actions[bot]", Body: "Long bot recap of prior reviews.", Resolved: false},
	}, nil)
	if !strings.Contains(got, "Keep this open question about 422.") {
		t.Fatal("expected unresolved human comment")
	}
	if strings.Contains(got, "Already done.") || strings.Contains(got, "Long bot recap") {
		t.Fatalf("prompt should omit resolved and bot comments:\n%s", got)
	}
}
