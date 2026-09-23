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

1. **Detection** — The extension detects when you're on a GitHub pull request page and activates automatically.
2. **Overlay** — An analysis panel appears alongside the native PR interface.
3. **Analysis** — The UI sends the PR URL to the backend, which clones the PR head and runs Claude Code or Codex in that tree.
4. **Streaming results** — Structured review streams back in real time: summary, risk score, categorized sections, and a final recommendation.
5. **Review submission** — Approve, request changes, or comment directly from the overlay — no need to leave the page.

The raw diff remains fully visible. PR Lens augments it — it does not replace it.

---

## Analysis Pipeline

Reviews run in a shallow clone of the pull request head:

1. **Fetch** — Diff, PR title/body, and existing human comments from GitHub.
2. **Clone** — PR head at the review SHA.
3. **Harness** — `claude -p` or `codex exec` in that directory. The agent can install dependencies and read their source before it writes a finding.
4. **Events** — The same JSON event sequence (risk, summary, categories, recommendation) streams to the CLI and the extension.

Results are cached by diff hash for 24 hours. Existing review comments are always fetched fresh and shown above the analysis.

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
pr-lens/
├── backend/            # Go HTTP server + CLI
│   ├── analysis/       # Prompts, diff anchors, leftover pipeline helpers
│   ├── analyze/        # Shared fetch + stream used by HTTP and CLI
│   ├── harness/        # Claude Code / Codex subprocess reviewer
│   ├── cmd/pr-lens/    # Terminal review CLI
│   ├── cache/          # In-memory diff-keyed result cache
│   ├── github/         # GitHub API client
│   ├── handler/        # HTTP route handlers (analyze, review, health)
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
- `claude` (Claude Code) or `codex` on PATH, already signed in or keyed the way that CLI expects

### Setup

```bash
# Install all dependencies
make setup

# Configure the backend
cp backend/.env.example backend/.env
# Edit backend/.env — fill in GITHUB_TOKEN (and AGENT_HARNESS if not using Claude Code)
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

### CLI

Review a pull request from the terminal (same `.env` and pipeline as the server):

```bash
make review URL=https://github.com/owner/repo/pull/123

# shorthand
make review URL=owner/repo#123

# print events as JSON
make review URL=owner/repo#123 FLAGS=--json

# post findings + recommendation to GitHub
make review URL=owner/repo#123 FLAGS=--post

# JSON and post together; override the GitHub token for this run
make review URL=owner/repo#123 FLAGS="--json --post --token ghp_..."
```

`FLAGS` is passed through to the CLI. Flags:

| Flag | Description |
|---|---|
| `--json` | Print the analysis events as JSON |
| `--post` | Post the findings and recommendation to GitHub |
| `--token TOKEN` | GitHub PAT for this run (default: `GITHUB_TOKEN`) |

The same flags work on the binary after `make cli-build`: `./backend/bin/pr-lens owner/repo#123 --json`.

---

## Configuration

All backend config is via environment variables (or `backend/.env`):

| Variable | Required | Default | Description |
|---|---|---|---|
| `GITHUB_TOKEN` | For local `/analyze` only | — | Optional server PAT. `/review` never uses this; the extension must send a user PAT. |
| `REQUIRE_CLIENT_GITHUB_TOKEN` | No | unset | When `true`, `/analyze` also requires a client PAT (recommended in deploy). |
| `AGENT_HARNESS` | No | from `AI_PROVIDER` | `claude` or `codex`. Overrides `AI_PROVIDER` for which local binary reviews. |
| `AI_PROVIDER` | No | `claude` | `claude`/`bedrock` → Claude Code; `openai` → Codex |
| `CLAUDE_BIN` / `CODEX_BIN` | No | `claude` / `codex` | Path to the harness binary |
| `ANTHROPIC_MODEL` | If using Claude Code | Claude Code default | Passed as `--model`. Bedrock IDs get a `us.` prefix. |
| `ANTHROPIC_API_KEY` | If Claude Code needs it | — | Passed through to the `claude` process |
| `OPENAI_API_KEY` | If Codex needs it | — | Passed through to the `codex` process |
| `PORT` | No | `8080` | Server port |
| `CORS_ORIGINS` | No | `https://github.com` | Allowed origins (comma-separated). Do not use `*` on a shared host. |

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

`event` must be one of `APPROVE`, `REQUEST_CHANGES`, or `COMMENT`. The caller must send `Authorization: Bearer <github-pat>`; the server token is not accepted.

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
