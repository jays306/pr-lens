package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/just-pr/backend/ai"
	ghclient "github.com/just-pr/backend/github"
)

type analyzeRequest struct {
	URL string `json:"url"`
}

// AnalyzeHandler returns an http.HandlerFunc that streams PR analysis via SSE.
func AnalyzeHandler(ghClient *ghclient.Client, provider ai.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req analyzeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
			http.Error(w, "invalid request body: expected {\"url\":\"...\"}", http.StatusBadRequest)
			return
		}

		// Parse PR URL
		ref, err := ghclient.ParsePRURL(req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Fetch diff and file context in parallel
		type diffResult struct {
			diff string
			err  error
		}
		type filesResult struct {
			contents []ghclient.FileContent
			prFiles  []ghclient.PRFile
		}

		diffCh := make(chan diffResult, 1)
		filesCh := make(chan filesResult, 1)

		go func() {
			d, err := ghClient.FetchDiff(r.Context(), ref)
			diffCh <- diffResult{d, err}
		}()

		go func() {
			// Fetch PR info to get base SHA, then fetch file contents
			info, err := ghClient.FetchPRInfo(r.Context(), ref)
			if err != nil {
				log.Printf("[analyze] fetch PR info error (non-fatal): %v", err)
				filesCh <- filesResult{}
				return
			}

			prFiles, err := ghClient.FetchPRFiles(r.Context(), ref)
			if err != nil {
				log.Printf("[analyze] fetch PR files error (non-fatal): %v", err)
				filesCh <- filesResult{}
				return
			}

			// Only fetch base content for modified files (not added/removed)
			var toFetch []string
			for _, f := range prFiles {
				if f.Status == "modified" || f.Status == "renamed" {
					toFetch = append(toFetch, f.Filename)
				}
			}

			contents := ghClient.FetchFileContents(r.Context(), ref, info.Base.SHA, toFetch)
			filesCh <- filesResult{prFiles: prFiles, contents: contents}
		}()

		dr := <-diffCh
		if dr.err != nil {
			log.Printf("[analyze] fetch diff error: %v", dr.err)
			http.Error(w, "failed to fetch PR diff: "+dr.err.Error(), http.StatusBadGateway)
			return
		}
		diff := dr.diff
		fr := <-filesCh

		// Set up SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		flusher, canFlush := w.(http.Flusher)

		emit := func(event ai.StreamEvent) error {
			data, err := json.Marshal(event)
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

		// Stream analysis — use FullAnalyzer interface if available (PipelineProvider)
		var analyzeErr error
		if full, ok := provider.(ai.FullAnalyzer); ok {
			analyzeErr = full.AnalyzePRFull(r.Context(), diff, fr.prFiles, fr.contents, emit)
		} else {
			userPrompt := ai.UserPrompt(diff, fr.contents)
			analyzeErr = provider.AnalyzePR(r.Context(), userPrompt, emit)
		}
		if analyzeErr != nil {
			log.Printf("[analyze] provider error: %v", analyzeErr)
			_ = emit(ai.StreamEvent{
				Type: "error",
				Data: map[string]any{"message": analyzeErr.Error()},
			})
		}
	}
}

// HealthHandler returns a simple health check.
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
