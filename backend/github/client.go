package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// PRRef holds parsed pull request coordinates.
type PRRef struct {
	Owner  string
	Repo   string
	Number int
}

// ParsePRURL extracts owner, repo, and PR number from a GitHub PR URL.
func ParsePRURL(rawURL string) (*PRRef, error) {
	// https://github.com/owner/repo/pull/123
	re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pull/(\d+)`)
	m := re.FindStringSubmatch(rawURL)
	if len(m) != 4 {
		return nil, fmt.Errorf("not a recognized GitHub PR URL: %s", rawURL)
	}
	n, _ := strconv.Atoi(m[3])
	return &PRRef{Owner: m[1], Repo: m[2], Number: n}, nil
}

// Client is a minimal GitHub API client for fetching PR diffs.
type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token:      token,
		httpClient: &http.Client{},
	}
}

// FetchDiff retrieves the unified diff for a pull request.
func (c *Client) FetchDiff(ctx context.Context, ref *PRRef) (string, error) {
	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/pulls/%d",
		ref.Owner, ref.Repo, ref.Number,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3.diff")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("PR not found (404): %s/%s#%d — %s", ref.Owner, ref.Repo, ref.Number, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("invalid GitHub token")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	diff, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading diff: %w", err)
	}

	return string(diff), nil
}

// PRFile represents a changed file in a pull request.
type PRFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"` // added, removed, modified, renamed
	Patch    string `json:"patch"`
}

// PRInfo holds metadata about the pull request.
type PRInfo struct {
	Base struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
}

// FetchPRInfo retrieves the PR metadata (base/head refs).
func (c *Client) FetchPRInfo(ctx context.Context, ref *PRRef) (*PRInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", ref.Owner, ref.Repo, ref.Number)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var info PRInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding PR info: %w", err)
	}
	return &info, nil
}

// FetchPRFiles returns the list of changed files in a PR.
func (c *Client) FetchPRFiles(ctx context.Context, ref *PRRef) ([]PRFile, error) {
	var allFiles []PRFile
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/files?per_page=100&page=%d",
			ref.Owner, ref.Repo, ref.Number, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("github request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("github API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var files []PRFile
		if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
			return nil, fmt.Errorf("decoding PR files: %w", err)
		}
		allFiles = append(allFiles, files...)
		if len(files) < 100 {
			break
		}
		page++
	}
	return allFiles, nil
}

// FileContent holds the full content of a file at a given ref.
type FileContent struct {
	Path    string
	Content string
}

// FetchFileContents fetches the full content of multiple files from the base branch (for context).
// It fetches in parallel with a concurrency limit.
func (c *Client) FetchFileContents(ctx context.Context, ref *PRRef, baseSHA string, filenames []string) []FileContent {
	const maxConcurrency = 10
	const maxFileSize = 50_000 // skip files larger than 50KB

	results := make([]FileContent, len(filenames))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for i, filename := range filenames {
		wg.Add(1)
		go func(idx int, fname string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			content, err := c.fetchSingleFile(ctx, ref, baseSHA, fname)
			if err != nil || len(content) > maxFileSize {
				return // skip on error or too large
			}
			results[idx] = FileContent{Path: fname, Content: content}
		}(i, filename)
	}
	wg.Wait()

	// Filter out empty entries
	var out []FileContent
	for _, fc := range results {
		if fc.Path != "" {
			out = append(out, fc)
		}
	}
	return out
}

func (c *Client) fetchSingleFile(ctx context.Context, ref *PRRef, sha string, path string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		ref.Owner, ref.Repo, path, sha)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
