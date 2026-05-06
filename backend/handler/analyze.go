package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/pr-lens/backend/analysis"
	"github.com/pr-lens/backend/cache"
	"github.com/pr-lens/backend/github"
	"github.com/pr-lens/backend/gitclone"
)

type analyzeRequest struct {
	URL string `json:"url"`
}

// Analyze returns an http.HandlerFunc that streams PR analysis via SSE.
func Analyze(githubTokenDefault string, analyzer analysis.Analyzer, store *cache.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		token, err := ResolveGitHubToken(r, githubTokenDefault)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		ghClient := github.NewClient(token)

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
			prFiles          []analysis.PRFile
			contents         []analysis.FileContent
			treePaths        []string
			info             *analysis.PRInfo
			existingComments []analysis.ExistingComment
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

			// Fetch the repo tree, diff-file contents, and existing review comments concurrently.
			type treeResult struct {
				paths []string
				err   error
			}
			type commentsResult struct {
				comments []analysis.ExistingComment
				err      error
			}
			treeCh := make(chan treeResult, 1)
			commentsCh := make(chan commentsResult, 1)
			go func() {
				entries, err := ghClient.FetchRepoTree(r.Context(), ref, info.Head.SHA)
				if err != nil {
					treeCh <- treeResult{nil, err}
					return
				}
				paths := make([]string, 0, len(entries))
				for _, e := range entries {
					paths = append(paths, e.Path)
				}
				treeCh <- treeResult{paths, nil}
			}()
			go func() {
				comments, err := ghClient.FetchReviewComments(r.Context(), ref)
				commentsCh <- commentsResult{comments, err}
			}()

			contents := ghClient.FetchFileContents(r.Context(), ref, info.Base.SHA, toFetch)

			tr := <-treeCh
			if tr.err != nil {
				log.Printf("[analyze] FetchRepoTree error (non-fatal): %v", tr.err)
			}
			cr := <-commentsCh
			if cr.err != nil {
				log.Printf("[analyze] FetchReviewComments error (non-fatal): %v", cr.err)
			}

			log.Printf("[analyze] FetchPRInfo+Files+Contents+Tree+Comments done in %s (%d files, %d contents, %d tree entries, %d comments)",
				time.Since(t).Round(time.Millisecond), len(prFiles), len(contents), len(tr.paths), len(cr.comments))
			filesCh <- filesResult{prFiles: prFiles, contents: contents, treePaths: tr.paths, info: info, existingComments: cr.comments}
		}()

		dr := <-diffCh
		if dr.err != nil {
			log.Printf("[analyze] fetch diff error: %v", dr.err)
			http.Error(w, "failed to fetch PR diff: "+dr.err.Error(), http.StatusBadGateway)
			return
		}
		fr := <-filesCh

		cloned := false
		if os.Getenv("CLONE_REPOS") == "true" && fr.info != nil {
			// Shallow-clone the PR head to give the AI the full codebase as context.
			t := time.Now()
			cloneResult, err := gitclone.ShallowClone(r.Context(), token, ref.Owner, ref.Repo, fr.info.Head.SHA)
			if err != nil {
				log.Printf("[analyze] shallow clone failed (non-fatal): %v", err)
			} else {
				defer cloneResult.Cleanup()
				contents, err := gitclone.ReadAllFiles(cloneResult.Dir, 400_000)
				if err != nil {
					log.Printf("[analyze] ReadAllFiles failed (non-fatal): %v", err)
				} else {
					fr.contents = contents
					cloned = true
					log.Printf("[analyze] shallow clone done in %s (%d files, total context)", time.Since(t).Round(time.Millisecond), len(contents))
				}
			}
		}

		if !cloned {
			// Resolve cross-references: find files imported by the diff that we
			// haven't fetched yet, then fetch those from the head SHA so the AI
			// sees the full context of referenced types, interfaces, and helpers.
			if len(fr.treePaths) > 0 && dr.diff != "" && fr.info != nil {
				alreadyFetched := make(map[string]bool, len(fr.contents))
				for _, fc := range fr.contents {
					alreadyFetched[fc.Path] = true
				}
				var allFilenames []string
				for _, f := range fr.prFiles {
					allFilenames = append(allFilenames, f.Filename)
				}
				xrefPaths := analysis.ResolveXRefs(dr.diff, allFilenames, fr.treePaths, alreadyFetched)
				if len(xrefPaths) > 0 {
					t := time.Now()
					log.Printf("[analyze] fetching %d cross-referenced files", len(xrefPaths))
					xrefContents := ghClient.FetchFileContents(r.Context(), ref, fr.info.Head.SHA, xrefPaths)
					fr.contents = append(fr.contents, xrefContents...)
					log.Printf("[analyze] xref fetch done in %s (%d files fetched)", time.Since(t).Round(time.Millisecond), len(xrefContents))
				}
			}
		}

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

		// Always emit existing comments before AI results (not cached).
		if len(fr.existingComments) > 0 {
			comments := make([]map[string]any, 0, len(fr.existingComments))
			for _, c := range fr.existingComments {
				comments = append(comments, map[string]any{
					"path":         c.Path,
					"line":         c.Line,
					"originalLine": c.OriginalLine,
					"author":       c.Author,
					"body":         c.Body,
					"anchor":       c.Anchor,
					"source":       c.Source,
					"resolved":     c.Resolved,
				})
			}
			_ = emit(analysis.StreamEvent{Type: "comments", Data: map[string]any{"comments": comments}})
		}

		cacheKey := cache.Key(dr.diff, commentsCacheMaterial(fr.existingComments))
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
			analyzeErr = full.AnalyzePRFull(r.Context(), dr.diff, fr.prFiles, fr.contents, fr.existingComments, collectingEmit)
		} else {
			analyzeErr = analyzer.AnalyzePR(r.Context(), analysis.UserPrompt(dr.diff, fr.contents, fr.existingComments), collectingEmit)
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

func commentsCacheMaterial(comments []analysis.ExistingComment) string {
	if len(comments) == 0 {
		return ""
	}
	data, err := json.Marshal(comments)
	if err != nil {
		return fmt.Sprint(comments)
	}
	return string(data)
}

// Health returns a simple health check handler.
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
