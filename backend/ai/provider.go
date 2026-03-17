package ai

import (
	"bytes"
	"context"
	"encoding/json"

	ghclient "github.com/just-pr/backend/github"
)

// StreamEvent is a chunk of analysis streamed to the caller.
type StreamEvent struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// Provider is the interface every AI backend must implement.
type Provider interface {
	// AnalyzePR streams structured SSE events for a given user prompt (diff + context).
	AnalyzePR(ctx context.Context, userPrompt string, emit func(StreamEvent) error) error
	Name() string
}

// FullAnalyzer is an optional interface for providers that need structured PR data.
// PipelineProvider implements this; ClaudeProvider and OpenAICompatProvider do not.
type FullAnalyzer interface {
	AnalyzePRFull(
		ctx context.Context,
		diff string,
		prFiles []ghclient.PRFile,
		fileContents []ghclient.FileContent,
		emit func(StreamEvent) error,
	) error
}

// parseStreamEvent parses a single JSON line into a StreamEvent.
func parseStreamEvent(line string) (StreamEvent, error) {
	var ev StreamEvent
	var raw struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(bytes.NewBufferString(line)).Decode(&raw); err != nil {
		return ev, err
	}
	ev.Type = raw.Type
	ev.Data = raw.Data
	return ev, nil
}
