# Design: Two-Stage Parallel PR Analysis Pipeline

**Date:** 2026-03-05
**Status:** Approved

---

## Problem

The current single-call architecture sends the full PR diff and file context to one Claude Sonnet call. This works well for small PRs but has four compounding problems at scale:

- **Latency:** A large PR (~5k lines) takes 20-30s before the last category arrives; the client waits for one long sequential stream.
- **Quality:** A single model call analyzing 11 categories with limited per-category focus degrades analysis quality on complex diffs.
- **Scale:** Diffs beyond ~200k characters are truncated; file context budget is shared across all categories.
- **Cost:** Every PR regardless of size hits Sonnet at full token depth.

---

## Solution: Two-Stage Parallel Pipeline

### Stage 1 — Triage (Haiku, ~2s)

A fast, cheap call that classifies changed files into categories. Input is the compact file list with status and patch size (not full diff content). Output is a `map[string][]string` (category to files).

This call uses `claude-haiku-4-5-20251001`. It does not stream — a single `Messages.New` call returns structured JSON.

**Fallback:** If triage fails or returns malformed JSON, the pipeline falls back to the existing single-call `ClaudeProvider` transparently.

**Threshold:** Pipeline only activates for PRs with >= 10 changed files or >= 500 diff lines. Below that, single-call is faster.

### Stage 2 — Parallel Specialists (Sonnet, concurrent)

One goroutine per affected category, all dispatched simultaneously after triage completes. Each specialist receives:

- Diff hunks filtered to its assigned files (extracted by file path from the full diff string)
- Base file context filtered to its files only
- A category-scoped system prompt instructing it to emit only `category` and `snippet` events for its one category

**Concurrency:** Unbounded goroutines (max 11, one per category). No semaphore needed — Anthropic rate limits are per-token, not per-connection.

**Risk/Summary call:** A separate Sonnet call runs concurrently with the specialists. It receives the triage output (category map + file list) plus the first 2,000 lines of the diff. It produces `risk`, `summary`, and `systems` events. This is typically what the client sees first (~5s).

**Recommendation call:** Fired after all specialists complete. Receives all category summaries (not full diffs). Produces the final `recommendation` event.

---

## Architecture

```
PR URL
  |
  +-- [parallel, existing]
  |    +-- FetchDiff
  |    +-- FetchPRInfo + FetchPRFiles
  |    +-- FetchFileContents
  |
  v
Stage 1: Triage (Haiku)
  input:  file list + patch sizes
  output: map[string][]string  e.g. {"security": ["auth/jwt.go"], "api": ["handlers/user.go"]}
  |
  v (parallel dispatch)
  +-- Risk/Summary/Systems call (Sonnet) ----------------------+
  +-- Specialist: security (Sonnet)                           |
  +-- Specialist: api (Sonnet)                               emit SSE as
  +-- Specialist: database (Sonnet)                          each completes
  +-- ...                                                     |
  |                                                           |
  v (after all specialists done)                             |
  Recommendation call (Sonnet) --------------------------------+
  |
  v
Done event
```

### Latency Profile (large PR, ~5k lines)

| Time  | Event |
|-------|-------|
| t=0s  | GitHub fetches start |
| t=~1s | GitHub fetches complete |
| t=~1s | Triage call starts |
| t=~3s | Triage complete, parallel dispatch |
| t=~5s | Risk/Summary/Systems events stream to client |
| t=~8s | First specialists complete, category events trickle in |
| t=~12s | All specialists done, Recommendation fires |
| t=~14s | Done event |

Compared to current: first output at ~5s vs ~8s; last category at ~14s vs ~20-30s.

---

## Code Structure

```
backend/
  ai/
    claude.go           -- unchanged (single-call provider, used as fallback)
    openai_compat.go    -- unchanged
    provider.go         -- unchanged (Provider interface)
    prompt.go           -- add TriagePrompt(), SpecialistSystemPrompt(category), SummaryPrompt()
    triage.go           -- Haiku triage call -> map[string][]string
    specialist.go       -- single category Sonnet call -> []StreamEvent
    diff_filter.go      -- extract diff hunks for a given set of filenames
    pipeline.go         -- PipelineProvider: orchestrates triage -> parallel specialists -> recommendation
  handlers/
    analyze.go          -- unchanged (PipelineProvider drops in via Provider interface)
```

`PipelineProvider` implements the existing `ai.Provider` interface. `handlers/analyze.go` receives it the same way it receives `ClaudeProvider` today — no handler changes.

---

## Error Handling

| Failure | Behavior |
|---------|----------|
| Triage call fails | Fall back to single-call `ClaudeProvider` |
| Triage returns malformed JSON | Fall back to single-call `ClaudeProvider` |
| PR below threshold | Skip pipeline, use single-call |
| Individual specialist fails | Emit degraded category event with `riskLevel: "unknown"` and error note; other categories unaffected |
| Risk/Summary call fails | Emit minimal risk event (score 50, note); specialists continue |
| Context deadline exceeded | All goroutines respect `ctx` cancellation; partial results already streamed remain visible |

---

## Testing Strategy

**Unit tests:**
- `diff_filter.go` — table tests with fixture diffs; verify correct hunk extraction per file set
- `triage.go` — mock Anthropic client; verify JSON parsing, fallback on malformed response
- `pipeline.go` — mock triage + mock specialists; verify orchestration, partial failure, threshold logic

**Integration test:**
- One end-to-end test against a recorded real PR diff (golden file); verify SSE event sequence is valid and contains all expected event types

**Existing tests unaffected** — `ClaudeProvider` and `AnalyzeHandler` tests require no changes.

---

## Constraints and Non-Goals

- No new external dependencies (same Anthropic SDK, same SSE wire format)
- No changes to the Chrome extension or SSE event schema
- No changes to `handlers/analyze.go`
- GitLab and Bitbucket support deferred (triage file routing only implemented for GitHub for now)
