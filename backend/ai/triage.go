package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	ghclient "github.com/just-pr/backend/github"
)

const triageModel = "claude-haiku-4-5-20251001"

// TriageResult maps category ID to the list of filenames assigned to it.
type TriageResult map[string][]string

// Triage classifies changed files into categories using a fast Haiku call.
// Returns an error on API failure or malformed JSON.
func Triage(ctx context.Context, apiKey string, files []ghclient.PRFile) (TriageResult, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	prompt := buildTriagePrompt(files)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(triageModel),
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: triageSystemPrompt()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
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

Available categories: security, api, database, migrations, performance, logic, refactor, tests, dependencies, config, docs

Respond with ONLY a valid JSON object mapping category names to arrays of filenames.
Example: {"security":["auth/jwt.go"],"api":["handlers/user.go"]}
No explanation. No markdown. Only the JSON object.`
}

func buildTriagePrompt(files []ghclient.PRFile) string {
	var b strings.Builder
	b.WriteString("Classify these changed files into categories:\n\n")
	for _, f := range files {
		b.WriteString(fmt.Sprintf("- %s (%s)\n", f.Filename, f.Status))
	}
	return b.String()
}

func parseTriageResponse(raw string) (TriageResult, error) {
	raw = strings.TrimSpace(raw)
	// Strip markdown code fences if present
	if strings.HasPrefix(raw, "```") {
		var inner []string
		for _, l := range strings.Split(raw, "\n") {
			if strings.HasPrefix(l, "```") {
				continue
			}
			inner = append(inner, l)
		}
		raw = strings.TrimSpace(strings.Join(inner, "\n"))
	}

	var result TriageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("triage: parse failed: %w", err)
	}
	return result, nil
}
