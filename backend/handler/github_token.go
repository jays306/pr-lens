package handler

import (
	"fmt"
	"net/http"
	"strings"
)

// ResolveGitHubToken picks a token for this request, in order:
// 1) Authorization: Bearer <token>
// 2) X-GitHub-Token: <token>
// 3) serverDefault, only when allowServerDefault is true (typically GITHUB_TOKEN)
func ResolveGitHubToken(r *http.Request, serverDefault string, allowServerDefault bool) (string, error) {
	if h := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		t := strings.TrimSpace(h[7:])
		if t != "" {
			return t, nil
		}
	}
	if t := strings.TrimSpace(r.Header.Get("X-GitHub-Token")); t != "" {
		return t, nil
	}
	if allowServerDefault {
		if t := strings.TrimSpace(serverDefault); t != "" {
			return t, nil
		}
	}
	return "", fmt.Errorf("GitHub token required: configure the PR-LENS extension with a PAT")
}
