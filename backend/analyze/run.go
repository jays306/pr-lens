package analyze

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/pr-lens/backend/analysis"
	"github.com/pr-lens/backend/cache"
	"github.com/pr-lens/backend/gitclone"
	"github.com/pr-lens/backend/github"
	"github.com/pr-lens/backend/harness"
)

// ErrInvalidURL means the argument is not a GitHub pull request URL.
var ErrInvalidURL = errors.New("invalid pull request URL")

// Job is a fetched PR ready to analyze.
type Job struct {
	Ref              *analysis.PRRef
	Diff             string
	PRFiles          []analysis.PRFile
	Contents         []analysis.FileContent
	ExistingComments []analysis.ExistingComment
	Info             *analysis.PRInfo
	CloneDir         string
	Token            string
	cleanup          func()
}

// Close removes any clone directory created during Load.
func (j *Job) Close() {
	if j != nil && j.cleanup != nil {
		j.cleanup()
		j.cleanup = nil
	}
}

// Load fetches the PR diff, files, and comments from GitHub and clones the head.
func Load(ctx context.Context, prURL, token string) (*Job, error) {
	ref, err := github.ParsePRURL(prURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	gh := github.NewClient(token)
	log.Printf("[analyze] start %s/%s #%d", ref.Owner, ref.Repo, ref.Number)
	reqStart := time.Now()

	type diffResult struct {
		diff string
		err  error
	}
	type filesResult struct {
		prFiles          []analysis.PRFile
		info             *analysis.PRInfo
		existingComments []analysis.ExistingComment
		err              error
	}

	diffCh := make(chan diffResult, 1)
	filesCh := make(chan filesResult, 1)

	go func() {
		t := time.Now()
		d, err := gh.FetchDiff(ctx, ref)
		log.Printf("[analyze] FetchDiff done in %s", time.Since(t).Round(time.Millisecond))
		diffCh <- diffResult{d, err}
	}()

	go func() {
		t := time.Now()
		info, err := gh.FetchPRInfo(ctx, ref)
		if err != nil {
			filesCh <- filesResult{err: fmt.Errorf("fetch PR info: %w", err)}
			return
		}
		prFiles, err := gh.FetchPRFiles(ctx, ref)
		if err != nil {
			log.Printf("[analyze] fetch PR files error (non-fatal): %v", err)
		}
		comments, err := gh.FetchReviewComments(ctx, ref)
		if err != nil {
			log.Printf("[analyze] FetchReviewComments error (non-fatal): %v", err)
		}
		log.Printf("[analyze] FetchPRInfo+Files+Comments done in %s (%d files, %d comments)",
			time.Since(t).Round(time.Millisecond), len(prFiles), len(comments))
		filesCh <- filesResult{prFiles: prFiles, info: info, existingComments: comments}
	}()

	dr := <-diffCh
	if dr.err != nil {
		return nil, fmt.Errorf("failed to fetch PR diff: %w", dr.err)
	}
	fr := <-filesCh
	if fr.err != nil {
		return nil, fr.err
	}
	if fr.info == nil {
		return nil, fmt.Errorf("missing PR info")
	}

	job := &Job{
		Ref:              ref,
		Diff:             dr.diff,
		PRFiles:          fr.prFiles,
		ExistingComments: fr.existingComments,
		Info:             fr.info,
		Token:            token,
	}

	t := time.Now()
	cloneResult, err := gitclone.ShallowClone(ctx, token, ref.Owner, ref.Repo, fr.info.Head.SHA, ref.Number)
	if err != nil {
		return nil, fmt.Errorf("clone PR head: %w", err)
	}
	job.CloneDir = cloneResult.Dir
	job.cleanup = cloneResult.Cleanup
	log.Printf("[analyze] shallow clone done in %s dir=%s", time.Since(t).Round(time.Millisecond), cloneResult.Dir)
	log.Printf("[analyze] github fetch phase done in %s", time.Since(reqStart).Round(time.Millisecond))
	return job, nil
}

// Stream runs analysis and emits events. store may be nil.
func (j *Job) Stream(ctx context.Context, analyzer analysis.Analyzer, store *cache.Store, emit func(analysis.StreamEvent) error) error {
	if j == nil {
		return errors.New("no analysis job")
	}

	if len(j.ExistingComments) > 0 {
		comments := make([]map[string]any, 0, len(j.ExistingComments))
		for _, c := range j.ExistingComments {
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

	reqStart := time.Now()
	if store != nil {
		cacheKey := cache.Key(j.Diff, commentsCacheMaterial(j.ExistingComments))
		if cached, ok := store.Get(cacheKey); ok {
			store.LogStats(cacheKey, true)
			if err := cache.Replay(cached, emit); err != nil {
				log.Printf("[analyze] cache replay error: %v", err)
			}
			_ = emit(analysis.StreamEvent{Type: "done", Data: map[string]any{}})
			log.Printf("[analyze] done (cache hit) total=%s", time.Since(reqStart).Round(time.Millisecond))
			return nil
		}

		release, waited := store.WaitForInflight(cacheKey)
		for waited {
			if cached, ok := store.Get(cacheKey); ok {
				store.LogStats(cacheKey, true)
				if err := cache.Replay(cached, emit); err != nil {
					log.Printf("[analyze] cache replay error: %v", err)
				}
				_ = emit(analysis.StreamEvent{Type: "done", Data: map[string]any{}})
				log.Printf("[analyze] done (cache hit after wait) total=%s", time.Since(reqStart).Round(time.Millisecond))
				return nil
			}
			release, waited = store.WaitForInflight(cacheKey)
		}
		defer release()
		store.LogStats(cacheKey, false)
	}

	diffIndex := analysis.ParseDiffIndex(j.Diff)
	collected, collectingEmit := cache.Collect(func(ev analysis.StreamEvent) error {
		if ev.Type == "category" {
			ev = analysis.EnrichCategory(ev, diffIndex)
		}
		return emit(ev)
	})

	prompt := analysis.UserPrompt(j.Diff, j.Contents, j.ExistingComments, j.Info)
	if j.Ref != nil {
		ctx = harness.WithAuth(ctx, j.Token, j.Ref.Owner)
	}

	aiStart := time.Now()
	var analyzeErr error
	if dirA, ok := analyzer.(analysis.DirAnalyzer); ok {
		if j.CloneDir == "" {
			analyzeErr = errors.New("harness review needs a cloned PR head")
		} else {
			analyzeErr = dirA.AnalyzePRInDir(ctx, j.CloneDir, prompt, collectingEmit)
		}
	} else if full, ok := analyzer.(analysis.FullAnalyzer); ok {
		analyzeErr = full.AnalyzePRFull(ctx, j.Diff, j.PRFiles, j.Contents, j.ExistingComments, j.Info, collectingEmit)
	} else {
		analyzeErr = analyzer.AnalyzePR(ctx, prompt, collectingEmit)
	}
	if analyzeErr != nil {
		log.Printf("[analyze] error: %v", analyzeErr)
		_ = emit(analysis.StreamEvent{Type: "error", Data: map[string]any{"message": analyzeErr.Error()}})
	} else if store != nil {
		store.Set(cache.Key(j.Diff, commentsCacheMaterial(j.ExistingComments)), *collected)
	}
	log.Printf("[analyze] done total=%s ai=%s", time.Since(reqStart).Round(time.Millisecond), time.Since(aiStart).Round(time.Millisecond))
	return analyzeErr
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
