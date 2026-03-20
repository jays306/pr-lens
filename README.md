# PR Lens

**AI-native PR review overlay — system-level reasoning for every pull request.**

PR Lens is a Chrome extension + Go backend that injects a structured AI analysis panel directly onto GitHub pull request pages. No tab switching. No copy-pasting links. The native PR interface stays fully functional underneath.

---

## The Problem

Pull requests were designed for a world where humans wrote code at human speed.

Today, AI generates larger PRs, faster iteration cycles, and cross-cutting refactors. We didn't remove the bottleneck — we moved it. PR review is now the constraint.

PR Lens bridges AI velocity with human reasoning.

---

## How It Works

1. **Detection** — The extension detects when you're on a GitHub PR page and activates automatically.
2. **Overlay** — An analysis panel appears alongside the native PR interface.
3. **Analysis** — The UI sends the PR URL to the backend, which fetches the diff and runs AI analysis.
4. **Streaming results** — Structured review streams back in real time: summary, risk score, categorized sections, and a final recommendation.
5. **Review submission** — Approve, request changes, or comment directly from the overlay — no need to leave the page.

The raw diff remains fully visible. PR Lens augments it — it does not replace it.

---

## Analysis Pipeline

For non-trivial PRs (≥2 files or ≥50 diff lines), PR Lens uses a two-stage parallel pipeline:

1. **Triage** — A fast model classifies files into system concerns (uses Claude Haiku if available).
2. **Parallel specialists** — Each concern runs concurrently against the relevant diff slice and file contents, including cross-referenced files not in the diff.
3. **Summary + Recommendation** — A summary and final action recommendation (`approve` / `request_changes` / `needs_review`) are derived from the specialist outputs.

Results stream to the extension as Server-Sent Events, sorted by risk level (critical → high → medium → low).

Results are cached by diff hash for 24 hours. Existing review comments are always fetched fresh and shown above the AI analysis.

---

## Horizontal Review

Instead of scrolling through diffs file-by-file, PR Lens organizes changes by system concern:

| Category | Scope |
|---|---|
| 🔌 API | Interface & contract changes |
| 🗄 Database | Schema, queries, indexes |
| 🧬 Migrations | Data migrations |
| 🔐 Security | Auth, input validation, secrets |
| 📦 Dependencies | Package additions/updates |
| 🧪 Tests | Test coverage changes |
| ⚙️ Config / Infrastructure | Env, CI, deployment |
| ⚡ Performance | Hot paths, N+1s, caching |
| 🧱 Refactor | Internal restructuring |
| 📝 Docs / Other | Documentation, comments |

---

## Project Structure

```
just-pr/
├── backend/            # Go HTTP server
│   ├── analysis/       # Pipeline, triage, specialist, prompt logic
│   ├── cache/          # In-memory diff-keyed result cache
│   ├── github/         # GitHub API client
│   ├── handler/        # HTTP route handlers (analyze, review, health)
│   ├── providers/      # AI provider adapters (Claude, OpenAI-compatible)
│   └── main.go
├── extension/          # Chrome extension (TypeScript + Bun)
│   ├── src/            # Content script, overlay UI, types
│   ├── public/         # Popup HTML/JS
│   └── manifest.json
└── Makefile
```

---

## Getting Started

### Prerequisites

- [Go 1.26+](https://go.dev/dl/)
- [Bun](https://bun.sh/)
- A GitHub personal access token (repo read scope)
- An Anthropic or OpenAI API key

### Setup

```bash
# Install all dependencies
make setup

# Configure the backend
cp backend/.env.example backend/.env
# Edit backend/.env — fill in GITHUB_TOKEN and ANTHROPIC_API_KEY
```

### Run

```bash
make dev
```

This builds the extension and starts the backend on `http://localhost:8080`.

Then load the extension in Chrome:
1. Go to `chrome://extensions`
2. Enable **Developer mode**
3. Click **Load unpacked** → select the `extension/` directory

---

## Configuration

All backend config is via environment variables (or `backend/.env`):

| Variable | Required | Default | Description |
|---|---|---|---|
| `GITHUB_TOKEN` | Yes | — | GitHub PAT with repo read access |
| `ANTHROPIC_API_KEY` | If using Claude | — | Anthropic API key (also used for Haiku triage in OpenAI mode) |
| `OPENAI_API_KEY` | If using OpenAI | — | OpenAI API key |
| `AI_PROVIDER` | No | `claude` | `claude` or `openai` |
| `ANTHROPIC_MODEL` | No | `claude-sonnet-4-6` | Model override |
| `ANTHROPIC_BASE_URL` | No | — | Override for proxies (e.g. LiteLLM) — switches to OpenAI-compatible mode |
| `PORT` | No | `8080` | Server port |
| `CORS_ORIGINS` | No | `*` | Allowed origins (comma-separated) |

---

## API

### `POST /analyze`

Analyze a pull request. Streams results as Server-Sent Events.

**Request:**
```json
{ "url": "https://github.com/owner/repo/pull/123" }
```

**Response:** SSE stream of structured analysis events:

| Event type | Description |
|---|---|
| `comments` | Existing review comments on the PR |
| `risk` | Overall risk score and label |
| `summary` | High-level PR summary |
| `systems` | List of affected system concerns |
| `category` | Per-concern analysis (files, findings, risk level) |
| `recommendation` | Final action: `approve`, `request_changes`, or `needs_review` |
| `done` | Stream complete |
| `error` | Analysis error |

### `POST /review`

Submit a GitHub pull request review.

**Request:**
```json
{
  "url": "https://github.com/owner/repo/pull/123",
  "event": "APPROVE",
  "body": "LGTM",
  "comments": []
}
```

`event` must be one of `APPROVE`, `REQUEST_CHANGES`, or `COMMENT`.

### `GET /health`

Returns `{"status":"ok"}` when the server is running.

---

## Make Targets

```
make setup           Install all dependencies
make dev             Build extension + run backend
make backend-run     Run the Go backend
make backend-build   Build the Go binary
make backend-test    Run backend tests
make extension-build Build the Chrome extension
make extension-dev   Watch mode for extension development
make clean           Remove build artifacts
```
