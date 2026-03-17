package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAICompatProvider implements Provider using the official OpenAI Go SDK.
// Works with any OpenAI-compatible API (OpenAI, LiteLLM proxies, Azure OpenAI, etc.)
type OpenAICompatProvider struct {
	client openai.Client
	model  string
}

func NewOpenAICompatProvider(apiKey, baseURL, model string) *OpenAICompatProvider {
	if model == "" {
		model = "gpt-4o"
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &OpenAICompatProvider{
		client: openai.NewClient(opts...),
		model:  model,
	}
}

func (p *OpenAICompatProvider) Name() string { return "openai-compat/" + p.model }

func (p *OpenAICompatProvider) AnalyzePR(ctx context.Context, userPrompt string, emit func(StreamEvent) error) error {
	stream := p.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:     openai.ChatModel(p.model),
		MaxTokens: openai.Int(16384),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(SystemPrompt()),
			openai.UserMessage(userPrompt),
		},
	})

	var lineBuf strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) == 0 {
			continue
		}
		lineBuf.WriteString(chunk.Choices[0].Delta.Content)

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
		return fmt.Errorf("openai stream: %w", err)
	}

	if remaining := strings.TrimSpace(lineBuf.String()); remaining != "" {
		if ev, err := parseStreamEvent(remaining); err == nil {
			_ = emit(ev)
		}
	}

	return nil
}
