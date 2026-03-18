package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/just-pr/backend/analysis"
	"github.com/just-pr/backend/cache"
	"github.com/just-pr/backend/github"
)

type analyzeRequest struct {
	URL string `json:"url"`
}

// Analyze returns an http.HandlerFunc that streams PR analysis via SSE.
func Analyze(ghClient *github.Client, analyzer analysis.Analyzer, store *cache.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req analyzeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
			http.Error(w, `invalid request body: expected {"url":"..."}`, http.StatusBadRequest)
			return
		}

		ref, err := github.ParsePRURL(req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		log.Printf("[analyze] start %s/%s #%d", ref.Owner, ref.Repo, ref.Number)
		reqStart := time.Now()

		type diffResult struct {
			diff string
			err  error
		}
		type filesResult struct {
			prFiles  []analysis.PRFile
			contents []analysis.FileContent
		}

		diffCh := make(chan diffResult, 1)
		filesCh := make(chan filesResult, 1)

		go func() {
			t := time.Now()
			d, err := ghClient.FetchDiff(r.Context(), ref)
			log.Printf("[analyze] FetchDiff done in %s", time.Since(t).Round(time.Millisecond))
			diffCh <- diffResult{d, err}
		}()

		go func() {
			t := time.Now()
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
			var toFetch []string
			for _, f := range prFiles {
				if f.Status == "modified" || f.Status == "renamed" {
					toFetch = append(toFetch, f.Filename)
				}
			}
			contents := ghClient.FetchFileContents(r.Context(), ref, info.Base.SHA, toFetch)
			log.Printf("[analyze] FetchPRInfo+Files+Contents done in %s (%d files, %d contents)", time.Since(t).Round(time.Millisecond), len(prFiles), len(contents))
			filesCh <- filesResult{prFiles: prFiles, contents: contents}
		}()

		dr := <-diffCh
		if dr.err != nil {
			log.Printf("[analyze] fetch diff error: %v", dr.err)
			http.Error(w, "failed to fetch PR diff: "+dr.err.Error(), http.StatusBadGateway)
			return
		}
		fr := <-filesCh
		log.Printf("[analyze] github fetch phase done in %s", time.Since(reqStart).Round(time.Millisecond))

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

		cacheKey := cache.Key(dr.diff)
		if cached, ok := store.Get(cacheKey); ok {
			store.LogStats(cacheKey, true)
			if err := cache.Replay(cached, emit); err != nil {
				log.Printf("[analyze] cache replay error: %v", err)
			}
			_ = emit(analysis.StreamEvent{Type: "done", Data: map[string]any{}})
			log.Printf("[analyze] done (cache hit) total=%s", time.Since(reqStart).Round(time.Millisecond))
			return
		}
		store.LogStats(cacheKey, false)

		collected, collectingEmit := cache.Collect(emit)

		aiStart := time.Now()
		var analyzeErr error
		if full, ok := analyzer.(analysis.FullAnalyzer); ok {
			analyzeErr = full.AnalyzePRFull(r.Context(), dr.diff, fr.prFiles, fr.contents, collectingEmit)
		} else {
			analyzeErr = analyzer.AnalyzePR(r.Context(), analysis.UserPrompt(dr.diff, fr.contents), collectingEmit)
		}
		if analyzeErr != nil {
			log.Printf("[analyze] error: %v", analyzeErr)
			_ = emit(analysis.StreamEvent{Type: "error", Data: map[string]any{"message": analyzeErr.Error()}})
		} else {
			store.Set(cacheKey, *collected)
		}
		log.Printf("[analyze] done total=%s ai=%s", time.Since(reqStart).Round(time.Millisecond), time.Since(aiStart).Round(time.Millisecond))
	}
}

// Health returns a simple health check handler.
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
