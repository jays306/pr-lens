package harness

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/pr-lens/backend/analysis"
)

// claudeTextDelta extracts a stream-json text delta. Empty if the line is not text.
func claudeTextDelta(line string) string {
	var ev struct {
		Type  string `json:"type"`
		Event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		} `json:"event"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return ""
	}
	if ev.Type != "stream_event" || ev.Event.Delta.Type != "text_delta" {
		return ""
	}
	return ev.Event.Delta.Text
}

// claudeAssistantText is the completed assistant turn, used when partials are absent.
func claudeAssistantText(line string) string {
	var ev struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Type != "assistant" {
		return ""
	}
	var b strings.Builder
	for _, c := range ev.Message.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// claudeResult is the final stream-json result. isError is set when Claude
// failed (auth, policy, empty prompt) — that text is on stdout, not stderr.
func claudeResult(line string) (text string, isError bool, ok bool) {
	var ev struct {
		Type    string `json:"type"`
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Type != "result" {
		return "", false, false
	}
	if ev.IsError || ev.Error != "" {
		msg := strings.TrimSpace(ev.Result)
		if msg == "" {
			msg = ev.Error
		}
		return msg, true, true
	}
	return ev.Result, false, true
}

func claudeAssistantError(line string) string {
	var ev struct {
		Type    string `json:"type"`
		Error   string `json:"error"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Type != "assistant" || ev.Error == "" {
		return ""
	}
	for _, c := range ev.Message.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			return strings.TrimSpace(c.Text)
		}
	}
	return ev.Error
}

// codexAgentMessage is the agent_message text from a Codex --json line.
func codexAgentMessage(line string) string {
	var ev struct {
		Type string `json:"type"`
		Item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return ""
	}
	if ev.Item.Type != "agent_message" || ev.Item.Text == "" {
		return ""
	}
	return ev.Item.Text
}

func emitJSONEvents(chunk string, buf *strings.Builder, emit func(analysis.StreamEvent) error) error {
	buf.WriteString(chunk)
	s := buf.String()
	i := 0
	last := 0

	for i < len(s) {
		start := strings.IndexByte(s[i:], '{')
		if start == -1 {
			break
		}
		start += i
		depth := 0
		inStr := false
		end := -1
		for j := start; j < len(s); j++ {
			c := s[j]
			if inStr {
				if c == '\\' {
					j++
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
			buf.Reset()
			buf.WriteString(s[start:])
			return nil
		}
		jsonStr := strings.TrimSpace(s[start : end+1])
		if jsonStr != "" {
			ev, err := analysis.ParseStreamEvent(jsonStr)
			if err != nil {
				log.Printf("[harness] parse error (len=%d): %v | prefix: %.120s", len(jsonStr), err, jsonStr)
			} else if ev.Type != "" && ev.Type != "dependency" {
				log.Printf("[harness] emit type=%q", ev.Type)
				if err := emit(ev); err != nil {
					return err
				}
			}
		}
		last = end + 1
		i = end + 1
	}
	buf.Reset()
	if last < len(s) {
		buf.WriteString(s[last:])
	}
	return nil
}
