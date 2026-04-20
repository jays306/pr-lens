# providers/stream.go
DOES: flushes newline-delimited JSON lines from buffer, parses each as StreamEvent
SYMBOLS: flushLines(buf *strings.Builder, emit func(StreamEvent) error) → error
CALLED BY: ClaudeProvider.Call, OpenAIProvider.Call

# providers/claude.go
DOES: wraps Anthropic SDK streaming into Analyzer and Caller interfaces
TYPE: ClaudeProvider { client anthropic.Client, model string }
SYMBOLS: NewClaudeProvider(apiKey, baseURL, model) → *ClaudeProvider, AnalyzePR(ctx, userPrompt, emit) → error, Call(ctx, systemPrompt, userPrompt, emit) → error
CALLED BY: main.buildAnalyzer, analysis.callAndCollect
CONFIG: ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL, ANTHROPIC_MODEL (default: claude-sonnet-4-6)

# providers/openai.go
DOES: wraps OpenAI-compatible SDK streaming into Analyzer and Caller interfaces; handles LiteLLM/Azure proxies
TYPE: OpenAIProvider { client openai.Client, model string }
SYMBOLS: NewOpenAIProvider(apiKey, baseURL, model) → *OpenAIProvider, AnalyzePR(ctx, userPrompt, emit) → error, Call(ctx, systemPrompt, userPrompt, emit) → error
CALLED BY: main.buildAnalyzer, analysis.callAndCollect
CONFIG: OPENAI_API_KEY, ANTHROPIC_BASE_URL (openai-compat mode)
USE WHEN: AI_PROVIDER=openai or ANTHROPIC_BASE_URL set with claude provider
