# JUST-PR

## The AI-Native PR Review System

------------------------------------------------------------------------

## The Problem

Pull requests were designed for a world where humans wrote code at human
speed.

Today, AI generates: - Larger PRs
- Faster iteration cycles
- Cross-cutting refactors
- Multi-layer system changes

We didn't remove the bottleneck.
We moved it.

From writing code → to reviewing code.

PR review is now the constraint.

------------------------------------------------------------------------

## The Shift

Traditional PR Review: - Vertical diff scrolling - File-by-file
inspection - High cognitive load - Context switching fatigue

AI-era PR Review needs: - System-level reasoning - Risk-based
prioritization - Structured navigation - Faster signal extraction

JUST-PR rethinks review from the ground up.

------------------------------------------------------------------------

## Core Idea: Horizontal Review

Instead of reviewing vertically by file, JUST-PR organizes PRs
horizontally by system concern:

-   🔌 API
-   🗄 Database
-   🧬 Migrations
-   🔐 Security
-   📦 Dependencies
-   🧪 Tests
-   ⚙️ Config / Infrastructure
-   ⚡ Performance
-   🧱 Refactor / Internal Changes
-   📝 Docs / Other

Review becomes structured and intentional.

------------------------------------------------------------------------

## How It Works

### 1. AI Intelligence Layer

When a PR opens, JUST-PR generates:

-   High-level summary
-   Risk score
-   Criticality classification
-   Affected system map
-   Cross-cutting change detection
-   Suggested review order

Before reading code, reviewers understand impact.

------------------------------------------------------------------------

### 2. Interactive Diff --- Not a Replacement

Raw diffs remain fully visible and accessible.

JUST-PR enhances them by:

-   Semantically grouping diff hunks by concern
-   Highlighting risk areas
-   Extracting relevant snippets per section
-   Allowing instant jump-to-file navigation
-   Collapsible AI annotations
-   Filtered diff views by category

This preserves trust while improving clarity.

------------------------------------------------------------------------

### 3. Structured Review Flow

Reviewers can:

-   Navigate section-by-section
-   Comment per concern
-   Flag risk areas
-   Jump directly into full raw diff context

Each section includes: - AI-generated summary - Highlighted code
segments - Risk indicators - Suggested review questions

No forced wizard flow.
Just structured control.

------------------------------------------------------------------------

### 4. Decision Aggregation

At the end of the review:

-   All flags are summarized
-   Unresolved comments surfaced
-   Risk score updated
-   Suggested final action presented (Approve / Request Changes)

Human judgment remains final.

------------------------------------------------------------------------

## Product Form

JUST-PR ships first as a **Chrome extension**.

### Chrome Extension

When you navigate to any Pull Request page (GitHub, GitLab, Bitbucket), the extension detects the PR URL and injects an overlay panel directly on top of the existing page. No tab switching. No copy-pasting links.

**How it works:**

1. **Detection** — The extension detects you are on a PR page and activates automatically.
2. **Overlay** — A side panel or modal overlays the current PR page without navigating away from it.
3. **Analysis request** — The UI sends the PR URL to the JUST-PR backend.
4. **Streaming results** — The backend fetches the diff, runs AI analysis, and streams the structured review back to the extension in real time (summary, risk score, categorized sections).
5. **Inline experience** — Results appear alongside the native PR interface, so you can cross-reference the AI analysis and the raw diff simultaneously.

The native PR page remains fully functional underneath the overlay. JUST-PR augments it — it does not replace it.

**Future form factors:**

-   GitHub App (server-side, webhook-triggered)
-   Native GitHub integration (long term)

------------------------------------------------------------------------

## Why This Matters

AI-generated PRs will continue to grow in size and complexity.

Humans are not optimized to review: - 5,000+ line diffs - Cross-layer
refactors - Auto-generated migrations - Automated dependency updates

Humans are optimized to review: - Intent - Architecture - Security
implications - Data integrity - Performance risks

JUST-PR bridges AI velocity with human reasoning.

------------------------------------------------------------------------

## Vision

In the agentic development era:

-   AI writes the code
-   AI pre-analyzes the code
-   Humans validate safety, architecture, and intent

JUST-PR becomes the coordination layer between AI execution speed and
human oversight.
