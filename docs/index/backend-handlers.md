# main.go
DOES: loads .env, wires GitHub client + AI provider + cache + HTTP mux, starts server with CORS
SYMBOLS: main(), buildAnalyzer() → analysis.Analyzer, corsMiddleware(origins, next) → http.Handler, requireEnv(key) → string
CALLS: github.NewClient, providers.NewClaudeProvider, providers.NewOpenAIProvider, analysis.NewPipelineProvider, cache.New, handler.Health, handler.Analyze, handler.Review
ROUTES: GET /health → handler.Health, POST /analyze → handler.Analyze, POST /review → handler.Review
CONFIG: PORT, CORS_ORIGINS, GITHUB_TOKEN, AI_PROVIDER, ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL, ANTHROPIC_MODEL, OPENAI_API_KEY
USE WHEN: entry point; choose provider by AI_PROVIDER env var

# handler/analyze.go
DOES: POST /analyze — fetches diff + PR metadata concurrently, resolves xrefs, checks cache, streams SSE analysis events
SYMBOLS: Analyze(ghClient, analyzer, store) → http.HandlerFunc, Health() → http.HandlerFunc
CALLS: github.ParsePRURL, ghClient.FetchDiff/FetchPRInfo/FetchPRFiles/FetchFileContents/FetchRepoTree/FetchReviewComments, analysis.ResolveXRefs, cache.Key/Get/Replay/Collect, analyzer.AnalyzePRFull/AnalyzePR
USE WHEN: primary analysis endpoint; uses FullAnalyzer path when available, falls back to AnalyzePR

# handler/review.go
DOES: POST /review — validates event type, parses PR URL, posts GitHub PR review with inline comments
TYPE: reviewRequest { URL, Event, Body string, Comments []github.ReviewComment }
SYMBOLS: Review(ghClient) → http.HandlerFunc
CALLS: github.ParsePRURL, ghClient.PostReview
