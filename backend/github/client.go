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

	"github.com/pr-lens/backend/analysis"
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

// TreeEntry represents a single entry in the repository's git tree.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
}

// FetchRepoTree returns the recursive file tree for a repository at the given SHA.
// Only blob (file) entries are returned; directories are omitted.
func (c *Client) FetchRepoTree(ctx context.Context, ref *analysis.PRRef, sha string) ([]TreeEntry, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", ref.Owner, ref.Repo, sha)
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

	var result struct {
		Tree      []TreeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding tree: %w", err)
	}
	if result.Truncated {
		// For very large repos the tree is truncated; we still return what we have.
		// Cross-reference resolution will simply miss files beyond the limit.
	}

	var files []TreeEntry
	for _, e := range result.Tree {
		if e.Type == "blob" {
			files = append(files, e)
		}
	}
	return files, nil
}

// FetchReviewComments returns existing PR discussion from inline comments,
// review bodies, and PR conversation comments.
func (c *Client) FetchReviewComments(ctx context.Context, ref *analysis.PRRef) ([]analysis.ExistingComment, error) {
	var all []analysis.ExistingComment
	inline, err := c.fetchInlineReviewComments(ctx, ref)
	if err != nil {
		return nil, err
	}
	all = append(all, inline...)

	reviews, err := c.fetchReviewBodies(ctx, ref)
	if err != nil {
		return nil, err
	}
	all = append(all, reviews...)

	issueComments, err := c.fetchIssueComments(ctx, ref)
	if err != nil {
		return nil, err
	}
	all = append(all, issueComments...)

	return all, nil
}

func (c *Client) fetchInlineReviewComments(ctx context.Context, ref *analysis.PRRef) ([]analysis.ExistingComment, error) {
	var all []analysis.ExistingComment
	for page := 1; ; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/comments?per_page=100&page=%d",
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
		var batch []struct {
			ID       int    `json:"id"`
			Path     string `json:"path"`
			Line     *int   `json:"line"`
			OrigLine *int   `json:"original_line"`
			Body     string `json:"body"`
			User     struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			return nil, fmt.Errorf("decoding review comments: %w", err)
		}
		for _, c := range batch {
			line := 0
			if c.Line != nil {
				line = *c.Line
			}
			originalLine := 0
			if c.OrigLine != nil {
				originalLine = *c.OrigLine
			}
			// line == nil means the comment's target line no longer exists in the
			// current diff (outdated). Still anchor as inline so it renders with
			// its original line context, just flagged outdated.
			outdated := line == 0 && originalLine > 0
			all = append(all, analysis.ExistingComment{
				ID:           c.ID,
				Path:         c.Path,
				Line:         line,
				OriginalLine: originalLine,
				Author:       c.User.Login,
				Body:         c.Body,
				Anchor:       "inline",
				Source:       "review_comment",
				Outdated:     outdated,
			})
		}
		if len(batch) < 100 {
			break
		}
	}

	// Fetch resolved thread IDs and mark comments accordingly. A failure here
	// is non-fatal — we just skip resolution tagging.
	resolvedIDs, err := c.fetchResolvedThreadIDs(ctx, ref)
	if err == nil && len(resolvedIDs) > 0 {
		for i := range all {
			if resolvedIDs[all[i].ID] {
				all[i].Resolved = true
			}
		}
	}

	return all, nil
}

func (c *Client) fetchReviewBodies(ctx context.Context, ref *analysis.PRRef) ([]analysis.ExistingComment, error) {
	var all []analysis.ExistingComment
	for page := 1; ; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/reviews?per_page=100&page=%d",
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
		var batch []struct {
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			return nil, fmt.Errorf("decoding reviews: %w", err)
		}
		for _, r := range batch {
			if strings.TrimSpace(r.Body) == "" {
				continue
			}
			all = append(all, analysis.ExistingComment{
				Author: r.User.Login,
				Body:   r.Body,
				Anchor: "pr",
				Source: "review",
			})
		}
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

func (c *Client) fetchIssueComments(ctx context.Context, ref *analysis.PRRef) ([]analysis.ExistingComment, error) {
	var all []analysis.ExistingComment
	for page := 1; ; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments?per_page=100&page=%d",
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
		var batch []struct {
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			return nil, fmt.Errorf("decoding issue comments: %w", err)
		}
		for _, comment := range batch {
			if strings.TrimSpace(comment.Body) == "" {
				continue
			}
			all = append(all, analysis.ExistingComment{
				Author: comment.User.Login,
				Body:   comment.Body,
				Anchor: "pr",
				Source: "issue_comment",
			})
		}
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
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
		Body     string      `json:"body"`
		Event    string      `json:"event"`
		Comments []ghComment `json:"comments,omitempty"`
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

// fetchResolvedThreadIDs calls the GitHub GraphQL API to determine which review
// thread comment IDs belong to resolved threads. It returns a map of comment
// database IDs → true for all comments in resolved threads.
func (c *Client) fetchResolvedThreadIDs(ctx context.Context, ref *analysis.PRRef) (map[int]bool, error) {
	const query = `query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewThreads(first: 100) {
        nodes {
          isResolved
          comments(first: 100) {
            nodes {
              databaseId
            }
          }
        }
      }
    }
  }
}`

	type variables struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Number int    `json:"number"`
	}
	type requestBody struct {
		Query     string    `json:"query"`
		Variables variables `json:"variables"`
	}

	body, err := json.Marshal(requestBody{
		Query:     query,
		Variables: variables{Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number},
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/graphql", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graphql request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graphql API error %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved bool `json:"isResolved"`
							Comments   struct {
								Nodes []struct {
									DatabaseID int `json:"databaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding graphql response: %w", err)
	}

	resolved := make(map[int]bool)
	for _, thread := range result.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if !thread.IsResolved {
			continue
		}
		for _, comment := range thread.Comments.Nodes {
			resolved[comment.DatabaseID] = true
		}
	}
	return resolved, nil
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
