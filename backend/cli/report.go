package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pr-lens/backend/analysis"
)

// FormatReport turns streamed analysis events into a terminal review.
func FormatReport(title string, events []analysis.StreamEvent) string {
	if len(events) == 0 {
		return "No analysis events.\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "PR-LENS  %s\n", title)

	var score, level, label, summary, recAction, recReason string
	var categories []analysis.StreamEvent
	var warnings []string

	for _, ev := range events {
		switch ev.Type {
		case "risk":
			score = stringify(ev.Data["score"])
			level, _ = ev.Data["level"].(string)
			label, _ = ev.Data["label"].(string)
		case "summary":
			if t, ok := ev.Data["text"].(string); ok {
				summary += t
			}
		case "category":
			categories = append(categories, ev)
		case "recommendation":
			recAction, _ = ev.Data["action"].(string)
			recReason, _ = ev.Data["reason"].(string)
		case "warning":
			if m, ok := ev.Data["message"].(string); ok && m != "" {
				warnings = append(warnings, m)
			}
		case "error":
			if m, ok := ev.Data["message"].(string); ok && m != "" {
				warnings = append(warnings, m)
			}
		}
	}

	if score != "" || level != "" {
		fmt.Fprintf(&b, "Risk     %s", strings.TrimSpace(score+"  "+level))
		if label != "" {
			fmt.Fprintf(&b, "  %s", label)
		}
		b.WriteByte('\n')
	}
	if s := strings.TrimSpace(summary); s != "" {
		fmt.Fprintf(&b, "\n%s\n", s)
	}
	for _, ev := range categories {
		writeCategory(&b, ev)
	}
	if recAction != "" || recReason != "" {
		fmt.Fprintf(&b, "\nRecommendation  %s\n", recAction)
		if recReason != "" {
			fmt.Fprintf(&b, "%s\n", recReason)
		}
	}
	for _, w := range warnings {
		fmt.Fprintf(&b, "\nWarning  %s\n", w)
	}
	return b.String()
}

func writeCategory(b *strings.Builder, ev analysis.StreamEvent) {
	label, _ := ev.Data["label"].(string)
	if label == "" {
		label, _ = ev.Data["id"].(string)
	}
	risk, _ := ev.Data["riskLevel"].(string)
	summary, _ := ev.Data["summary"].(string)
	fmt.Fprintf(b, "\n## %s", label)
	if risk != "" {
		fmt.Fprintf(b, "  (%s)", risk)
	}
	b.WriteByte('\n')
	if s := strings.TrimSpace(summary); s != "" {
		fmt.Fprintf(b, "%s\n", s)
	}
	for _, q := range objectSlice(ev.Data["reviewQuestions"]) {
		text, _ := q["text"].(string)
		if strings.TrimSpace(text) == "" {
			continue
		}
		file, _ := q["file"].(string)
		line := asInt(q["lineStart"])
		b.WriteByte('\n')
		if file != "" && line > 0 {
			fmt.Fprintf(b, "  %s:%d\n", file, line)
		} else if file != "" {
			fmt.Fprintf(b, "  %s\n", file)
		}
		fmt.Fprintf(b, "  %s\n", text)
	}
}

func objectSlice(v any) []map[string]any {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

func stringify(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case float64:
		return strconv.Itoa(int(n))
	case float32:
		return strconv.Itoa(int(n))
	default:
		return ""
	}
}
