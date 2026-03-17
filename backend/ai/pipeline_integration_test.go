//go:build integration

package ai

import (
	"os"
	"strings"
	"testing"
)

func TestPipelineThresholdDetection(t *testing.T) {
	diff, err := os.ReadFile("../testdata/sample.diff")
	if err != nil {
		t.Skipf("no sample.diff fixture: %v", err)
	}

	diffStr := string(diff)
	lineCount := strings.Count(diffStr, "\n")
	t.Logf("fixture diff: %d lines", lineCount)

	// Verify FilterDiffByFiles correctly filters by filename
	result := FilterDiffByFiles(diffStr, []string{"auth/jwt.go", "migrations/0043_add_email_verified.sql"})
	if !strings.Contains(result, "auth/jwt.go") {
		t.Error("expected auth/jwt.go in filtered result")
	}
	if !strings.Contains(result, "migrations/0043_add_email_verified.sql") {
		t.Error("expected migration file in filtered result")
	}
	if strings.Contains(result, "README.md") {
		t.Error("expected README.md to be excluded from filtered result")
	}
}

func TestFixtureHasSufficientFiles(t *testing.T) {
	diff, err := os.ReadFile("../testdata/sample.diff")
	if err != nil {
		t.Skipf("no sample.diff fixture: %v", err)
	}

	diffStr := string(diff)
	fileCount := strings.Count(diffStr, "diff --git ")
	if fileCount < 10 {
		t.Errorf("fixture should have at least 10 files for threshold testing, got %d", fileCount)
	}
	t.Logf("fixture has %d changed files", fileCount)
}
