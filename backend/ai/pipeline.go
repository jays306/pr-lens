package ai

import (
	"context"
	"fmt"
	"strings"

	ghclient "github.com/just-pr/backend/github"
)

// PipelineConfig holds configuration for the two-stage pipeline.
type PipelineConfig struct {
	APIKey           string   // Anthropic API key
	SonnetModel      string   // model for specialist + summary calls (defaults to claude-sonnet-4-6)
	FallbackProvider Provider // used when below threshold or triage fails
}

// PipelineProvider implements Provider using two-stage parallel analysis.
type PipelineProvider struct {
	cfg PipelineConfig
}

// NewPipelineProvider creates a PipelineProvider.
// fallback is used for small PRs and on triage failure.
func NewPipelineProvider(cfg PipelineConfig) *PipelineProvider {
	if cfg.SonnetModel == "" {
		cfg.SonnetModel = "claude-sonnet-4-6"
	}
	return &PipelineProvider{cfg: cfg}
}

func (p *PipelineProvider) Name() string {
	return "pipeline/" + p.cfg.SonnetModel
}

// AnalyzePR satisfies the Provider interface. Delegates to fallback.
// Use AnalyzePRFull for the full pipeline with structured PR data.
func (p *PipelineProvider) AnalyzePR(ctx context.Context, userPrompt string, emit func(StreamEvent) error) error {
	return p.cfg.FallbackProvider.AnalyzePR(ctx, userPrompt, emit)
}

// AnalyzePRFull runs the full two-stage pipeline with structured PR data.
func (p *PipelineProvider) AnalyzePRFull(
	ctx context.Context,
	diff string,
	prFiles []ghclient.PRFile,
	fileContents []ghclient.FileContent,
	emit func(StreamEvent) error,
) error {
	if !aboveThreshold(prFiles, diff) {
		return p.cfg.FallbackProvider.AnalyzePR(ctx, UserPrompt(diff, fileContents), emit)
	}

	triage, err := Triage(ctx, p.cfg.APIKey, prFiles)
	if err != nil || len(triage) == 0 {
		return p.cfg.FallbackProvider.AnalyzePR(ctx, UserPrompt(diff, fileContents), emit)
	}

	type workerResult struct {
		events []StreamEvent
		err    error
	}

	numWorkers := len(triage) + 1 // specialists + summary
	resultsCh := make(chan workerResult, numWorkers)

	// Summary call: risk + summary + systems
	go func() {
		events, err := runSummaryCall(ctx, p.cfg.APIKey, p.cfg.SonnetModel, triage, first2000Lines(diff))
		resultsCh <- workerResult{events, err}
	}()

	// Specialist calls: one per category
	for catID, files := range triage {
		go func(cat string, catFiles []string) {
			events, err := RunSpecialist(ctx, p.cfg.APIKey, p.cfg.SonnetModel, cat, catFiles, diff, fileContents)
			resultsCh <- workerResult{events, err}
		}(catID, files)
	}

	// Stream results as workers complete; deduplicate categories by ID.
	var categoryEvents []StreamEvent
	emittedCategories := make(map[string]bool)
	for range numWorkers {
		r := <-resultsCh
		if r.err != nil {
			continue
		}
		for _, ev := range r.events {
			switch ev.Type {
			case "risk", "summary", "systems":
				if err := emit(ev); err != nil {
					return err
				}
			case "category":
				id, _ := ev.Data["id"].(string)
				if id != "" && emittedCategories[id] {
					continue // drop duplicate category
				}
				if id != "" {
					emittedCategories[id] = true
				}
				categoryEvents = append(categoryEvents, ev)
				if err := emit(ev); err != nil {
					return err
				}
			}
		}
	}

	// Recommendation: synthesize from all category summaries
	recEvents, err := runRecommendationCall(ctx, p.cfg.APIKey, p.cfg.SonnetModel, categoryEvents)
	if err == nil {
		for _, ev := range recEvents {
			if err := emit(ev); err != nil {
				return err
			}
		}
	}

	return emit(StreamEvent{Type: "done", Data: map[string]any{}})
}

// aboveThreshold returns true if the PR warrants the pipeline path.
func aboveThreshold(files []ghclient.PRFile, diff string) bool {
	if len(files) >= 10 {
		return true
	}
	return strings.Count(diff, "\n") >= 500
}

// first2000Lines returns at most the first 2000 lines of s.
func first2000Lines(s string) string {
	lines := strings.SplitN(s, "\n", 2002)
	if len(lines) <= 2000 {
		return s
	}
	return strings.Join(lines[:2000], "\n") + "\n[truncated for summary]"
}

// runSummaryCall produces risk, summary, and systems events from a condensed diff.
func runSummaryCall(ctx context.Context, apiKey, model string, triage TriageResult, condensedDiff string) ([]StreamEvent, error) {
	var b strings.Builder
	b.WriteString("# Triage classification\n\n")
	for cat, files := range triage {
		fmt.Fprintf(&b, "- **%s**: %s\n", cat, strings.Join(files, ", "))
	}
	b.WriteString("\n# Diff (first 2000 lines)\n\n```diff\n")
	b.WriteString(condensedDiff)
	b.WriteString("\n```\n")

	return runStreamingCall(ctx, apiKey, model, SummarySystemPrompt(), b.String(), func(ev StreamEvent) bool {
		return ev.Type == "risk" || ev.Type == "summary" || ev.Type == "systems"
	})
}

// runRecommendationCall synthesizes a final recommendation from category summaries.
func runRecommendationCall(ctx context.Context, apiKey, model string, categoryEvents []StreamEvent) ([]StreamEvent, error) {
	var b strings.Builder
	b.WriteString("Based on the following category analysis, provide a final recommendation.\n\n")
	for _, ev := range categoryEvents {
		if id, ok := ev.Data["id"].(string); ok {
			summary, _ := ev.Data["summary"].(string)
			riskLevel, _ := ev.Data["riskLevel"].(string)
			fmt.Fprintf(&b, "## %s (risk: %s)\n%s\n\n", id, riskLevel, summary)
		}
	}

	sysPrompt := `You are JUST-PR. Based on the category analysis provided, emit exactly one JSON line:
{"type":"recommendation","data":{"action":"approve|request_changes|needs_review","reason":"<markdown: 2-3 sentences>"}}
Then emit: {"type":"done","data":{}}`

	return runStreamingCall(ctx, apiKey, model, sysPrompt, b.String(), func(ev StreamEvent) bool {
		return ev.Type == "recommendation"
	})
}
