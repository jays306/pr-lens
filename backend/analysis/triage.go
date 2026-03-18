package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const triageModel = "claude-haiku-4-5-20251001"

// TriageResult maps category ID to the list of filenames assigned to it.
type TriageResult map[string][]string

// Triage classifies changed files into categories using a fast Haiku call.
func Triage(ctx context.Context, apiKey string, files []PRFile) (TriageResult, error) {
	if len(files) == 0 {
		return TriageResult{}, nil
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(triageModel),
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: triageSystemPrompt()}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(buildTriagePrompt(files)))},
	})
	if err != nil {
		return nil, fmt.Errorf("triage call failed: %w", err)
	}
	if len(msg.Content) == 0 {
		return nil, fmt.Errorf("triage: empty response")
	}
	text, ok := msg.Content[0].AsAny().(anthropic.TextBlock)
	if !ok {
		return nil, fmt.Errorf("triage: unexpected content type")
	}
	return parseTriageResponse(text.Text)
}

func triageSystemPrompt() string {
	return `You are a code classification assistant. Given a list of files changed in a pull request, classify each file into exactly one category.

Available categories:
- security: auth, tokens, input validation, injection risks
- api: route changes, request/response shapes, middleware
- database: queries, schema, connections, transactions
- migrations: schema migrations, data migrations
- performance: N+1 queries, memory, concurrency, caching
- logic: core domain logic, state machines, workflow rules, validation
- refactor: code organization, naming, patterns
- tests: test coverage, test quality, missing tests
- dependencies: package changes, version bumps (go.mod, go.sum, package.json, Gemfile, etc.)
- config: application-level config: env vars, feature flags, app settings files (*.yml, *.env, *.toml that configure the app itself)
- infra: infrastructure and deployment: Helm charts, Kubernetes manifests, Docker, Terraform, CI/CD pipelines, GitHub Actions, Makefile
- docs: documentation, comments, README

Every file must be assigned to exactly one category. Helm/Kubernetes/Docker/CI files go to "infra", not "config". Use "docs" only for pure documentation.

Respond with ONLY a valid JSON object mapping category names to arrays of filenames.
Example: {"dependencies":["go.mod","go.sum"],"infra":["helm/values.yaml","helm/templates/config.yaml"],"logic":["pkg/worker.go"]}
No explanation. No markdown. Only the JSON object.`
}

func buildTriagePrompt(files []PRFile) string {
	var b strings.Builder
	b.WriteString("Classify these changed files into categories:\n\n")
	for _, f := range files {
		fmt.Fprintf(&b, "- %s (%s)\n", f.Filename, f.Status)
	}
	return b.String()
}

func parseTriageResponse(raw string) (TriageResult, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		var inner []string
		for _, l := range strings.Split(raw, "\n") {
			if !strings.HasPrefix(l, "```") {
				inner = append(inner, l)
			}
		}
		raw = strings.TrimSpace(strings.Join(inner, "\n"))
	}

	var result TriageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("triage: parse failed: %w", err)
	}

	return result, nil
}
