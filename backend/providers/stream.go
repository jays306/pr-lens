package providers

import (
	"log"
	"strings"

	"github.com/pr-lens/backend/analysis"
)

// flushLines emits complete JSON events from buf, handling both single-line
// and pretty-printed (multi-line) JSON by tracking brace depth.
func flushLines(buf *strings.Builder, emit func(analysis.StreamEvent) error) error {
	s := buf.String()
	i := 0
	last := 0

	for i < len(s) {
		// Find the start of a JSON object.
		start := strings.IndexByte(s[i:], '{')
		if start == -1 {
			break
		}
		start += i

		// Walk forward counting braces to find the matching close.
		depth := 0
		inStr := false
		end := -1
		for j := start; j < len(s); j++ {
			c := s[j]
			if inStr {
				if c == '\\' {
					j++ // skip escaped char
				} else if c == '"' {
					inStr = false
				}
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = j
				}
			}
			if end != -1 {
				break
			}
		}

		if end == -1 {
			// Incomplete JSON — keep everything from start onwards in buf.
			buf.Reset()
			buf.WriteString(s[start:])
			return nil
		}

		jsonStr := strings.TrimSpace(s[start : end+1])
		if jsonStr != "" {
			ev, err := analysis.ParseStreamEvent(jsonStr)
			if err != nil {
				log.Printf("[stream] parse error (len=%d): %v | prefix: %.120s", len(jsonStr), err, jsonStr)
			} else {
				log.Printf("[stream] emit type=%q", ev.Type)
				if err := emit(ev); err != nil {
					return err
				}
			}
		}
		last = end + 1
		i = end + 1
	}

	// Keep any trailing incomplete content.
	buf.Reset()
	if last < len(s) {
		buf.WriteString(s[last:])
	}
	return nil
}
