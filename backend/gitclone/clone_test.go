package gitclone

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pr-lens/backend/analysis"
)

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"plain text", []byte("hello world\n"), false},
		{"null byte", []byte("hello\x00world"), true},
		{"empty", []byte{}, false},
		{"go source", []byte("package main\n\nfunc main() {}\n"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBinary(tc.data); got != tc.want {
				t.Errorf("isBinary() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadAllFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a .git directory that should be skipped.
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(gitDir, "config"), "should be skipped")

	// Create text files.
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeFile(t, filepath.Join(dir, "README.md"), "# hello\n")

	// Create a binary file.
	writeFile(t, filepath.Join(dir, "image.png"), "PNG\x00binary")

	// Create a file over the per-file size limit — we fake the limit by using
	// a large content and a small maxSingleFileBytes via a sub-test helper.
	writeFile(t, filepath.Join(dir, "large.go"), strings.Repeat("x", maxSingleFileBytes+1))

	contents, err := ReadAllFiles(dir, 1_000_000)
	if err != nil {
		t.Fatalf("ReadAllFiles error: %v", err)
	}

	paths := make(map[string]bool, len(contents))
	for _, fc := range contents {
		paths[fc.Path] = true
	}

	if !paths["main.go"] {
		t.Error("expected main.go to be included")
	}
	if !paths["README.md"] {
		t.Error("expected README.md to be included")
	}
	if paths[".git/config"] {
		t.Error("expected .git/config to be excluded")
	}
	if paths["image.png"] {
		t.Error("expected image.png (binary) to be excluded")
	}
	if paths["large.go"] {
		t.Error("expected large.go (over size limit) to be excluded")
	}
}

func TestReadAllFiles_TotalBudget(t *testing.T) {
	dir := t.TempDir()

	// Two files of 10 bytes each; budget of 10 → only the first fits (budget
	// is checked before reading the next file: after reading a.txt total==10
	// which equals budget, so b.txt is skipped).
	writeFile(t, filepath.Join(dir, "a.txt"), "0123456789") // 10 bytes
	writeFile(t, filepath.Join(dir, "b.txt"), "0123456789") // 10 bytes

	contents, err := ReadAllFiles(dir, 10)
	if err != nil {
		t.Fatalf("ReadAllFiles error: %v", err)
	}
	if len(contents) != 1 {
		t.Errorf("expected 1 file within budget, got %d", len(contents))
	}
}

func TestReadAllFiles_ContentCorrect(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hello.go"), "package hello\n")

	contents, err := ReadAllFiles(dir, 1_000_000)
	if err != nil {
		t.Fatalf("ReadAllFiles error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 file, got %d", len(contents))
	}
	fc := contents[0]
	if fc.Path != "hello.go" {
		t.Errorf("path = %q, want hello.go", fc.Path)
	}
	if fc.Content != "package hello\n" {
		t.Errorf("content = %q, want 'package hello\\n'", fc.Content)
	}
}

func TestReadAllFiles_NestedPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "util"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "pkg", "util", "helper.go"), "package util\n")

	contents, err := ReadAllFiles(dir, 1_000_000)
	if err != nil {
		t.Fatalf("ReadAllFiles error: %v", err)
	}

	want := filepath.Join("pkg", "util", "helper.go")
	for _, fc := range contents {
		if fc.Path == want {
			return
		}
	}
	t.Errorf("expected path %q in results, got %v", want, fileContentPaths(contents))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func fileContentPaths(fcs []analysis.FileContent) []string {
	out := make([]string, len(fcs))
	for i, fc := range fcs {
		out[i] = fc.Path
	}
	return out
}
