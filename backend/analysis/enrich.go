package analysis

import (
	"regexp"
	"strings"
)

// EnrichCategory rewrites snippet and review-question line numbers so they
// point at the actual changed line in the diff (not the hunk header) and
// carry the GitHub comment side (RIGHT for additions, LEFT for deletions).
func EnrichCategory(ev StreamEvent, idx DiffIndex) StreamEvent {
	ev = SanitizeCategory(ev)
	if ev.Data == nil {
		return ev
	}

	snippets := asObjectSlice(ev.Data["snippets"])
	for _, s := range snippets {
		file, _ := s["file"].(string)
		after, _ := s["after"].(string)
		before, _ := s["before"].(string)
		claimed := asInt(s["lineStart"])
		a := InferSnippetAnchor(idx, file, after, before, claimed)
		if expl, _ := s["explanation"].(string); expl != "" {
			if better := AnchorFromBody(idx, file, a.Line, expl); better.OK {
				a = better
			}
		}
		if !a.OK {
			continue
		}
		s["lineStart"] = a.Line
		s["side"] = a.Side
		s["lineEnd"] = SnippetLineEnd(idx, file, after, before, a.Line)
		if a.Path != "" {
			s["file"] = a.Path
		}
	}
	if snippets != nil {
		ev.Data["snippets"] = anySlice(snippets)
	}

	questions := asObjectSlice(ev.Data["reviewQuestions"])
	kept := make([]map[string]any, 0, len(questions))
	for _, q := range questions {
		file, _ := q["file"].(string)
		claimed := asInt(q["lineStart"])
		after, before := "", ""
		for _, s := range snippets {
			sf, _ := s["file"].(string)
			if file != "" && sf == file {
				after, _ = s["after"].(string)
				before, _ = s["before"].(string)
				if claimed <= 0 {
					claimed = asInt(s["lineStart"])
				}
				break
			}
		}
		text, _ := q["text"].(string)
		if subjectNotInDiff(idx, file, text) {
			continue
		}
		a := AnchorFromBody(idx, file, claimed, text)
		if !a.OK {
			a = InferSnippetAnchor(idx, file, after, before, claimed)
		}
		if a.OK {
			q["lineStart"] = a.Line
			q["side"] = a.Side
			if a.Path != "" {
				q["file"] = a.Path
			}
		}
		kept = append(kept, q)
	}
	if questions != nil {
		ev.Data["reviewQuestions"] = anySlice(kept)
	}

	return ev
}

func asObjectSlice(v any) []map[string]any {
	switch items := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return items
	default:
		return nil
	}
}

func anySlice(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, m := range in {
		out[i] = m
	}
	return out
}

var fillerQuestionRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(is|was|are|were)\b.{0,120}\b(intentional|intended|wanted)\b`),
	regexp.MustCompile(`(?i)\b(intended|intentional|wanted)\?\s*$`),
	regexp.MustCompile(`(?i)\bshould it stay\b`),
	regexp.MustCompile(`(?i)\bstay bounded\b`),
	regexp.MustCompile(`(?i)\b(keep|restore) the old\b`),
	regexp.MustCompile(`(?i)\bis that acceptable\b`),
	regexp.MustCompile(`(?i)\bbad host\b`),
	regexp.MustCompile(`(?i)\bwhere does\b.{0,200}\bcome from\b`),
	regexp.MustCompile(`(?i)\bis it genuinely\b`),
}

// fillerQuestion is a "was this intended?" ask. Those are dropped before the
// recommendation so a test-updated behavior change cannot be reopened.
// subjectNotInDiff is true when the comment leads with a function name that
// does not appear anywhere in that file's diff, so the comment cannot be
// attached to the code it is about.
func subjectNotInDiff(idx DiffIndex, path, text string) bool {
	var subject string
	for _, id := range identRe.FindAllString(text, 6) {
		if len(id) < 6 || id[0] < 'A' || id[0] > 'Z' {
			continue
		}
		subject = id
		break
	}
	if subject == "" || strings.Contains(strings.ToLower(text), "assert") {
		return false
	}
	_, lines := idx.resolvePath(path)
	for _, l := range lines {
		if tokenIn(l.text, subject) {
			return false
		}
	}
	return len(lines) > 0
}

func fillerQuestion(text string) bool {
	for _, re := range fillerQuestionRes {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// SanitizeCategory drops filler "is this intended?" questions and fills a
// missing riskLevel so sort/recommend don't treat blank as critical.
func SanitizeCategory(ev StreamEvent) StreamEvent {
	if ev.Data == nil {
		return ev
	}
	if rl, _ := ev.Data["riskLevel"].(string); strings.TrimSpace(rl) == "" {
		ev.Data["riskLevel"] = "medium"
	}
	questions := asObjectSlice(ev.Data["reviewQuestions"])
	if questions == nil {
		return ev
	}
	kept := make([]map[string]any, 0, len(questions))
	for _, q := range questions {
		text, _ := q["text"].(string)
		if fillerQuestion(text) {
			continue
		}
		kept = append(kept, q)
	}
	ev.Data["reviewQuestions"] = anySlice(kept)
	return ev
}
