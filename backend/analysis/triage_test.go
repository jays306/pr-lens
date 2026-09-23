package analysis

import (
	"strings"
	"testing"
)

func TestBuildTriagePrompt(t *testing.T) {
	files := []PRFile{
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
	if !strings.Contains(prompt, "modified") {
		t.Error("expected status 'modified' in triage prompt")
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
	if len(result["api"]) != 1 || result["api"][0] != "handlers/user.go" {
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

func TestBuildTriagePrompt_Empty(t *testing.T) {
	prompt := buildTriagePrompt(nil)
	if !strings.Contains(prompt, "Classify") {
		t.Error("expected prompt header even for empty file list")
	}
}

func TestHeuristicTriage(t *testing.T) {
	files := []PRFile{
		{Filename: "go.mod"},
		{Filename: "auth/jwt.go"},
		{Filename: "handler/analyze.go"},
		{Filename: "foo_test.go"},
		{Filename: "README.md"},
		{Filename: "pkg/worker.go"},
	}
	got := HeuristicTriage(files)
	if got["dependencies"][0] != "go.mod" {
		t.Errorf("go.mod: %v", got["dependencies"])
	}
	if got["security"][0] != "auth/jwt.go" {
		t.Errorf("auth: %v", got["security"])
	}
	if got["api"][0] != "handler/analyze.go" {
		t.Errorf("handler: %v", got["api"])
	}
	if got["tests"][0] != "foo_test.go" {
		t.Errorf("test: %v", got["tests"])
	}
	if got["docs"][0] != "README.md" {
		t.Errorf("docs: %v", got["docs"])
	}
	if got["logic"][0] != "pkg/worker.go" {
		t.Errorf("logic: %v", got["logic"])
	}
}

func TestTriage_EmptyAPIKeyUsesHeuristic(t *testing.T) {
	got, err := Triage(t.Context(), "", []PRFile{{Filename: "go.mod"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got["dependencies"]) != 1 {
		t.Fatalf("expected heuristic dependencies, got %v", got)
	}
}

func TestParseTriageResponse_UnknownCategory(t *testing.T) {
	raw := `{"security":["auth/jwt.go"],"hallucinated_category":["foo.go"]}`
	result, err := parseTriageResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["hallucinated_category"]; ok {
		t.Error("expected unknown category to be filtered out")
	}
	if len(result["security"]) != 1 {
		t.Error("expected security category to be preserved")
	}
}
