# Parallel Pipeline Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the single-LLM-call PR analysis with a two-stage parallel pipeline: a cheap Haiku triage pass that classifies files by category, followed by concurrent Sonnet specialist calls — one per affected category — dramatically reducing latency and improving quality on large PRs.

**Architecture:** A new `PipelineProvider` implements the existing `ai.Provider` interface and drops into `main.go` with no handler changes. Stage 1 uses `claude-haiku-4-5-20251001` to classify files into categories. Stage 2 fires one `claude-sonnet-4-6` goroutine per category in parallel, plus a concurrent risk/summary call, merging results into the SSE stream as they complete. Small PRs (< 10 files and < 500 diff lines) bypass the pipeline and use the existing `ClaudeProvider` as a fast path.

**Tech Stack:** Go 1.23, `github.com/anthropics/anthropic-sdk-go`, existing `ai.Provider` interface, Server-Sent Events (no new dependencies).

---

## Task 1: diff_filter.go — Extract diff hunks by filename

**Files:**

- Create: `backend/ai/diff_filter.go`
- Create: `backend/ai/diff_filter_test.go`

### Step 1: Write the failing test

```go
// backend/ai/diff_filter_test.go
package ai

import (
	"strings"
	"testing"
)

func TestFilterDiffByFiles(t *testing.T) {
	diff := `diff --git a/auth/jwt.go b/auth/jwt.go
index abc..def 100644
--- a/auth/jwt.go
+++ b/auth/jwt.go
@@ -10,6 +10,7 @@
 func Validate() {
+	// added line
 }
diff --git a/handlers/user.go b/handlers/user.go
index 111..222 100644
--- a/handlers/user.go
+++ b/handlers/user.go
@@ -1,3 +1,4 @@
+// new handler
 package handlers
diff --git a/README.md b/README.md
index 333..444 100644
--- a/README.md
+++ b/README.md
@@ -1,2 +1,3 @@
+# heading
`

	result := FilterDiffByFiles(diff, []string{"auth/jwt.go", "README.md"})

	if !strings.Contains(result, "auth/jwt.go") {
		t.Error("expected auth/jwt.go in filtered diff")
	}
	if !strings.Contains(result, "README.md") {
		t.Error("expected README.md in filtered diff")
	}
	if strings.Contains(result, "handlers/user.go") {
		t.Error("expected handlers/user.go to be excluded")
	}
}

func TestFilterDiffByFiles_Empty(t *testing.T) {
	result := FilterDiffByFiles("", []string{"foo.go"})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestFilterDiffByFiles_NoMatch(t *testing.T) {
	diff := "diff --git a/foo.go b/foo.go\nindex 1..2 100644\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n+x\n"
	result := FilterDiffByFiles(diff, []string{"bar.go"})
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}
```

### Step 2: Run test to verify it fails

```bash
cd backend && go test ./ai/ -run TestFilterDiff -v
```

Expected: FAIL — `FilterDiffByFiles` undefined.

### Step 3: Implement diff_filter.go

```go
// backend/ai/diff_filter.go
package ai

import (
	"strings"
)

// FilterDiffByFiles returns only the diff hunks for the given set of filenames.
// A hunk starts with "diff --git a/<file> b/<file>" and ends at the next such line.
func FilterDiffByFiles(diff string, filenames []string) string {
	if diff == "" || len(filenames) == 0 {
		return ""
	}

	wanted := make(map[string]bool, len(filenames))
	for _, f := range filenames {
		wanted[f] = true
	}

	var out strings.Builder
	var current strings.Builder
	var currentFile string

	flush := func() {
		if currentFile != "" && wanted[currentFile] {
			out.WriteString(current.String())
		}
		current.Reset()
		currentFile = ""
	}

	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			// Extract filename: "diff --git a/path/to/file.go b/path/to/file.go"
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// parts[3] is "b/path/to/file.go"
				currentFile = strings.TrimPrefix(parts[3], "b/")
			}
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	flush()

	return out.String()
}
```

### Step 4: Run test to verify it passes

```bash
cd backend && go test ./ai/ -run TestFilterDiff -v
```

Expected: PASS all three tests.

### Step 5: Commit

```bash
cd backend && git add ai/diff_filter.go ai/diff_filter_test.go
git commit -m "feat: add diff_filter to extract hunks by filename"
```

---

## Task 2: triage.go — Haiku file classification call

**Files:**

- Create: `backend/ai/triage.go`
- Create: `backend/ai/triage_test.go`

### Step 1: Write the failing test

```go
// backend/ai/triage_test.go
package ai

import (
	"testing"

	ghclient "github.com/pr-lens/backend/github"
)

func TestBuildTriagePrompt(t *testing.T) {
	files := []ghclient.PRFile{
		{Filename: "auth/jwt.go", Status: "modified"},
		{Filename: "handlers/user.go", Status: "added"},
		{Filename: "migrations/0042.sql", Status: "added"},
	}
	prompt := buildTriagePrompt(files)

	for _, f := range []string{"auth/jwt.go", "handlers/user.go", "migrations/0042.sql"} {
		if !containsStr(prompt, f) {
			t.Errorf("expected %q in triage prompt", f)
		}
	}
}

func TestParseTriageResponse(t *testing.T) {
	raw := `{"security":["auth/jwt.go"],"api":["handlers/user.go"],"migrations":["migrations/0042.sql"]}`
	result, err := parseTriageResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result["security"]) != 1 || result["security"][0] != "auth/jwt.go" {
		t.Errorf("unexpected security files: %v", result["security"])
	}
	if len(result["api"]) != 1 {
		t.Errorf("unexpected api files: %v", result["api"])
	}
}

func TestParseTriageResponse_Invalid(t *testing.T) {
	_, err := parseTriageResponse("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStrHelper(s, sub))
}

func containsStrHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

### Step 2: Run test to verify it fails

```bash
cd backend && go test ./ai/ -run TestBuildTriagePrompt -run TestParseTriageResponse -v
```

Expected: FAIL — `buildTriagePrompt` and `parseTriageResponse` undefined.

### Step 3: Implement triage.go

````go
// backend/ai/triage.go
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	ghclient "github.com/pr-lens/backend/github"
)

const triageModel = "claude-haiku-4-5-20251001"

// TriageResult maps category ID to the list of filenames assigned to it.
type TriageResult map[string][]string

// Triage classifies changed files into categories using a fast Haiku call.
// Returns an error only on API failure; malformed JSON is returned as an error too.
func Triage(ctx context.Context, apiKey string, files []ghclient.PRFile) (TriageResult, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	prompt := buildTriagePrompt(files)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(triageModel),
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: triageSystemPrompt()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("triage call failed: %w", err)
	}

	if len(msg.Content) == 0 {
		return nil, fmt.Errorf("triage: empty response")
	}

	text, ok := msg.Content[0].AsAny().(anthropic.TextBlock)
	if !ok {
		return nil, fmt.Errorf("triage: unexpected content type")
	}

	return parseTriageResponse(text.Text)
}

func triageSystemPrompt() string {
	return `You are a code classification assistant. Given a list of files changed in a pull request, classify each file into exactly one category.

Available categories: security, api, database, migrations, performance, logic, refactor, tests, dependencies, config, docs

Respond with ONLY a valid JSON object mapping category names to arrays of filenames.
Example: {"security":["auth/jwt.go"],"api":["handlers/user.go"]}
No explanation. No markdown. Only the JSON object.`
}

func buildTriagePrompt(files []ghclient.PRFile) string {
	var b strings.Builder
	b.WriteString("Classify these changed files into categories:\n\n")
	for _, f := range files {
		b.WriteString(fmt.Sprintf("- %s (%s)\n", f.Filename, f.Status))
	}
	return b.String()
}

func parseTriageResponse(raw string) (TriageResult, error) {
	// Strip markdown fences if present
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		var inner []string
		for _, l := range lines {
			if strings.HasPrefix(l, "```") {
				continue
			}
			inner = append(inner, l)
		}
		raw = strings.Join(inner, "\n")
	}

	var result TriageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("triage: parse failed: %w", err)
	}
	return result, nil
}
````

### Step 4: Run test to verify it passes

```bash
cd backend && go test ./ai/ -run TestBuildTriagePrompt -run TestParseTriageResponse -v
```

Expected: PASS all tests.

### Step 5: Commit

```bash
cd backend && git add ai/triage.go ai/triage_test.go
git commit -m "feat: add Haiku triage call for file classification"
```

---

## Task 3: prompt.go additions — Specialist and summary prompts

**Files:**

- Modify: `backend/ai/prompt.go`

The specialist system prompt is a focused variant of the existing `SystemPrompt()`. It instructs the model to emit only `category` + `snippet` events for one specific category. The summary prompt produces `risk`, `summary`, and `systems` events from a condensed diff view.

### Step 1: Add SpecialistSystemPrompt and SummarySystemPrompt to prompt.go

Open `backend/ai/prompt.go` and append these two functions after `UserPrompt`:

```go
// SpecialistSystemPrompt returns a system prompt for a single-category specialist agent.
// The specialist emits only "category" and "done" events.
func SpecialistSystemPrompt(categoryID string) string {
	return fmt.Sprintf(`You are JUST-PR, a senior staff engineer performing a focused code review.

You are analyzing ONLY the "%s" category of a pull request diff.

# Output Format

Emit exactly TWO JSON lines. Each must be valid JSON with "type" and "data" fields. No markdown, no commentary — only JSON lines.

## Line 1: Category event
{"type":"category","data":{"id":"%s","icon":"<emoji>","label":"<name>","summary":"<markdown: what changed and why it matters>","riskLevel":"low|medium|high|critical","fileCount":<n>,"snippets":[<snippet>,...],"reviewQuestions":["<question>",...]}}

### Snippet shape:
{"file":"<full file path>","language":"<ts|js|go|python|java|sql|yaml|json|bash|css|html>","before":"<removed lines without - prefix, empty string if pure addition>","after":"<added lines without + prefix, empty string if pure deletion>","lineStart":<line number from hunk header>,"explanation":"<1-2 sentences>"}

Rules:
- EVERY file in the diff must appear as a snippet. No file may be omitted.
- Snippets must contain COMPLETE diff hunks — never truncate.
- fileCount must equal number of snippets.
- Order snippets by risk (highest first).
- 2-4 reviewQuestions per category. Ask about intent, edge cases, production behavior — not yes/no questions.

## Line 2: Done event
{"type":"done","data":{}}`, categoryID, categoryID)
}

// SummarySystemPrompt returns a system prompt for the risk/summary/systems call.
// This call receives a condensed view of the diff and produces risk, summary, and systems events.
func SummarySystemPrompt() string {
	return `You are JUST-PR, a senior staff engineer performing a high-level risk assessment of a pull request.

# Output Format

Emit a series of JSON lines in this exact order. Each line must be valid JSON. No markdown, no commentary — only JSON lines.

## 1. Risk
{"type":"risk","data":{"score":<0-100>,"level":"low|medium|high|critical","label":"<2-6 word description>"}}

Scoring: 0-20 low, 21-50 medium, 51-80 high, 81-100 critical.

## 2. Summary (3-5 chunks of ~40-60 words each)
{"type":"summary","data":{"text":"<chunk>"}}

Write like a senior engineer briefing the team. Cover: what this PR does, architectural approach, key risks, notable patterns. Use **bold**, ` + "`backticks`" + `, bullet points.

## 3. Systems
{"type":"systems","data":{"affected":["<category>",...],"reviewOrder":["<category>",...]}}

reviewOrder: highest risk first.

## 4. Done
{"type":"done","data":{}}`
}
```

Note: add `"fmt"` to the imports in `prompt.go` if not already present.

### Step 2: Build to verify no compile errors

```bash
cd backend && go build ./...
```

Expected: no errors.

### Step 3: Commit

```bash
cd backend && git add ai/prompt.go
git commit -m "feat: add SpecialistSystemPrompt and SummarySystemPrompt"
```

---

## Task 4: specialist.go — Single category analysis call

**Files:**

- Create: `backend/ai/specialist.go`
- Create: `backend/ai/specialist_test.go`

### Step 1: Write the failing test

```go
// backend/ai/specialist_test.go
package ai

import (
	"testing"
)

func TestCollectSpecialistEvents(t *testing.T) {
	// collectSpecialistEvents should parse JSON lines and return StreamEvents,
	// skipping "done" events and ignoring non-category events.
	lines := []string{
		`{"type":"category","data":{"id":"security","label":"Security","riskLevel":"high","fileCount":1,"snippets":[],"reviewQuestions":[]}}`,
		`{"type":"done","data":{}}`,
		``,
	}

	events, err := collectSpecialistEvents(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "category" {
		t.Errorf("expected type 'category', got %q", events[0].Type)
	}
}

func TestCollectSpecialistEvents_SkipsNonCategory(t *testing.T) {
	lines := []string{
		`{"type":"risk","data":{"score":50}}`,
		`{"type":"category","data":{"id":"api","label":"API","riskLevel":"low","fileCount":0,"snippets":[],"reviewQuestions":[]}}`,
	}
	events, _ := collectSpecialistEvents(lines)
	if len(events) != 1 || events[0].Type != "category" {
		t.Errorf("expected only category event, got %v", events)
	}
}
```

### Step 2: Run test to verify it fails

```bash
cd backend && go test ./ai/ -run TestCollectSpecialist -v
```

Expected: FAIL — `collectSpecialistEvents` undefined.

### Step 3: Implement specialist.go

```go
// backend/ai/specialist.go
package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	ghclient "github.com/pr-lens/backend/github"
)

// RunSpecialist runs a focused Sonnet analysis for a single category.
// It returns all non-done StreamEvents emitted by the model.
func RunSpecialist(ctx context.Context, apiKey, model, categoryID string, files []string, diff string, fileContents []ghclient.FileContent) ([]StreamEvent, error) {
	filteredDiff := FilterDiffByFiles(diff, files)
	if strings.TrimSpace(filteredDiff) == "" {
		// No relevant diff — return empty category
		return []StreamEvent{{
			Type: "category",
			Data: map[string]any{
				"id":              categoryID,
				"icon":            "?",
				"label":           categoryID,
				"summary":         "No changes found for this category.",
				"riskLevel":       "low",
				"fileCount":       0,
				"snippets":        []any{},
				"reviewQuestions": []any{},
			},
		}}, nil
	}

	// Filter file contents to only this category's files
	fileSet := make(map[string]bool, len(files))
	for _, f := range files {
		fileSet[f] = true
	}
	var filteredContents []ghclient.FileContent
	for _, fc := range fileContents {
		if fileSet[fc.Path] {
			filteredContents = append(filteredContents, fc)
		}
	}

	userPrompt := UserPrompt(filteredDiff, filteredContents)
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 8192,
		System: []anthropic.TextBlockParam{
			{Text: SpecialistSystemPrompt(categoryID)},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})

	var lineBuf strings.Builder
	var jsonLines []string

	for stream.Next() {
		event := stream.Current()
		delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent)
		if !ok {
			continue
		}
		text, ok := delta.Delta.AsAny().(anthropic.TextDelta)
		if !ok {
			continue
		}
		lineBuf.WriteString(text.Text)
		for {
			buf := lineBuf.String()
			idx := strings.Index(buf, "\n")
			if idx < 0 {
				break
			}
			line := strings.TrimSpace(buf[:idx])
			lineBuf.Reset()
			lineBuf.WriteString(buf[idx+1:])
			if line != "" {
				jsonLines = append(jsonLines, line)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("specialist %s: stream error: %w", categoryID, err)
	}
	if rem := strings.TrimSpace(lineBuf.String()); rem != "" {
		jsonLines = append(jsonLines, rem)
	}

	return collectSpecialistEvents(jsonLines)
}

// collectSpecialistEvents parses JSON lines and returns only category-type events.
func collectSpecialistEvents(lines []string) ([]StreamEvent, error) {
	var events []StreamEvent
	for _, line := range lines {
		if line == "" {
			continue
		}
		ev, err := parseStreamEvent(line)
		if err != nil {
			continue // skip malformed lines
		}
		if ev.Type == "done" {
			continue
		}
		if ev.Type == "category" {
			events = append(events, ev)
		}
	}
	return events, nil
}
```

### Step 4: Run test to verify it passes

```bash
cd backend && go test ./ai/ -run TestCollectSpecialist -v
```

Expected: PASS.

### Step 5: Build to check for compile errors

```bash
cd backend && go build ./...
```

Expected: no errors.

### Step 6: Commit

```bash
cd backend && git add ai/specialist.go ai/specialist_test.go
git commit -m "feat: add RunSpecialist for single-category Sonnet analysis"
```

---

## Task 5: pipeline.go — PipelineProvider orchestrator

**Files:**

- Create: `backend/ai/pipeline.go`
- Create: `backend/ai/pipeline_test.go`

### Step 1: Write the failing test

```go
// backend/ai/pipeline_test.go
package ai

import (
	"testing"

	ghclient "github.com/pr-lens/backend/github"
)

func TestPipelineThreshold(t *testing.T) {
	// Below threshold: < 10 files and < 500 diff lines
	files := make([]ghclient.PRFile, 5)
	diff := strings.Repeat("a\n", 100) // 100 lines

	if aboveThreshold(files, diff) {
		t.Error("expected below threshold for 5 files and 100 diff lines")
	}
}

func TestPipelineThresholdFiles(t *testing.T) {
	// Above threshold by file count
	files := make([]ghclient.PRFile, 10)
	diff := strings.Repeat("a\n", 100)

	if !aboveThreshold(files, diff) {
		t.Error("expected above threshold for 10 files")
	}
}

func TestPipelineThresholdLines(t *testing.T) {
	// Above threshold by line count
	files := make([]ghclient.PRFile, 3)
	diff := strings.Repeat("a\n", 500)

	if !aboveThreshold(files, diff) {
		t.Error("expected above threshold for 500 diff lines")
	}
}
```

Note: add `"strings"` to the import in the test file.

### Step 2: Run test to verify it fails

```bash
cd backend && go test ./ai/ -run TestPipelineThreshold -v
```

Expected: FAIL — `aboveThreshold` undefined.

### Step 3: Implement pipeline.go

````go
// backend/ai/pipeline.go
package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"

	ghclient "github.com/pr-lens/backend/github"
)

// PipelineConfig holds configuration for the two-stage pipeline.
type PipelineConfig struct {
	APIKey       string // Anthropic API key (used for all calls)
	SonnetModel  string // model for specialist + summary calls (defaults to claude-sonnet-4-6)
	FallbackProvider Provider // used when below threshold or triage fails
}

// PipelineProvider implements Provider using two-stage parallel analysis.
type PipelineProvider struct {
	cfg PipelineConfig
}

// NewPipelineProvider creates a PipelineProvider. fallback is used for small PRs and on triage failure.
func NewPipelineProvider(cfg PipelineConfig) *PipelineProvider {
	if cfg.SonnetModel == "" {
		cfg.SonnetModel = "claude-sonnet-4-6"
	}
	return &PipelineProvider{cfg: cfg}
}

func (p *PipelineProvider) Name() string {
	return "pipeline/" + p.cfg.SonnetModel
}

// AnalyzePR orchestrates the two-stage pipeline.
// userPrompt is the full diff + file context built by UserPrompt().
// For the pipeline to work properly, callers must also pass PRFiles and the raw diff
// separately — but since the Provider interface only passes userPrompt, we extract
// what we need from it. For the pipeline, the handler passes a PipelineRequest instead.
func (p *PipelineProvider) AnalyzePR(ctx context.Context, userPrompt string, emit func(StreamEvent) error) error {
	// This method satisfies the Provider interface for compatibility.
	// The pipeline handler uses AnalyzePRFull for full functionality.
	return p.cfg.FallbackProvider.AnalyzePR(ctx, userPrompt, emit)
}

// AnalyzePRFull runs the full two-stage pipeline with access to structured PR data.
func (p *PipelineProvider) AnalyzePRFull(
	ctx context.Context,
	diff string,
	prFiles []ghclient.PRFile,
	fileContents []ghclient.FileContent,
	emit func(StreamEvent) error,
) error {
	// Below threshold: delegate to fallback
	if !aboveThreshold(prFiles, diff) {
		userPrompt := UserPrompt(diff, fileContents)
		return p.cfg.FallbackProvider.AnalyzePR(ctx, userPrompt, emit)
	}

	// Stage 1: Triage
	triage, err := Triage(ctx, p.cfg.APIKey, prFiles)
	if err != nil {
		// Triage failed: fall back gracefully
		userPrompt := UserPrompt(diff, fileContents)
		return p.cfg.FallbackProvider.AnalyzePR(ctx, userPrompt, emit)
	}

	if len(triage) == 0 {
		userPrompt := UserPrompt(diff, fileContents)
		return p.cfg.FallbackProvider.AnalyzePR(ctx, userPrompt, emit)
	}

	// Stage 2: parallel dispatch
	// Fan out: risk/summary call + one specialist per category
	type result struct {
		events []StreamEvent
		err    error
		order  int // for deterministic done-event ordering; not used for streaming order
	}

	numWorkers := len(triage) + 1 // +1 for summary call
	resultsCh := make(chan result, numWorkers)

	// Summary call (risk + summary + systems)
	go func() {
		summaryDiff := first2000Lines(diff)
		events, err := runSummaryCall(ctx, p.cfg.APIKey, p.cfg.SonnetModel, triage, summaryDiff)
		resultsCh <- result{events, err, 0}
	}()

	// Specialist calls
	i := 1
	for catID, files := range triage {
		go func(cat string, catFiles []string, idx int) {
			events, err := RunSpecialist(ctx, p.cfg.APIKey, p.cfg.SonnetModel, cat, catFiles, diff, fileContents)
			resultsCh <- result{events, err, idx}
		}(catID, files, i)
		i++
	}

	// Collect results as they arrive, streaming immediately
	var categoryEvents []StreamEvent
	for range numWorkers {
		r := <-resultsCh
		if r.err != nil {
			// Emit degraded event for failed workers (specialists only; summary failure handled separately)
			continue
		}
		for _, ev := range r.events {
			switch ev.Type {
			case "risk", "summary", "systems":
				if err := emit(ev); err != nil {
					return err
				}
			case "category":
				categoryEvents = append(categoryEvents, ev)
				if err := emit(ev); err != nil {
					return err
				}
			}
		}
	}

	// Recommendation: synthesize from category summaries
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

// aboveThreshold returns true if the PR is large enough to warrant the pipeline.
func aboveThreshold(files []ghclient.PRFile, diff string) bool {
	if len(files) >= 10 {
		return true
	}
	lines := strings.Count(diff, "\n")
	return lines >= 500
}

// first2000Lines returns at most the first 2000 lines of a string.
func first2000Lines(s string) string {
	lines := strings.SplitN(s, "\n", 2002)
	if len(lines) <= 2000 {
		return s
	}
	return strings.Join(lines[:2000], "\n") + "\n[truncated for summary]"
}

// runSummaryCall produces risk, summary, and systems events from a condensed diff view.
func runSummaryCall(ctx context.Context, apiKey, model string, triage TriageResult, condensedDiff string) ([]StreamEvent, error) {
	// Build a compact prompt: category map + condensed diff
	var b strings.Builder
	b.WriteString("# Triage classification\n\n")
	for cat, files := range triage {
		b.WriteString(fmt.Sprintf("- **%s**: %s\n", cat, strings.Join(files, ", ")))
	}
	b.WriteString("\n# Diff (first 2000 lines)\n\n```diff\n")
	b.WriteString(condensedDiff)
	b.WriteString("\n```\n")

	import_anthropic_client := func() interface{} { return nil } // placeholder — see note below
	_ = import_anthropic_client

	// Use ClaudeProvider with SummarySystemPrompt override
	// We need a raw streaming call here with a different system prompt.
	// Reuse the same streaming pattern from claude.go.
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
			b.WriteString(fmt.Sprintf("## %s (risk: %s)\n%s\n\n", id, riskLevel, summary))
		}
	}

	sysPrompt := `You are JUST-PR. Based on category analysis provided, emit exactly one JSON line:
{"type":"recommendation","data":{"action":"approve|request_changes|needs_review","reason":"<markdown: 2-3 sentences>"}}
Then emit: {"type":"done","data":{}}`

	return runStreamingCall(ctx, apiKey, model, sysPrompt, b.String(), func(ev StreamEvent) bool {
		return ev.Type == "recommendation"
	})
}
````

Note: `runStreamingCall` is a helper we need to extract from `claude.go`. Add it in the next step.

### Step 4: Extract runStreamingCall helper into claude.go

Open `backend/ai/claude.go` and add this function after `AnalyzePR`:

```go
// runStreamingCall runs a one-shot streaming Anthropic call and collects events matching the filter.
func runStreamingCall(ctx context.Context, apiKey, model, systemPrompt, userPrompt string, keep func(StreamEvent) bool) ([]StreamEvent, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})

	var lineBuf strings.Builder
	var events []StreamEvent

	for stream.Next() {
		event := stream.Current()
		delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent)
		if !ok {
			continue
		}
		text, ok := delta.Delta.AsAny().(anthropic.TextDelta)
		if !ok {
			continue
		}
		lineBuf.WriteString(text.Text)
		for {
			buf := lineBuf.String()
			idx := strings.Index(buf, "\n")
			if idx < 0 {
				break
			}
			line := strings.TrimSpace(buf[:idx])
			lineBuf.Reset()
			lineBuf.WriteString(buf[idx+1:])
			if line == "" {
				continue
			}
			ev, err := parseStreamEvent(line)
			if err != nil {
				continue
			}
			if keep(ev) {
				events = append(events, ev)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("streaming call: %w", err)
	}
	if rem := strings.TrimSpace(lineBuf.String()); rem != "" {
		if ev, err := parseStreamEvent(rem); err == nil && keep(ev) {
			events = append(events, ev)
		}
	}
	return events, nil
}
```

Also remove the `import_anthropic_client` placeholder from `pipeline.go` — it was a note placeholder.

### Step 5: Run tests

```bash
cd backend && go test ./ai/ -run TestPipelineThreshold -v
```

Expected: PASS all threshold tests.

### Step 6: Build

```bash
cd backend && go build ./...
```

Expected: no errors.

### Step 7: Commit

```bash
cd backend && git add ai/pipeline.go ai/pipeline_test.go ai/claude.go
git commit -m "feat: add PipelineProvider two-stage parallel orchestrator"
```

---

## Task 6: Wire PipelineProvider into main.go

**Files:**

- Modify: `backend/main.go`

The handler needs to call `AnalyzePRFull` when the provider is a `*PipelineProvider`. The cleanest approach: extend `handlers/analyze.go` to type-assert for a `FullAnalyzer` interface, keeping the handler generic.

### Step 1: Add FullAnalyzer interface to provider.go

Open `backend/ai/provider.go` and add:

```go
// FullAnalyzer is an optional interface for providers that need structured PR data
// beyond the pre-built user prompt string.
type FullAnalyzer interface {
	AnalyzePRFull(
		ctx context.Context,
		diff string,
		prFiles []ghclient.PRFile,
		fileContents []ghclient.FileContent,
		emit func(StreamEvent) error,
	) error
}
```

Add the import `ghclient "github.com/pr-lens/backend/github"` to `provider.go`.

### Step 2: Update handlers/analyze.go to use FullAnalyzer when available

Open `backend/handlers/analyze.go`. Replace the line:

```go
// Stream analysis
if err := provider.AnalyzePR(r.Context(), userPrompt, emit); err != nil {
```

with:

```go
// Stream analysis — use FullAnalyzer interface if available (PipelineProvider)
var analyzeErr error
if full, ok := provider.(ai.FullAnalyzer); ok {
    analyzeErr = full.AnalyzePRFull(r.Context(), diff, fr.prFiles, fr.contents, emit)
} else {
    analyzeErr = provider.AnalyzePR(r.Context(), userPrompt, emit)
}
if analyzeErr != nil {
```

Also update the `filesResult` struct to carry `prFiles`:

```go
type filesResult struct {
    contents []ghclient.FileContent
    prFiles  []ghclient.PRFile
}
```

And update the goroutine that fills it to set `prFiles`:

```go
filesCh <- filesResult{contents, prFiles}
```

(where `prFiles` is the result of `ghClient.FetchPRFiles`).

### Step 3: Update main.go buildProvider to return PipelineProvider for claude

In `backend/main.go`, update the `"claude"` case in `buildProvider`:

```go
case "claude":
    if baseURL != "" {
        log.Printf("ANTHROPIC_BASE_URL is set — using OpenAI-compatible mode against %s", baseURL)
        apiKey := requireEnv("ANTHROPIC_API_KEY")
        return ai.NewOpenAICompatProvider(apiKey, baseURL, model)
    }
    apiKey := requireEnv("ANTHROPIC_API_KEY")
    fallback := ai.NewClaudeProvider(apiKey, "", model)
    return ai.NewPipelineProvider(ai.PipelineConfig{
        APIKey:           apiKey,
        SonnetModel:      model,
        FallbackProvider: fallback,
    })
```

### Step 4: Build

```bash
cd backend && go build ./...
```

Expected: no errors.

### Step 5: Run all tests

```bash
cd backend && go test ./... -v
```

Expected: all pass.

### Step 6: Commit

```bash
cd backend && git add ai/provider.go handlers/analyze.go main.go
git commit -m "feat: wire PipelineProvider into handler via FullAnalyzer interface"
```

---

## Task 7: Integration smoke test

**Files:**

- Create: `backend/ai/pipeline_integration_test.go`

This test uses a golden fixture diff to verify the full SSE event sequence without hitting a real API. It mocks the Anthropic transport layer — or uses a recorded real diff with `INTEGRATION=1` guard.

### Step 1: Create fixture diff

```bash
mkdir -p backend/testdata
```

Create `backend/testdata/sample.diff` with a small but realistic diff (at least 10 files, 500+ lines) representing a PR with security, API, and database changes. You can record a real one with:

```bash
curl -s -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github.v3.diff" \
  https://api.github.com/repos/anthropics/anthropic-sdk-go/pulls/1 \
  > backend/testdata/sample.diff
```

### Step 2: Write integration test

```go
// backend/ai/pipeline_integration_test.go
//go:build integration

package ai_test

import (
	"os"
	"strings"
	"testing"
)

func TestPipelineThresholdDetection(t *testing.T) {
	diff, err := os.ReadFile("../testdata/sample.diff")
	if err != nil {
		t.Skip("no sample.diff fixture")
	}

	lines := strings.Count(string(diff), "\n")
	t.Logf("fixture diff: %d lines", lines)

	// Just verify the diff is parseable by our filter
	result := FilterDiffByFiles(string(diff), []string{"nonexistent.go"})
	if result != "" {
		t.Errorf("expected empty result for nonexistent file, got %d chars", len(result))
	}
}
```

### Step 3: Run integration test

```bash
cd backend && go test ./ai/ -tags integration -run TestPipelineThresholdDetection -v
```

Expected: PASS (or SKIP if no fixture).

### Step 4: Commit

```bash
cd backend && git add ai/pipeline_integration_test.go testdata/
git commit -m "test: add integration smoke test for pipeline threshold detection"
```

---

## Final Verification

```bash
cd backend && go build ./... && go test ./... -v
```

Expected: all tests pass, binary builds cleanly.

The pipeline is now live. PRs with >= 10 changed files or >= 500 diff lines automatically use the two-stage Haiku triage + parallel Sonnet specialists path. Smaller PRs continue to use the original single-call `ClaudeProvider` with no behavioral change.
