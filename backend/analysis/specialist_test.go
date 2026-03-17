package analysis

import (
	"testing"
)

func TestCollectSpecialistEvents(t *testing.T) {
	lines := []string{
		`{"type":"category","data":{"id":"security","label":"Security","riskLevel":"high","fileCount":1,"snippets":[],"reviewQuestions":[]}}`,
		`{"type":"done","data":{}}`,
		``,
	}
	events, err := collectSpecialistEvents(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "category" {
		t.Errorf("expected type 'category', got %q", events[0].Type)
	}
}

func TestCollectSpecialistEvents_SkipsNonCategory(t *testing.T) {
	lines := []string{
		`{"type":"risk","data":{"score":50}}`,
		`{"type":"category","data":{"id":"api","label":"API","riskLevel":"low","fileCount":0,"snippets":[],"reviewQuestions":[]}}`,
	}
	events, _ := collectSpecialistEvents(lines)
	if len(events) != 1 || events[0].Type != "category" {
		t.Errorf("expected only category event, got %v", events)
	}
}

func TestCollectSpecialistEvents_EmptyLines(t *testing.T) {
	events, err := collectSpecialistEvents([]string{"", "   ", ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for empty lines, got %d", len(events))
	}
}

func TestCollectSpecialistEvents_MalformedJSON(t *testing.T) {
	lines := []string{
		`not json at all`,
		`{"type":"category","data":{"id":"api","label":"API","riskLevel":"low","fileCount":0,"snippets":[],"reviewQuestions":[]}}`,
	}
	events, _ := collectSpecialistEvents(lines)
	if len(events) != 1 {
		t.Errorf("expected 1 event (malformed line skipped), got %d", len(events))
	}
}
