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
	"unicode"

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

func validGitHubName(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func writeAskpass() (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "pr-lens-askpass-*.sh")
	if err != nil {
		return "", nil, err
	}
	script := "#!/bin/sh\ncase \"$1\" in\n*Username*|*username*) echo x-access-token ;;\n*) echo \"$GIT_ASKPASS_PASSWORD\" ;;\nesac\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	if err := f.Chmod(0o700); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", nil, err
	}
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

func gitEnv(askpass, token string) []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS="+askpass,
		"GIT_ASKPASS_PASSWORD="+token,
		"GCM_INTERACTIVE=never",
	)
}

// AuthEnv returns extra environment entries so a child process can fetch
// private GitHub modules. cleanup removes the askpass helper. token is never
// placed in argv — only GIT_ASKPASS_PASSWORD.
func AuthEnv(token, owner string) (extra []string, cleanup func(), err error) {
	cleanup = func() {}
	if owner != "" && validGitHubName(owner) {
		prefix := "github.com/" + owner
		extra = append(extra, "GOPRIVATE="+prefix+","+prefix+"/*", "GONOSUMDB="+prefix+","+prefix+"/*")
	}
	if token == "" {
		return extra, cleanup, nil
	}
	askpass, askpassCleanup, err := writeAskpass()
	if err != nil {
		return nil, nil, err
	}
	extra = append(extra,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS="+askpass,
		"GIT_ASKPASS_PASSWORD="+token,
		"GCM_INTERACTIVE=never",
	)
	return extra, askpassCleanup, nil
}

// ShallowClone clones owner/repo and checks out headSHA (depth=1) to a temp dir.
// Authentication uses GIT_ASKPASS so the token is not written into the remote URL.
func ShallowClone(ctx context.Context, token, owner, repo, headSHA string, prNumber int) (*CloneResult, error) {
	if !validGitHubName(owner) || !validGitHubName(repo) {
		return nil, fmt.Errorf("invalid owner or repo")
	}
	if headSHA == "" || strings.ContainsAny(headSHA, " \n\t") {
		return nil, fmt.Errorf("invalid head SHA")
	}

	dir, err := os.MkdirTemp("", "pr-lens-clone-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(dir) }

	askpass, askpassCleanup, err := writeAskpass()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("creating askpass helper: %w", err)
	}
	defer askpassCleanup()

	env := gitEnv(askpass, token)
	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)

	var errBuf strings.Builder
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", "--no-tags", "--single-branch", cloneURL, dir)
	cmd.Env = env
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		cleanup()
		msg := strings.ReplaceAll(errBuf.String(), token, "<token>")
		return nil, fmt.Errorf("git clone failed: %w — %s", err, strings.TrimSpace(msg))
	}

	errBuf.Reset()
	fetchSHA := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--depth=1", "origin", headSHA)
	fetchSHA.Env = env
	fetchSHA.Stderr = &errBuf
	if err := fetchSHA.Run(); err != nil && prNumber > 0 {
		errBuf.Reset()
		fetchPR := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--depth=1", "origin", fmt.Sprintf("pull/%d/head", prNumber))
		fetchPR.Env = env
		fetchPR.Stderr = &errBuf
		if err := fetchPR.Run(); err != nil {
			cleanup()
			msg := strings.ReplaceAll(errBuf.String(), token, "<token>")
			return nil, fmt.Errorf("git fetch of PR head failed: %s", strings.TrimSpace(msg))
		}
		checkout := exec.CommandContext(ctx, "git", "-C", dir, "checkout", "--detach", "FETCH_HEAD")
		checkout.Env = env
		if err := checkout.Run(); err != nil {
			cleanup()
			return nil, fmt.Errorf("git checkout of PR head failed: %w", err)
		}
		return &CloneResult{Dir: dir, Cleanup: cleanup}, nil
	}

	checkout := exec.CommandContext(ctx, "git", "-C", dir, "checkout", "--detach", headSHA)
	checkout.Env = env
	checkout.Stderr = &errBuf
	if err := checkout.Run(); err != nil {
		cleanup()
		msg := strings.ReplaceAll(errBuf.String(), token, "<token>")
		return nil, fmt.Errorf("git checkout of %s failed: %s", headSHA, strings.TrimSpace(msg))
	}

	return &CloneResult{Dir: dir, Cleanup: cleanup}, nil
}

type fileEntry struct {
	path string
	size int64
}

// ReadAllFiles walks the clone dir, reads all non-binary text files up to
// maxTotalBytes total, and returns them as []analysis.FileContent.
func ReadAllFiles(dir string, maxTotalBytes int) ([]analysis.FileContent, error) {
	var entries []fileEntry

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
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
		out = append(out, analysis.FileContent{Path: relPath, Content: string(data), Ref: "head"})
	}

	return out, nil
}

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
