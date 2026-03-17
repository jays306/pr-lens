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
			before, after, found := strings.Cut(lineBuf.String(), "\n")
			if !found {
				break
			}
			lineBuf.Reset()
			lineBuf.WriteString(after)
			jsonLine := strings.TrimSpace(before)
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

// runStreamingCall runs a one-shot streaming Anthropic call and collects events matching the keep filter.
func runStreamingCall(ctx context.Context, apiKey, model, systemPrompt, userPrompt string, keep func(StreamEvent) bool) ([]StreamEvent, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})

	var lineBuf strings.Builder
	var events []StreamEvent

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
			before, after, found := strings.Cut(lineBuf.String(), "\n")
			if !found {
				break
			}
			lineBuf.Reset()
			lineBuf.WriteString(after)
			line := strings.TrimSpace(before)
			if line == "" {
				continue
			}
			ev, err := parseStreamEvent(line)
			if err != nil {
				continue
			}
			if keep(ev) {
				events = append(events, ev)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("streaming call: %w", err)
	}
	if rem := strings.TrimSpace(lineBuf.String()); rem != "" {
		if ev, err := parseStreamEvent(rem); err == nil && keep(ev) {
			events = append(events, ev)
		}
	}
	return events, nil
}
