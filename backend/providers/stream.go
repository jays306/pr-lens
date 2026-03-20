package providers

import (
	"strings"

	"github.com/pr-lens/backend/analysis"
)

// flushLines emits all complete newline-delimited JSON events from buf.
func flushLines(buf *strings.Builder, emit func(analysis.StreamEvent) error) error {
	for {
		before, after, found := strings.Cut(buf.String(), "\n")
		if !found {
			return nil
		}
		buf.Reset()
		buf.WriteString(after)
		line := strings.TrimSpace(before)
		if line == "" {
			continue
		}
		ev, err := analysis.ParseStreamEvent(line)
		if err != nil {
			continue
		}
		if err := emit(ev); err != nil {
			return err
		}
	}
}
