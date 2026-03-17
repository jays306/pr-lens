package ai

import (
	"strings"
	"testing"

	ghclient "github.com/just-pr/backend/github"
)

func TestBuildTriagePrompt(t *testing.T) {
	files := []ghclient.PRFile{
		{Filename: "auth/jwt.go", Status: "modified"},
		{Filename: "handlers/user.go", Status: "added"},
		{Filename: "migrations/0042.sql", Status: "added"},
	}
	prompt := buildTriagePrompt(files)

	for _, f := range []string{"auth/jwt.go", "handlers/user.go", "migrations/0042.sql"} {
		if !strings.Contains(prompt, f) {
			t.Errorf("expected %q in triage prompt", f)
		}
	}
}

func TestParseTriageResponse(t *testing.T) {
	raw := `{"security":["auth/jwt.go"],"api":["handlers/user.go"],"migrations":["migrations/0042.sql"]}`
	result, err := parseTriageResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result["security"]) != 1 || result["security"][0] != "auth/jwt.go" {
		t.Errorf("unexpected security files: %v", result["security"])
	}
	if len(result["api"]) != 1 {
		t.Errorf("unexpected api files: %v", result["api"])
	}
}

func TestParseTriageResponse_Invalid(t *testing.T) {
	_, err := parseTriageResponse("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseTriageResponse_MarkdownFenced(t *testing.T) {
	raw := "```json\n{\"security\":[\"auth/jwt.go\"]}\n```"
	result, err := parseTriageResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result["security"]) != 1 {
		t.Errorf("expected security files, got %v", result)
	}
}
