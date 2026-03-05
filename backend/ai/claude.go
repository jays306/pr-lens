package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultClaudeModel = "claude-sonnet-4-6"

// ClaudeProvider implements Provider using the official Anthropic Go SDK.
type ClaudeProvider struct {
	client anthropic.Client
	model  string
}

func NewClaudeProvider(apiKey, baseURL, model string) *ClaudeProvider {
	if model == "" {
		model = defaultClaudeModel
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &ClaudeProvider{
		client: anthropic.NewClient(opts...),
		model:  model,
	}
}

func (c *ClaudeProvider) Name() string { return "claude/" + c.model }

func (c *ClaudeProvider) AnalyzePR(ctx context.Context, userPrompt string, emit func(StreamEvent) error) error {
	stream := c.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 16384,
		System: []anthropic.TextBlockParam{
			{Text: SystemPrompt()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})

	var lineBuf strings.Builder
	for stream.Next() {
		event := stream.Current()
		delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent)
		if !ok {
			continue
		}
		text, ok := delta.Delta.AsAny().(anthropic.TextDelta)
		if !ok {
			continue
		}
		lineBuf.WriteString(text.Text)

		for {
			buf := lineBuf.String()
			idx := strings.Index(buf, "\n")
			if idx < 0 {
				break
			}
			jsonLine := strings.TrimSpace(buf[:idx])
			lineBuf.Reset()
			lineBuf.WriteString(buf[idx+1:])
			if jsonLine == "" {
				continue
			}
			ev, err := parseStreamEvent(jsonLine)
			if err != nil {
				continue
			}
			if err := emit(ev); err != nil {
				return err
			}
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("anthropic stream: %w", err)
	}

	if remaining := strings.TrimSpace(lineBuf.String()); remaining != "" {
		if ev, err := parseStreamEvent(remaining); err == nil {
			_ = emit(ev)
		}
	}

	return nil
}
