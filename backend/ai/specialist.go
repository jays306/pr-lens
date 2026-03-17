package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	ghclient "github.com/just-pr/backend/github"
)

// RunSpecialist runs a focused Sonnet analysis for a single category.
// It returns all category-type StreamEvents emitted by the model.
func RunSpecialist(ctx context.Context, apiKey, model, categoryID string, files []string, diff string, fileContents []ghclient.FileContent) ([]StreamEvent, error) {
	filteredDiff := FilterDiffByFiles(diff, files)
	if strings.TrimSpace(filteredDiff) == "" {
		return []StreamEvent{{
			Type: "category",
			Data: map[string]any{
				"id":              categoryID,
				"icon":            "?",
				"label":           categoryID,
				"summary":         "No changes found for this category.",
				"riskLevel":       "low",
				"fileCount":       0,
				"snippets":        []any{},
				"reviewQuestions": []any{},
			},
		}}, nil
	}

	fileSet := make(map[string]bool, len(files))
	for _, f := range files {
		fileSet[f] = true
	}
	var filteredContents []ghclient.FileContent
	for _, fc := range fileContents {
		if fileSet[fc.Path] {
			filteredContents = append(filteredContents, fc)
		}
	}

	userPrompt := UserPrompt(filteredDiff, filteredContents)
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 8192,
		System: []anthropic.TextBlockParam{
			{Text: SpecialistSystemPrompt(categoryID)},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	})

	var lineBuf strings.Builder
	var jsonLines []string

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
			if line := strings.TrimSpace(before); line != "" {
				jsonLines = append(jsonLines, line)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("specialist %s: stream error: %w", categoryID, err)
	}
	if rem := strings.TrimSpace(lineBuf.String()); rem != "" {
		jsonLines = append(jsonLines, rem)
	}

	return collectSpecialistEvents(jsonLines)
}

// collectSpecialistEvents parses JSON lines and returns only category-type events.
// Malformed lines and non-category events (including "done") are silently skipped.
func collectSpecialistEvents(lines []string) ([]StreamEvent, error) {
	var events []StreamEvent
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		ev, err := parseStreamEvent(line)
		if err != nil {
			continue
		}
		if ev.Type == "category" {
			events = append(events, ev)
		}
	}
	return events, nil
}
