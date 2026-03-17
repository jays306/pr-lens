// Package analysis defines the core PR analysis interfaces and types.
package analysis

import (
	"bytes"
	"context"
	"encoding/json"
)

// StreamEvent is a chunk of analysis streamed to the caller.
type StreamEvent struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// Analyzer is the interface every AI backend must implement.
type Analyzer interface {
	AnalyzePR(ctx context.Context, userPrompt string, emit func(StreamEvent) error) error
	Name() string
}

// Caller is an optional interface for providers that support explicit system prompts.
// The pipeline uses this to issue focused summary, specialist, and recommendation calls
// without being coupled to a specific SDK. If a provider doesn't implement Caller,
// the pipeline falls back to embedding the system prompt in the user message.
type Caller interface {
	Call(ctx context.Context, systemPrompt, userPrompt string, emit func(StreamEvent) error) error
}

// FullAnalyzer is an optional interface for analyzers that need structured PR data
// rather than a pre-built prompt string. PipelineProvider implements this.
type FullAnalyzer interface {
	AnalyzePRFull(
		ctx context.Context,
		diff string,
		prFiles []PRFile,
		fileContents []FileContent,
		emit func(StreamEvent) error,
	) error
}

// ParseStreamEvent parses a single JSON line into a StreamEvent.
func ParseStreamEvent(line string) (StreamEvent, error) {
	var ev StreamEvent
	if err := json.NewDecoder(bytes.NewBufferString(line)).Decode(&ev); err != nil {
		return ev, err
	}
	return ev, nil
}
