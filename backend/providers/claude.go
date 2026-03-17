package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/just-pr/backend/analysis"
)

// ClaudeProvider implements analysis.Analyzer against the Anthropic API.
type ClaudeProvider struct {
	client anthropic.Client
	model  string
}

func NewClaudeProvider(apiKey, baseURL, model string) *ClaudeProvider {
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &ClaudeProvider{client: anthropic.NewClient(opts...), model: model}
}

func (p *ClaudeProvider) Name() string { return "claude/" + p.model }

func (p *ClaudeProvider) AnalyzePR(ctx context.Context, userPrompt string, emit func(analysis.StreamEvent) error) error {
	return p.Call(ctx, analysis.SystemPrompt(), userPrompt, emit)
}

func (p *ClaudeProvider) Call(ctx context.Context, systemPrompt, userPrompt string, emit func(analysis.StreamEvent) error) error {
	stream := p.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: 16384,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt))},
	})

	var buf strings.Builder
	for stream.Next() {
		delta, ok := stream.Current().AsAny().(anthropic.ContentBlockDeltaEvent)
		if !ok {
			continue
		}
		text, ok := delta.Delta.AsAny().(anthropic.TextDelta)
		if !ok {
			continue
		}
		buf.WriteString(text.Text)
		if err := flushLines(&buf, emit); err != nil {
			return err
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("anthropic stream: %w", err)
	}
	if rem := strings.TrimSpace(buf.String()); rem != "" {
		if ev, err := analysis.ParseStreamEvent(rem); err == nil {
			_ = emit(ev)
		}
	}
	return nil
}
