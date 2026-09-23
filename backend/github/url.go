package github

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/pr-lens/backend/analysis"
)

var prPathRE = regexp.MustCompile(`^/([^/]+)/([^/]+)/pull/(\d+)`)

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

// ParsePRURL extracts owner, repo, and PR number from a GitHub PR URL.
func ParsePRURL(rawURL string) (*analysis.PRRef, error) {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return nil, fmt.Errorf("not a recognized GitHub PR URL: empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("not a recognized GitHub PR URL: %s", rawURL)
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return nil, fmt.Errorf("not a recognized GitHub PR URL: %s", rawURL)
	}
	m := prPathRE.FindStringSubmatch(u.Path)
	if len(m) != 4 {
		return nil, fmt.Errorf("not a recognized GitHub PR URL: %s", rawURL)
	}
	if !validGitHubName(m[1]) || !validGitHubName(m[2]) {
		return nil, fmt.Errorf("invalid repository in GitHub PR URL: %s", rawURL)
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("not a recognized GitHub PR URL: %s", rawURL)
	}
	return &analysis.PRRef{Owner: m[1], Repo: m[2], Number: n}, nil
}
