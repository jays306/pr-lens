package analysis

import (
	"strings"
	"testing"
)

func TestEnrichCategory_DropsCommentAboutUnchangedFunction(t *testing.T) {
	diff := `diff --git a/dropbox-sign.go b/dropbox-sign.go
--- a/dropbox-sign.go
+++ b/dropbox-sign.go
@@ -232,2 +232,3 @@
 func (d *DropboxSignService) GetTemplate(ctx context.Context, templateID string) (Template, error) {
+	httpResp, err := d.rawClient.TemplateGet(ctx, templateID)
 }
`
	idx := ParseDiffIndex(diff)
	ev := StreamEvent{Type: "category", Data: map[string]any{
		"reviewQuestions": []any{
			map[string]any{"text": "ListTemplates still uses the generated parser — does the integer signer field break it too?", "file": "dropbox-sign.go", "lineStart": float64(233)},
			map[string]any{"text": "GetTemplate now bypasses the generated parser.", "file": "dropbox-sign.go", "lineStart": float64(233)},
		},
	}}
	got := EnrichCategory(ev, idx)
	qs := asObjectSlice(got.Data["reviewQuestions"])
	if len(qs) != 1 {
		t.Fatalf("got %d questions, want only the GetTemplate one: %+v", len(qs), qs)
	}
	if !strings.Contains(qs[0]["text"].(string), "GetTemplate") {
		t.Fatalf("kept %+v", qs[0])
	}
}

func TestSanitizeCategory_DropsIntentQuestions(t *testing.T) {
	ev := StreamEvent{Type: "category", Data: map[string]any{
		"reviewQuestions": []any{
			map[string]any{"text": "Should ErrConflict also return 422?", "file": "a.go", "lineStart": 1},
			map[string]any{"text": "The regex widened to 20xx — is that widening intended?", "file": "b.go", "lineStart": 2},
			map[string]any{"text": "Is the sync job's behavior what you wanted here?", "file": "c.go", "lineStart": 3},
			map[string]any{"text": "The year regex widened from 202[0-9]|2030 to 20xx, so titles containing 2015 now yield that year instead of the current one — intended?", "file": "d.go", "lineStart": 4},
			map[string]any{"text": "The year regex widened from 202[0-9]|2030 to 20\\d{2}, so a title containing 2015 now yields that year — should it stay bounded?", "file": "e.go", "lineStart": 5},
			map[string]any{"text": "ErrFromHTTP turns any 404 into ErrNotFound, so a bad host would also surface as 422 — is that acceptable here?", "file": "f.go", "lineStart": 6},
			map[string]any{"text": "Where does the new indirect contrib requirement come from?", "file": "go.mod", "lineStart": 84},
			map[string]any{"text": "Where does the new indirect contrib/aws/aws-sdk-go-v2/v2 come from, given the other contrib is the one imported?", "file": "go.mod", "lineStart": 84},
			map[string]any{"text": "go.temporal.io/api moved from direct to indirect here — is it genuinely no longer imported directly?", "file": "go.mod", "lineStart": 317},
		},
	}}
	got := SanitizeCategory(ev)
	qs := asObjectSlice(got.Data["reviewQuestions"])
	if len(qs) != 1 {
		t.Fatalf("got %d questions, want 1 (the ErrConflict one): %+v", len(qs), qs)
	}
	if qs[0]["text"] != "Should ErrConflict also return 422?" {
		t.Fatalf("kept %+v", qs[0])
	}
	if got.Data["riskLevel"] != "medium" {
		t.Fatalf("empty riskLevel should default to medium, got %v", got.Data["riskLevel"])
	}
}
