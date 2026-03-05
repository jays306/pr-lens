# PR Lens

**AI-native PR review overlay — system-level reasoning for every pull request.**

PR Lens is a Chrome extension + Go backend that injects a structured AI analysis panel directly onto GitHub, GitLab, and Bitbucket pull request pages. No tab switching. No copy-pasting links. The native PR interface stays fully functional underneath.

---

## The Problem

Pull requests were designed for a world where humans wrote code at human speed.

Today, AI generates larger PRs, faster iteration cycles, and cross-cutting refactors. We didn't remove the bottleneck — we moved it. PR review is now the constraint.

PR Lens bridges AI velocity with human reasoning.

---

## How It Works

1. **Detection** — The extension detects when you're on a PR page and activates automatically.
2. **Overlay** — An analysis panel appears alongside the native PR interface.
3. **Analysis** — The UI sends the PR URL to the backend, which fetches the diff and runs AI analysis.
4. **Streaming results** — Structured review streams back in real time: summary, risk score, and categorized sections.

The raw diff remains fully visible. PR Lens augments it — it does not replace it.

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
├── backend/          # Go HTTP server
│   ├── ai/           # AI provider abstraction (Claude, OpenAI-compatible)
│   ├── github/       # GitHub API client
│   ├── handlers/     # HTTP route handlers
│   └── main.go
├── extension/        # Chrome extension (TypeScript + Bun)
│   ├── src/          # Content script, overlay, types
│   ├── public/       # Popup HTML/JS
│   └── manifest.json
└── Makefile
```

---

## Getting Started

### Prerequisites

- [Go 1.21+](https://go.dev/dl/)
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
| `ANTHROPIC_API_KEY` | If using Claude | — | Anthropic API key |
| `OPENAI_API_KEY` | If using OpenAI | — | OpenAI API key |
| `AI_PROVIDER` | No | `claude` | `claude` or `openai` |
| `ANTHROPIC_MODEL` | No | `claude-sonnet-4-6` | Model override |
| `ANTHROPIC_BASE_URL` | No | — | Override for proxies (e.g. LiteLLM) |
| `PORT` | No | `8080` | Server port |
| `CORS_ORIGINS` | No | `*` | Allowed origins (comma-separated) |

---

## API

### `POST /analyze`

Analyze a pull request URL.

**Request:**
```json
{ "url": "https://github.com/owner/repo/pull/123" }
```

**Response:** Server-Sent Events stream of structured analysis events.

### `GET /health`

Returns `200 OK` when the server is running.

---

## Supported Platforms

- GitHub (`github.com/*/pull/*`)
- GitLab (`gitlab.com/*/merge_requests/*`)
- Bitbucket (`bitbucket.org/*/pull-requests/*`)

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
