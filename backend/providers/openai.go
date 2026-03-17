package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/just-pr/backend/analysis"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAIProvider implements analysis.Analyzer against any OpenAI-compatible API.
// Works with OpenAI, LiteLLM proxies, Azure OpenAI, etc.
type OpenAIProvider struct {
	client openai.Client
	model  string
}

func NewOpenAIProvider(apiKey, baseURL, model string) *OpenAIProvider {
	if model == "" {
		model = "gpt-4o"
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &OpenAIProvider{client: openai.NewClient(opts...), model: model}
}

func (p *OpenAIProvider) Name() string { return "openai-compat/" + p.model }

func (p *OpenAIProvider) AnalyzePR(ctx context.Context, userPrompt string, emit func(analysis.StreamEvent) error) error {
	return p.Call(ctx, analysis.SystemPrompt(), userPrompt, emit)
}

func (p *OpenAIProvider) Call(ctx context.Context, systemPrompt, userPrompt string, emit func(analysis.StreamEvent) error) error {
	stream := p.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:     openai.ChatModel(p.model),
		MaxTokens: openai.Int(16384),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	})

	var buf strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) == 0 {
			continue
		}
		buf.WriteString(chunk.Choices[0].Delta.Content)
		if err := flushLines(&buf, emit); err != nil {
			return err
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("openai stream: %w", err)
	}
	if rem := strings.TrimSpace(buf.String()); rem != "" {
		if ev, err := analysis.ParseStreamEvent(rem); err == nil {
			_ = emit(ev)
		}
	}
	return nil
}
