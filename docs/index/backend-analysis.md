# analysis.go
DOES: defines core interfaces (Analyzer, Caller, FullAnalyzer) and StreamEvent type; ParseStreamEvent parses JSON line
TYPE: StreamEvent { Type string, Data map[string]any }
INTERFACE: Analyzer { AnalyzePR(ctx, userPrompt, emit) error; Name() string }
INTERFACE: Caller { Call(ctx, systemPrompt, userPrompt, emit) error }
INTERFACE: FullAnalyzer { AnalyzePRFull(ctx, diff, prFiles, fileContents, existingComments, emit) error }
SYMBOLS: ParseStreamEvent(line string) → (StreamEvent, error)

# pr.go
DOES: plain data types for PR coordinates and GitHub API responses
TYPE: PRRef { Owner, Repo string, Number int }
TYPE: PRFile { Filename, Status, Patch string }
TYPE: PRInfo { Base{SHA,Ref}, Head{SHA,Ref} }
TYPE: FileContent { Path, Content string }
TYPE: ExistingComment { Path string, Line int, Author, Body string }

# prompt.go
DOES: returns system prompts (full, specialist, summary) and builds user prompt with diff + comments + file context
SYMBOLS: SystemPrompt() → string, UserPrompt(diff, fileContents, existingComments) → string, SpecialistSystemPrompt(categoryID) → string, SummarySystemPrompt() → string
CALLED BY: providers.ClaudeProvider, providers.OpenAIProvider, analysis.PipelineProvider, analysis.RunSpecialist
USE WHEN: modify prompts; categoryIDs: security, api, database, migrations, performance, logic, refactor, tests, dependencies, config, infra, docs

# triage.go
DOES: classifies PR files into category buckets via non-streaming Haiku call; returns TriageResult map
TYPE: TriageResult map[string][]string (category → []filename)
SYMBOLS: Triage(ctx, apiKey, files []PRFile) → (TriageResult, error)
CALLS: anthropic.Messages.New (claude-haiku-4-5-20251001)
CALLED BY: analysis.PipelineProvider.AnalyzePRFull
CONFIG: ANTHROPIC_API_KEY (passed as arg)

# pipeline.go
DOES: two-stage pipeline — triage → parallel (summary + N specialists) → recommendation → done; falls back for small PRs
TYPE: PipelineConfig { APIKey, Caller Analyzer, FallbackAnalyzer Analyzer }
SYMBOLS: NewPipelineProvider(cfg) → *PipelineProvider, AnalyzePRFull(ctx, diff, prFiles, fileContents, existingComments, emit) → error, aboveThreshold(files, diff) → bool
CALLS: analysis.Triage, analysis.RunSpecialist, analysis.SummarySystemPrompt, analysis.UserPrompt, callAndCollect
CALLED BY: handler.Analyze (via FullAnalyzer interface)
USE WHEN: large PRs (≥2 files or ≥50 diff lines)

# specialist.go
DOES: runs single-category AI call — filters diff/files to category, calls callAndCollect with SpecialistSystemPrompt
SYMBOLS: RunSpecialist(ctx, a Analyzer, categoryID, files, diff, fileContents, existingComments) → ([]StreamEvent, error)
CALLS: analysis.FilterDiffByFiles, analysis.callAndCollect, analysis.SpecialistSystemPrompt
CALLED BY: analysis.PipelineProvider.AnalyzePRFull

# diff.go
DOES: filters unified diff to only hunks for specified filenames
SYMBOLS: FilterDiffByFiles(diff string, filenames []string) → string
CALLED BY: analysis.RunSpecialist

# xref.go
DOES: resolves cross-referenced files from diff imports against repo tree; returns unfetched paths
SYMBOLS: ResolveXRefs(diff, diffFiles, repoTree, alreadyFetched) → []string
CALLED BY: handler.Analyze
USE WHEN: after fetching base-branch files, resolve additional context files referenced by imports

# Tests: diff_test.go, pipeline_test.go, pipeline_integration_test.go, specialist_test.go, triage_test.go
USE WHEN: run `go test ./analysis/...`; integration tests need ANTHROPIC_API_KEY
