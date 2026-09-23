package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pr-lens/backend/analysis"
	"github.com/pr-lens/backend/harness"
)

// LoadDotEnv loads backend/.env from the current directory and common parents.
func LoadDotEnv() {
	candidates := []string{".env", "backend/.env"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, ".env"),
			filepath.Join(dir, "..", ".env"),
			filepath.Join(dir, "..", "backend", ".env"),
		)
	}
	for _, p := range candidates {
		_ = godotenv.Load(p)
	}
}

// GitHubToken returns the server PAT from the environment.
func GitHubToken() string {
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

// AnalyzerFromEnv builds the Claude Code or Codex harness reviewer.
func AnalyzerFromEnv() (analysis.Analyzer, error) {
	cfg, err := harness.FromEnv()
	if err != nil {
		return nil, err
	}
	return harness.New(cfg), nil
}
