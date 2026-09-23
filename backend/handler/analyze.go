package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/pr-lens/backend/analysis"
	"github.com/pr-lens/backend/analyze"
	"github.com/pr-lens/backend/cache"
)

type analyzeRequest struct {
	URL string `json:"url"`
}

const maxRequestBytes = 1 << 20

// Analyze returns an http.HandlerFunc that streams PR analysis via SSE.
func Analyze(githubTokenDefault string, analyzer analysis.Analyzer, store *cache.Store, requireClientToken bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		token, err := ResolveGitHubToken(r, githubTokenDefault, !requireClientToken)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		var req analyzeRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
		if err := dec.Decode(&req); err != nil || req.URL == "" {
			http.Error(w, `invalid request body: expected {"url":"..."}`, http.StatusBadRequest)
			return
		}

		job, err := analyze.Load(r.Context(), req.URL, token)
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, analyze.ErrInvalidURL) {
				status = http.StatusBadRequest
			}
			log.Printf("[analyze] load error: %v", err)
			http.Error(w, err.Error(), status)
			return
		}
		defer job.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		flusher, canFlush := w.(http.Flusher)
		emit := func(ev analysis.StreamEvent) error {
			data, err := json.Marshal(ev)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return err
			}
			if canFlush {
				flusher.Flush()
			}
			return nil
		}

		reqStart := time.Now()
		if err := job.Stream(r.Context(), analyzer, store, emit); err != nil {
			log.Printf("[analyze] stream error: %v", err)
		}
		log.Printf("[analyze] request finished in %s", time.Since(reqStart).Round(time.Millisecond))
	}
}

// Health returns a simple health check handler.
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
