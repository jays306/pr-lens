package analysis

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"
)

// PipelineConfig holds configuration for the two-stage pipeline.
type PipelineConfig struct {
	// APIKey is used only for the Triage step (Haiku, non-streaming).
	APIKey string
	// Caller is the Analyzer used for summary, specialist, and recommendation calls.
	Caller Analyzer
	// FallbackAnalyzer handles small PRs and triage failures.
	FallbackAnalyzer Analyzer
}

// PipelineProvider implements Analyzer using two-stage parallel analysis.
// For small PRs it delegates to FallbackAnalyzer.
type PipelineProvider struct {
	cfg PipelineConfig
}

func NewPipelineProvider(cfg PipelineConfig) *PipelineProvider {
	return &PipelineProvider{cfg: cfg}
}

func (p *PipelineProvider) Name() string { return "pipeline(" + p.cfg.Caller.Name() + ")" }

// AnalyzePR satisfies Analyzer by delegating to the fallback.
// Callers with structured PR data should use AnalyzePRFull instead.
func (p *PipelineProvider) AnalyzePR(ctx context.Context, userPrompt string, emit func(StreamEvent) error) error {
	return p.cfg.FallbackAnalyzer.AnalyzePR(ctx, userPrompt, emit)
}

// AnalyzePRFull runs the full two-stage pipeline.
func (p *PipelineProvider) AnalyzePRFull(
	ctx context.Context,
	diff string,
	prFiles []PRFile,
	fileContents []FileContent,
	existingComments []ExistingComment,
	emit func(StreamEvent) error,
) error {
	log.Printf("[pipeline] start: %d files, %d diff lines", len(prFiles), strings.Count(diff, "\n"))

	if !aboveThreshold(prFiles, diff) {
		log.Printf("[pipeline] below threshold — using fallback analyzer")
		return p.cfg.FallbackAnalyzer.AnalyzePR(ctx, UserPrompt(diff, fileContents, existingComments), emit)
	}

	triageStart := time.Now()
	triage, err := Triage(ctx, p.cfg.APIKey, prFiles)
	if err != nil || len(triage) == 0 {
		log.Printf("[pipeline] triage failed (%v) — using fallback analyzer", err)
		return p.cfg.FallbackAnalyzer.AnalyzePR(ctx, UserPrompt(diff, fileContents, existingComments), emit)
	}
	log.Printf("[pipeline] triage done in %s: %d categories", time.Since(triageStart).Round(time.Millisecond), len(triage))
	for cat, files := range triage {
		log.Printf("[pipeline]   category %q: %v", cat, files)
	}

	type result struct {
		name   string
		events []StreamEvent
		err    error
	}

	numWorkers := len(triage) + 1 // specialists + summary
	ch := make(chan result, numWorkers)

	parallelStart := time.Now()
	go func() {
		t := time.Now()
		events, err := runSummaryCall(ctx, p.cfg.Caller, triage, first2000Lines(diff))
		log.Printf("[pipeline] summary call done in %s", time.Since(t).Round(time.Millisecond))
		ch <- result{"summary", events, err}
	}()
	for catID, files := range triage {
		go func(cat string, catFiles []string) {
			t := time.Now()
			events, err := RunSpecialist(ctx, p.cfg.Caller, cat, catFiles, diff, fileContents, existingComments)
			log.Printf("[pipeline] specialist %q done in %s", cat, time.Since(t).Round(time.Millisecond))
			ch <- result{cat, events, err}
		}(catID, files)
	}

	var categoryEvents []StreamEvent
	emitted := make(map[string]bool)
	for range numWorkers {
		r := <-ch
		if r.err != nil {
			log.Printf("[pipeline] worker %q error: %v", r.name, r.err)
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
				if id != "" && emitted[id] {
					continue
				}
				if id != "" {
					emitted[id] = true
				}
				categoryEvents = append(categoryEvents, ev)
			}
		}
	}
	log.Printf("[pipeline] parallel workers done in %s", time.Since(parallelStart).Round(time.Millisecond))

	// Sort categories by risk level before emitting so the frontend always
	// receives them highest-risk first, regardless of goroutine completion order.
	riskRank := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
	slices.SortStableFunc(categoryEvents, func(a, b StreamEvent) int {
		ra, _ := a.Data["riskLevel"].(string)
		rb, _ := b.Data["riskLevel"].(string)
		return riskRank[ra] - riskRank[rb]
	})
	for _, ev := range categoryEvents {
		if err := emit(ev); err != nil {
			return err
		}
	}

	recStart := time.Now()
	recEvents, err := runRecommendationCall(ctx, p.cfg.Caller, categoryEvents)
	if err == nil {
		log.Printf("[pipeline] recommendation call done in %s", time.Since(recStart).Round(time.Millisecond))
		for _, ev := range recEvents {
			if err := emit(ev); err != nil {
				return err
			}
		}
	} else {
		log.Printf("[pipeline] recommendation call error: %v", err)
	}

	return emit(StreamEvent{Type: "done", Data: map[string]any{}})
}

func aboveThreshold(files []PRFile, diff string) bool {
	if len(files) >= 2 {
		return true
	}
	return strings.Count(diff, "\n") >= 50
}

func first2000Lines(s string) string {
	lines := strings.SplitN(s, "\n", 2002)
	if len(lines) <= 2000 {
		return s
	}
	return strings.Join(lines[:2000], "\n") + "\n[truncated for summary]"
}

// callAndCollect issues a focused call with an explicit system prompt and collects matching events.
// Uses Caller if the analyzer supports it; falls back to prepending the system prompt to the user message.
func callAndCollect(ctx context.Context, a Analyzer, systemPrompt, userPrompt string, keep func(StreamEvent) bool) ([]StreamEvent, error) {
	var events []StreamEvent
	emit := func(ev StreamEvent) error {
		if keep(ev) {
			events = append(events, ev)
		}
		return nil
	}
	var err error
	if c, ok := a.(Caller); ok {
		err = c.Call(ctx, systemPrompt, userPrompt, emit)
	} else {
		err = a.AnalyzePR(ctx, systemPrompt+"\n\n---\n\n"+userPrompt, emit)
	}
	return events, err
}

func runSummaryCall(ctx context.Context, a Analyzer, triage TriageResult, condensedDiff string) ([]StreamEvent, error) {
	var b strings.Builder
	b.WriteString("# Triage classification\n\n")
	for cat, files := range triage {
		fmt.Fprintf(&b, "- **%s**: %s\n", cat, strings.Join(files, ", "))
	}
	b.WriteString("\n# Diff (first 2000 lines)\n\n```diff\n")
	b.WriteString(condensedDiff)
	b.WriteString("\n```\n")

	return callAndCollect(ctx, a, SummarySystemPrompt(), b.String(), func(ev StreamEvent) bool {
		return ev.Type == "risk" || ev.Type == "summary" || ev.Type == "systems"
	})
}

func runRecommendationCall(ctx context.Context, a Analyzer, categoryEvents []StreamEvent) ([]StreamEvent, error) {
	var b strings.Builder
	b.WriteString("Based on the following category analysis, provide a final recommendation.\n\n")
	for _, ev := range categoryEvents {
		if id, ok := ev.Data["id"].(string); ok {
			summary, _ := ev.Data["summary"].(string)
			riskLevel, _ := ev.Data["riskLevel"].(string)
			fmt.Fprintf(&b, "## %s (risk: %s)\n%s\n\n", id, riskLevel, summary)
		}
	}

	sysPrompt := `You are PR-LENS. Based on the category analysis provided, emit exactly one JSON line:
{"type":"recommendation","data":{"action":"approve|request_changes|needs_review","reason":"<markdown: 2-3 sentences>"}}
Then emit: {"type":"done","data":{}}`

	return callAndCollect(ctx, a, sysPrompt, b.String(), func(ev StreamEvent) bool {
		return ev.Type == "recommendation"
	})
}
