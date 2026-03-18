// Package github provides a minimal GitHub API client for fetching PR data.
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

	"github.com/just-pr/backend/analysis"
)

// ParsePRURL extracts owner, repo, and PR number from a GitHub PR URL.
func ParsePRURL(rawURL string) (*analysis.PRRef, error) {
	re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pull/(\d+)`)
	m := re.FindStringSubmatch(rawURL)
	if len(m) != 4 {
		return nil, fmt.Errorf("not a recognized GitHub PR URL: %s", rawURL)
	}
	n, _ := strconv.Atoi(m[3])
	return &analysis.PRRef{Owner: m[1], Repo: m[2], Number: n}, nil
}

// Client is a minimal GitHub API client for fetching PR diffs.
type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{token: token, httpClient: &http.Client{}}
}

func (c *Client) FetchDiff(ctx context.Context, ref *analysis.PRRef) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", ref.Owner, ref.Repo, ref.Number)
	req, err := c.newRequest(ctx, url, "application/vnd.github.v3.diff")
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("PR not found (404): %s/%s#%d — %s", ref.Owner, ref.Repo, ref.Number, strings.TrimSpace(string(body)))
	case http.StatusUnauthorized:
		return "", fmt.Errorf("invalid GitHub token")
	default:
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	diff, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading diff: %w", err)
	}
	return string(diff), nil
}

func (c *Client) FetchPRInfo(ctx context.Context, ref *analysis.PRRef) (*analysis.PRInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", ref.Owner, ref.Repo, ref.Number)
	req, err := c.newRequest(ctx, url, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var info analysis.PRInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding PR info: %w", err)
	}
	return &info, nil
}

func (c *Client) FetchPRFiles(ctx context.Context, ref *analysis.PRRef) ([]analysis.PRFile, error) {
	var all []analysis.PRFile
	for page := 1; ; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/files?per_page=100&page=%d",
			ref.Owner, ref.Repo, ref.Number, page)
		req, err := c.newRequest(ctx, url, "application/vnd.github+json")
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("github request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("github API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var batch []analysis.PRFile
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			return nil, fmt.Errorf("decoding PR files: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

func (c *Client) FetchFileContents(ctx context.Context, ref *analysis.PRRef, baseSHA string, filenames []string) []analysis.FileContent {
	const maxConcurrency = 10
	const maxFileSize = 50_000

	results := make([]analysis.FileContent, len(filenames))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for i, filename := range filenames {
		wg.Add(1)
		go func(idx int, fname string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			content, err := c.fetchSingleFile(ctx, ref, baseSHA, fname)
			if err == nil && len(content) <= maxFileSize {
				results[idx] = analysis.FileContent{Path: fname, Content: content}
			}
		}(i, filename)
	}
	wg.Wait()

	var out []analysis.FileContent
	for _, fc := range results {
		if fc.Path != "" {
			out = append(out, fc)
		}
	}
	return out
}

func (c *Client) fetchSingleFile(ctx context.Context, ref *analysis.PRRef, sha, path string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", ref.Owner, ref.Repo, path, sha)
	req, err := c.newRequest(ctx, url, "application/vnd.github.raw+json")
	if err != nil {
		return "", err
	}
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

// ReviewComment is a single inline comment for a pull request review.
type ReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// PostReview submits a pull request review via the GitHub REST API.
// event must be one of "APPROVE", "REQUEST_CHANGES", or "COMMENT".
func (c *Client) PostReview(ctx context.Context, ref *analysis.PRRef, event, body string, comments []ReviewComment) error {
	type ghComment struct {
		Path     string `json:"path"`
		Line     int    `json:"line"`
		Body     string `json:"body"`
		Side     string `json:"side"`
		Position int    `json:"position,omitempty"`
	}
	type payload struct {
		Body     string       `json:"body"`
		Event    string       `json:"event"`
		Comments []ghComment  `json:"comments,omitempty"`
	}

	ghComments := make([]ghComment, 0, len(comments))
	for _, c := range comments {
		if c.Path == "" || c.Body == "" {
			continue
		}
		ghComments = append(ghComments, ghComment{
			Path: c.Path,
			Line: c.Line,
			Body: c.Body,
			Side: "RIGHT",
		})
	}

	p := payload{Body: body, Event: event, Comments: ghComments}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshaling review payload: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/reviews", ref.Owner, ref.Repo, ref.Number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github API error %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, url, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}
