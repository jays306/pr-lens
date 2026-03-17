package analysis

import (
	"context"
	"strings"
)

// RunSpecialist runs a focused analysis for a single category using the given Analyzer.
func RunSpecialist(ctx context.Context, a Analyzer, categoryID string, files []string, diff string, fileContents []FileContent) ([]StreamEvent, error) {
	filteredDiff := FilterDiffByFiles(diff, files)
	if strings.TrimSpace(filteredDiff) == "" {
		return []StreamEvent{{
			Type: "category",
			Data: map[string]any{
				"id": categoryID, "icon": "?", "label": categoryID,
				"summary": "No changes found for this category.", "riskLevel": "low",
				"fileCount": 0, "snippets": []any{}, "reviewQuestions": []any{},
			},
		}}, nil
	}

	fileSet := make(map[string]bool, len(files))
	for _, f := range files {
		fileSet[f] = true
	}
	var filteredContents []FileContent
	for _, fc := range fileContents {
		if fileSet[fc.Path] {
			filteredContents = append(filteredContents, fc)
		}
	}

	return callAndCollect(ctx, a, SpecialistSystemPrompt(categoryID), UserPrompt(filteredDiff, filteredContents), func(ev StreamEvent) bool {
		return ev.Type == "category"
	})
}

func collectSpecialistEvents(lines []string) ([]StreamEvent, error) {
	var events []StreamEvent
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		ev, err := ParseStreamEvent(line)
		if err != nil {
			continue
		}
		if ev.Type == "category" {
			events = append(events, ev)
		}
	}
	return events, nil
}
