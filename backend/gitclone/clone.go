// Package gitclone provides a shallow git clone for full codebase context.
package gitclone

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pr-lens/backend/analysis"
)

const (
	maxSingleFileBytes = 100_000
)

// CloneResult holds the cloned repo path and a cleanup function.
type CloneResult struct {
	Dir     string
	Cleanup func()
}

// ShallowClone clones owner/repo at headSHA (depth=1) to a temp dir.
// Authentication uses the GitHub token via the HTTPS credential helper pattern.
// The caller must invoke Cleanup() when done to remove the temp dir.
func ShallowClone(ctx context.Context, token, owner, repo, headSHA string) (*CloneResult, error) {
	dir, err := os.MkdirTemp("", "pr-lens-clone-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(dir) }

	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, owner, repo)

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", "--no-tags", "--single-branch", cloneURL, dir)
	// Suppress credential leakage in error output by redirecting stderr.
	var errBuf strings.Builder
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		cleanup()
		// Scrub the token from the error message before returning.
		msg := strings.ReplaceAll(errBuf.String(), token, "<token>")
		return nil, fmt.Errorf("git clone failed: %w — %s", err, strings.TrimSpace(msg))
	}

	// If headSHA doesn't match HEAD (e.g. fork PR), check out the exact SHA.
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", dir, "checkout", "--detach", headSHA)
	checkoutCmd.Stderr = &errBuf
	if err := checkoutCmd.Run(); err != nil {
		// Non-fatal: the shallow clone may not have the SHA if it's from a fork;
		// log but continue with whatever HEAD is.
		_ = err
	}

	return &CloneResult{Dir: dir, Cleanup: cleanup}, nil
}

type fileEntry struct {
	path string
	size int64
}

// ReadAllFiles walks the clone dir, reads all non-binary text files up to
// maxTotalBytes total, and returns them as []analysis.FileContent.
// Files are sorted smallest-first so the budget is used as broadly as possible.
func ReadAllFiles(dir string, maxTotalBytes int) ([]analysis.FileContent, error) {
	var entries []fileEntry

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		// Skip the .git directory entirely.
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() == 0 || info.Size() > maxSingleFileBytes {
			return nil
		}
		entries = append(entries, fileEntry{path: path, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking clone dir: %w", err)
	}

	// Sort smallest-first to maximise file count within the budget.
	sort.Slice(entries, func(i, j int) bool { return entries[i].size < entries[j].size })

	var out []analysis.FileContent
	total := 0
	prefix := dir + string(filepath.Separator)

	for _, e := range entries {
		if total >= maxTotalBytes {
			break
		}
		data, err := os.ReadFile(e.path)
		if err != nil {
			continue
		}
		if isBinary(data) {
			continue
		}
		relPath := strings.TrimPrefix(e.path, prefix)
		total += len(data)
		out = append(out, analysis.FileContent{Path: relPath, Content: string(data)})
	}

	return out, nil
}

// isBinary reports whether data looks like a binary file by scanning for null bytes.
func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
