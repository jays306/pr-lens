package providers

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	bedrockpkg "github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/pr-lens/backend/analysis"
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

// bedrockModelID ensures bare "anthropic.*" model IDs get a cross-region
// inference profile prefix ("us.") required for on-demand throughput.
func bedrockModelID(model string) string {
	if strings.HasPrefix(model, "anthropic.") &&
		!strings.HasPrefix(model, "us.") &&
		!strings.HasPrefix(model, "eu.") &&
		!strings.HasPrefix(model, "ap.") {
		return "us." + model
	}
	return model
}

// NewBedrockProvider returns a ClaudeProvider backed by Amazon Bedrock.
// apiKey is used as a bearer token (BEDROCK_API_KEY).
// If apiKey is empty, credentials are loaded from the standard AWS credential
// chain (env vars, ~/.aws/credentials, IAM role, etc.).
func NewBedrockProvider(apiKey, region, model string) *ClaudeProvider {
	if model == "" {
		model = "anthropic.claude-sonnet-4-6"
	}
	model = bedrockModelID(model)
	if region == "" {
		region = "us-east-1"
	}

	endpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)

	var opts []option.RequestOption
	if apiKey != "" {
		opts = []option.RequestOption{
			option.WithAPIKey(apiKey),
			option.WithBaseURL(endpoint),
			bedrockpkg.WithConfig(aws.Config{
				Region:                  region,
				BearerAuthTokenProvider: bedrockpkg.NewStaticBearerTokenProvider(apiKey),
			}),
		}
	} else {
		cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
		if err != nil {
			panic("bedrock: failed to load AWS config: " + err.Error())
		}
		opts = []option.RequestOption{
			option.WithAPIKey("bedrock"),
			bedrockpkg.WithConfig(cfg),
		}
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
	if err := stream.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("anthropic stream: %w", err)
	}
	// Flush any remaining content — handles the last event when the model
	// doesn't emit a trailing newline, or pretty-printed JSON fragments.
	_ = flushLines(&buf, emit)
	return nil
}
