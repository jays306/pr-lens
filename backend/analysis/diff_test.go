package analysis

import (
	"strings"
	"testing"
)

func TestFilterDiffByFiles_Empty(t *testing.T) {
	if FilterDiffByFiles("", []string{"foo.go"}) != "" {
		t.Error("expected empty string for empty diff")
	}
	if FilterDiffByFiles("some diff", nil) != "" {
		t.Error("expected empty string for empty filenames")
	}
}

func TestFilterDiffByFiles_SingleFile(t *testing.T) {
	diff := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+new\n"
	result := FilterDiffByFiles(diff, []string{"foo.go"})
	if !strings.Contains(result, "foo.go") {
		t.Error("expected foo.go in filtered result")
	}
}

func TestFilterDiffByFiles_ExcludesOtherFiles(t *testing.T) {
	diff := "diff --git a/foo.go b/foo.go\ncontent1\ndiff --git a/bar.go b/bar.go\ncontent2\n"
	result := FilterDiffByFiles(diff, []string{"foo.go"})
	if strings.Contains(result, "bar.go") {
		t.Error("expected bar.go to be excluded")
	}
	if !strings.Contains(result, "foo.go") {
		t.Error("expected foo.go in result")
	}
}
