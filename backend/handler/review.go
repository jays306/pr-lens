package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/pr-lens/backend/github"
)

type reviewRequest struct {
	URL      string                 `json:"url"`
	Event    string                 `json:"event"`
	Body     string                 `json:"body"`
	Comments []github.ReviewComment `json:"comments"`
}

// Review returns an http.HandlerFunc that posts a pull request review to GitHub.
// The server GITHUB_TOKEN is never used here — a client-supplied PAT is required
// so a shared backend cannot approve PRs as the bot identity.
func Review() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		token, err := ResolveGitHubToken(r, "", false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		ghClient := github.NewClient(token)

		var req reviewRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || req.URL == "" {
			http.Error(w, `invalid request body: expected {"url":"...","event":"..."}`, http.StatusBadRequest)
			return
		}

		switch req.Event {
		case "APPROVE", "REQUEST_CHANGES", "COMMENT":
			// valid
		default:
			http.Error(w, `event must be one of APPROVE, REQUEST_CHANGES, COMMENT`, http.StatusBadRequest)
			return
		}

		ref, err := github.ParsePRURL(req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		log.Printf("[review] posting %s review to %s/%s #%d (%d comments)", req.Event, ref.Owner, ref.Repo, ref.Number, len(req.Comments))

		if err := ghClient.PostReview(r.Context(), ref, req.Event, req.Body, req.Comments); err != nil {
			log.Printf("[review] error: %v", err)
			http.Error(w, "failed to post review: "+err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}
