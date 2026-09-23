package analysis

import (
	"context"
	"log"
	"strings"
)

// RunSpecialist runs a focused analysis for a single category using the given Analyzer.
func RunSpecialist(ctx context.Context, a Analyzer, categoryID string, files []string, diff string, fileContents []FileContent, existingComments []ExistingComment, pr *PRInfo) ([]StreamEvent, error) {
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

	// Filter comments to only those on this category's files (inline + file-anchor).
	// PR-level discussion is not scoped to specific files and only adds noise here.
	var filteredComments []ExistingComment
	for _, c := range existingComments {
		if fileSet[c.Path] {
			filteredComments = append(filteredComments, c)
		}
	}

	events, err := callAndCollect(ctx, a, SpecialistSystemPrompt(categoryID), UserPrompt(filteredDiff, filteredContents, filteredComments, pr), func(ev StreamEvent) bool {
		return ev.Type == "category"
	})
	log.Printf("[specialist] %q collected %d events (err=%v)", categoryID, len(events), err)
	return events, err
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
