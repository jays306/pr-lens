package analysis

import (
	"os"
	"strings"
	"testing"
)

func TestAnchorFromBody_LivePR1275(t *testing.T) {
	p := os.Getenv("PR1275_DIFF")
	if p == "" {
		t.Skip("set PR1275_DIFF to the PR unified-diff path")
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	idx := ParseDiffIndex(string(raw))

	rawStruct := AnchorFromBody(idx, "internal/service/dropbox-sign.go", 230,
		"The hand-rolled `raw` struct returns a zero-value `Template` with no error when a 200 response has no `template` key.")
	if !rawStruct.OK || rawStruct.Line != 254 || rawStruct.Side != "RIGHT" {
		t.Fatalf("raw struct comment: got %+v, want line 254 RIGHT (var raw struct), not posted line 230", rawStruct)
	}

	fields := AnchorFromBody(idx, "internal/business/webhook_test.go", 399,
		"The POA/W4/W9 fixtures lost their `Fields` payload while still asserting `expectUpsert: true`")
	if !fields.OK || fields.Line == 399 {
		t.Fatalf("Fields comment still on hunk start: %+v", fields)
	}
	t.Logf("Fields comment snapped %d %s → %d %s", 399, "RIGHT", fields.Line, fields.Side)
}

func TestFilterDiffByFiles_Empty(t *testing.T) {
	if FilterDiffByFiles("", []string{"foo.go"}) != "" {
		t.Error("expected empty string for empty diff")
	}
	if FilterDiffByFiles("some diff", nil) != "" {
		t.Error("expected empty string for empty filenames")
	}
}

func TestFilterDiffByFiles_SingleFile(t *testing.T) {
	diff := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+new\n"
	result := FilterDiffByFiles(diff, []string{"foo.go"})
	if !strings.Contains(result, "foo.go") {
		t.Error("expected foo.go in filtered result")
	}
}

func TestFilterDiffByFiles_ExcludesOtherFiles(t *testing.T) {
	diff := "diff --git a/foo.go b/foo.go\ncontent1\ndiff --git a/bar.go b/bar.go\ncontent2\n"
	result := FilterDiffByFiles(diff, []string{"foo.go"})
	if strings.Contains(result, "bar.go") {
		t.Error("expected bar.go to be excluded")
	}
	if !strings.Contains(result, "foo.go") {
		t.Error("expected foo.go in result")
	}
}

func TestFilterDiffByFiles_PathWithSpaces(t *testing.T) {
	diff := "diff --git a/my file.go b/my file.go\n--- a/my file.go\n+++ b/my file.go\n@@ -1 +1 @@\n-old\n+new\n"
	result := FilterDiffByFiles(diff, []string{"my file.go"})
	if !strings.Contains(result, "my file.go") {
		t.Error("expected path with spaces to be kept")
	}
}

const sampleHunkDiff = `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -10,5 +10,6 @@ func Foo() {
 keep
 keep
-old
+new interesting
 keep
`

const getTemplateHunk = `diff --git a/internal/service/dropbox-sign.go b/internal/service/dropbox-sign.go
--- a/internal/service/dropbox-sign.go
+++ b/internal/service/dropbox-sign.go
@@ -227,20 +230,20 @@ func toSignatures() {
 	return signatures, nil
 }
 
 func (d *DropboxSignService) GetTemplate(ctx context.Context, templateID string) (Template, error) {
-	resp, err := d.client.TemplateGetWithResponse(ctx, templateID)
+	// Use the raw client (bypassing generated response parsing) because Dropbox Sign
+	httpResp, err := d.rawClient.TemplateGet(ctx, templateID)
+	var raw struct {
+		Template struct {
+			TemplateID *string ` + "`" + `json:"template_id"` + "`" + `
+		} ` + "`" + `json:"template"` + "`" + `
+	}
+	if err := json.Unmarshal(body, &raw); err != nil {
+		return Template{}, err
+	}
 }
`

func TestSnapCommentAnchor_HunkStartIsNotTheChange(t *testing.T) {
	idx := ParseDiffIndex(getTemplateHunk)
	got := SnapCommentAnchor(idx, "internal/service/dropbox-sign.go", 230)
	if !got.OK || got.Line == 230 {
		t.Fatalf("hunk-start 230 should snap off the closing brace, got %+v", got)
	}
}

func TestAnchorFromBody_RawStructNotHunkHeader(t *testing.T) {
	idx := ParseDiffIndex(getTemplateHunk)
	body := "The hand-rolled `raw` struct returns a zero-value `Template` with no error when a 200 response has no `template` key."
	got := AnchorFromBody(idx, "internal/service/dropbox-sign.go", 230, body)
	if !got.OK || got.Line != 236 || got.Side != "RIGHT" {
		t.Fatalf("got %+v, want the `var raw struct` line (236), not hunk start 230 or first + comment", got)
	}
}

func TestAnchorFromBody_MissingCaseUsesExistingRowNotFixture(t *testing.T) {
	diff := `diff --git a/webhook_test.go b/webhook_test.go
--- a/webhook_test.go
+++ b/webhook_test.go
@@ -399,6 +399,4 @@ func Test(t *testing.T) {
 		{
-			template: service.Template{Fields: map[string]int{}},
+			template:   service.Template{ID: "tmpl_poa", Title: "NY_774_POA"},
 			name:           "returns error when GetTemplate fails",
 			getTemplateErr: errors.New("dropbox sign unavailable"),
 		},
`
	idx := ParseDiffIndex(diff)
	body := "This table has no case where GetTemplate returns ErrNotFound."
	got := AnchorFromBody(idx, "webhook_test.go", 400, body)
	if !got.OK || got.Line == 400 || got.Side != "RIGHT" {
		t.Fatalf("stuck on the POA fixture: %+v", got)
	}
	var matched string
	for _, l := range idx["webhook_test.go"] {
		if l.newLine == got.Line {
			matched = l.text
		}
	}
	if !strings.Contains(matched, "GetTemplate") {
		t.Fatalf("landed on %q (line %d), want the GetTemplate error case", matched, got.Line)
	}
}

func TestAnchorFromBody_CurrentBehaviorStaysOnAddition(t *testing.T) {
	diff := `diff --git a/webhook_test.go b/webhook_test.go
--- a/webhook_test.go
+++ b/webhook_test.go
@@ -10,3 +10,3 @@ func Test(t *testing.T) {
-							sqlmock.AnyArg(), // metadata
+							sqlmock.AnyArg(),
`
	idx := ParseDiffIndex(diff)
	body := "The metadata argument is still AnyArg, so year is not asserted."
	got := AnchorFromBody(idx, "webhook_test.go", 11, body)
	if !got.OK || got.Side != "RIGHT" {
		t.Fatalf("comment about current AnyArg landed on a deletion: %+v", got)
	}
}

func TestAnchorFromBody_StaleVersionNotPinnedSum(t *testing.T) {
	diff := `diff --git a/go.sum b/go.sum
--- a/go.sum
+++ b/go.sum
@@ -37,6 +37,10 @@
+github.com/DataDog/dd-trace-go/contrib/aws/aws-sdk-go-v2/v2 v2.10.1 h1:abc=
+github.com/DataDog/dd-trace-go/contrib/aws/aws-sdk-go-v2/v2 v2.10.1/go.mod h1:def=
+github.com/DataDog/dd-trace-go/contrib/aws/aws-sdk-go/v2 v2.9.1 h1:ghi=
+github.com/DataDog/dd-trace-go/contrib/aws/aws-sdk-go/v2 v2.9.1/go.mod h1:jkl=
`
	idx := ParseDiffIndex(diff)
	body := "go.sum now carries both v2.9.1 and v2.10.1 hashes for dd-trace-go/v2 and the aws-sdk-go-v2 contrib while go.mod pins v2.9.1 — can you re-run `go mod tidy` to drop the stale v2.10.1 entries?"
	got := AnchorFromBody(idx, "go.sum", 40, body)
	if !got.OK || !strings.Contains(lineText(idx, "go.sum", got.Line), "v2.10.1") {
		t.Fatalf("stale v2.10.1 comment landed on %+v (%q)", got, lineText(idx, "go.sum", got.Line))
	}
}

func TestAnchorFromBody_RemovedFlagNotOtherLocalStackComment(t *testing.T) {
	diff := `diff --git a/iam.go b/iam.go
--- a/iam.go
+++ b/iam.go
@@ -29,6 +29,5 @@ func NewSessionProvider() {
 	if cfg.Endpoint != "" {
-		awsCfg.S3ForcePathStyle = aws.Bool(true)
-		awsCfg.DisableSSL = aws.Bool(true)
+		config.WithBaseEndpoint(cfg.Endpoint),
 		logger.Info().Msg("using custom AWS endpoint (LocalStack)")
 	}
@@ -50,3 +49,3 @@ func NewTestSessionProvider() {
-// Intended for tests that need custom session configuration (e.g., LocalStack).
+// Intended for tests that need custom configuration (e.g., LocalStack).
 func NewTestSessionProvider(cfg aws.Config) *SessionProvider {
`
	idx := ParseDiffIndex(diff)
	body := "The LocalStack branch no longer sets S3ForcePathStyle or DisableSSL — in SDK v2 path-style addressing is an S3 client option, so does the LocalStack setup still resolve buckets?"
	got := AnchorFromBody(idx, "iam.go", 52, body)
	text := lineText(idx, "iam.go", got.Line)
	if !got.OK || got.Side != "LEFT" || !strings.Contains(text, "S3ForcePathStyle") && !strings.Contains(text, "DisableSSL") {
		t.Fatalf("removed flag landed on %+v (%q), want the S3ForcePathStyle/DisableSSL deletion", got, text)
	}
}

func TestAnchorFromBody_OnlyFieldLandsOnAssert(t *testing.T) {
	diff := `diff --git a/iam_test.go b/iam_test.go
--- a/iam_test.go
+++ b/iam_test.go
@@ -44,8 +44,8 @@ func TestNewTestSessionProvider(t *testing.T) {
 	baseCfg := aws.Config{
 		Region:      "us-east-1",
 		Credentials: credentials.NewStaticCredentialsProvider("base", "base", "base"),
 	}
 	{
 		name:       "config is preserved",
 		baseConfig: baseCfg,
 	}
 			provider := NewTestSessionProvider(tt.baseConfig)
 			assert.Equal(t, tt.baseConfig.Region, provider.BaseConfig().Region)
`
	idx := ParseDiffIndex(diff)
	body := `Comparing only Region here means the "config is preserved" case no longer checks credentials survive the provider — worth asserting the credentials provider too.`
	got := AnchorFromBody(idx, "iam_test.go", 54, body)
	text := lineText(idx, "iam_test.go", got.Line)
	if !got.OK || !strings.Contains(text, "assert.Equal") || !strings.Contains(text, "Region") {
		t.Fatalf("narrowed assert landed on %+v (%q), want assert.Equal Region", got, text)
	}
}

func lineText(idx DiffIndex, path string, line int) string {
	_, lines := idx.resolvePath(path)
	for _, l := range lines {
		n := l.newLine
		if l.kind == '-' {
			n = l.oldLine
		}
		if n == line {
			return l.text
		}
	}
	return ""
}

func TestAnchorFromBody_FieldsPrefersChangedFixture(t *testing.T) {
	diff := `diff --git a/internal/business/webhook_test.go b/internal/business/webhook_test.go
--- a/internal/business/webhook_test.go
+++ b/internal/business/webhook_test.go
@@ -399,8 +399,4 @@ func TestBusiness_handleTemplateCreated(t *testing.T) {
 		{
 			name:       "POA template - syncs with raw doc_type unchanged",
 			templateID: "tmpl_poa",
-			template: service.Template{
-				ID:    "tmpl_poa",
-				Title: "NY_774_POA",
-				Fields: map[string]service.TemplateDocFields{},
-			},
+			template:   service.Template{ID: "tmpl_poa", Title: "NY_774_POA"},
 			expectUpsert:  true,
 		},
`
	idx := ParseDiffIndex(diff)
	body := "The POA/W4/W9 fixtures lost their `Fields` payload while still asserting `expectUpsert: true`"
	got := AnchorFromBody(idx, "internal/business/webhook_test.go", 399, body)
	if !got.OK {
		t.Fatal("expected a match")
	}
	if got.Line == 399 {
		t.Fatalf("still on hunk start `{`: %+v", got)
	}
}

func TestInferSnippetAnchor_UsesAddedLineNotHunkStart(t *testing.T) {
	idx := ParseDiffIndex(sampleHunkDiff)
	got := InferSnippetAnchor(idx, "foo.go", "new interesting", "old", 10)
	if !got.OK {
		t.Fatal("expected a match")
	}
	if got.Line != 12 || got.Side != "RIGHT" {
		t.Fatalf("got line=%d side=%s, want line=12 side=RIGHT", got.Line, got.Side)
	}
}

func TestInferSnippetAnchor_PureDeletionUsesLeft(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -10,3 +10,2 @@
 keep
-gone
 keep
`
	idx := ParseDiffIndex(diff)
	got := InferSnippetAnchor(idx, "foo.go", "", "gone", 10)
	if !got.OK {
		t.Fatal("expected a match")
	}
	if got.Line != 11 || got.Side != "LEFT" {
		t.Fatalf("got line=%d side=%s, want line=11 side=LEFT", got.Line, got.Side)
	}
}

func TestSnapCommentAnchor_ContextSnapsToAddition(t *testing.T) {
	idx := ParseDiffIndex(sampleHunkDiff)
	got := SnapCommentAnchor(idx, "foo.go", 10)
	if !got.OK {
		t.Fatal("expected a snap")
	}
	if got.Line != 12 || got.Side != "RIGHT" {
		t.Fatalf("got line=%d side=%s, want nearest addition 12 RIGHT", got.Line, got.Side)
	}
}

func TestSnapCommentAnchor_ReplacementPrefersRight(t *testing.T) {
	idx := ParseDiffIndex(sampleHunkDiff)
	got := SnapCommentAnchor(idx, "foo.go", 12)
	if !got.OK || got.Line != 12 || got.Side != "RIGHT" {
		t.Fatalf("got %+v, want replacement 12 RIGHT not LEFT", got)
	}
}

func TestSnapCommentAnchor_KeepsAddedLine(t *testing.T) {
	idx := ParseDiffIndex(sampleHunkDiff)
	got := SnapCommentAnchor(idx, "foo.go", 12)
	if !got.OK || got.Line != 12 || got.Side != "RIGHT" {
		t.Fatalf("got %+v, want added line 12 RIGHT", got)
	}
}

func TestSnapCommentAnchor_DeletionStaysLeft(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -10,3 +10,2 @@
 keep
-gone
 keep
`
	idx := ParseDiffIndex(diff)
	got := SnapCommentAnchor(idx, "foo.go", 11)
	if !got.OK || got.Line != 11 || got.Side != "LEFT" {
		t.Fatalf("got %+v, want deletion 11 LEFT", got)
	}
}

func TestSnapCommentAnchor_UnknownLineSnapsNearest(t *testing.T) {
	idx := ParseDiffIndex(sampleHunkDiff)
	got := SnapCommentAnchor(idx, "foo.go", 99)
	if !got.OK || got.Line != 12 || got.Side != "RIGHT" {
		t.Fatalf("got %+v, want nearest addition 12 RIGHT", got)
	}
}

func TestInferSnippetAnchor_ResolvesSuffixPath(t *testing.T) {
	idx := ParseDiffIndex(sampleHunkDiff)
	got := InferSnippetAnchor(idx, "foo.go", "new interesting", "", 10)
	if !got.OK || got.Path != "foo.go" || got.Line != 12 {
		t.Fatalf("got %+v, want path=foo.go line=12", got)
	}
	idx = ParseDiffIndex("diff --git a/backend/foo.go b/backend/foo.go\n--- a/backend/foo.go\n+++ b/backend/foo.go\n@@ -10,5 +10,6 @@ func Foo() {\n keep\n keep\n-old\n+new interesting\n keep\n")
	got = InferSnippetAnchor(idx, "foo.go", "new interesting", "", 10)
	if !got.OK || got.Path != "backend/foo.go" || got.Line != 12 {
		t.Fatalf("got %+v, want path=backend/foo.go line=12", got)
	}
}

func TestEnrichCategory_KeepsQuestionOnItsOwnHunk(t *testing.T) {
	diff := `diff --git a/foo_test.go b/foo_test.go
--- a/foo_test.go
+++ b/foo_test.go
@@ -1,3 +1,4 @@
 package foo
+import "svc"
 
 func TestA(t *testing.T) {}
@@ -10,3 +11,4 @@ func TestA(t *testing.T) {}
 func TestB(t *testing.T) {
-	want := old
+	want := "2019"
 }
`
	idx := ParseDiffIndex(diff)
	ev := StreamEvent{Type: "category", Data: map[string]any{
		"snippets": []any{
			map[string]any{"file": "foo_test.go", "after": "import \"svc\"\nwant := \"2019\"", "lineStart": float64(2)},
		},
		"reviewQuestions": []any{
			map[string]any{"text": "Why 2019?", "file": "foo_test.go", "lineStart": float64(12)},
		},
	}}
	got := EnrichCategory(ev, idx)
	q := got.Data["reviewQuestions"].([]any)[0].(map[string]any)
	if asInt(q["lineStart"]) != 12 {
		t.Fatalf("question %+v, want lineStart=12 (assertion), not the import hunk", q)
	}
}

func TestEnrichCategory_FixesHunkStartAndQuestions(t *testing.T) {
	idx := ParseDiffIndex(sampleHunkDiff)
	ev := StreamEvent{Type: "category", Data: map[string]any{
		"snippets": []any{
			map[string]any{
				"file": "foo.go", "after": "new interesting", "before": "old",
				"lineStart": float64(10),
			},
		},
		"reviewQuestions": []any{
			map[string]any{"text": "Why?", "file": "foo.go", "lineStart": float64(10)},
		},
	}}
	got := EnrichCategory(ev, idx)
	snips, _ := got.Data["snippets"].([]any)
	snip := snips[0].(map[string]any)
	if asInt(snip["lineStart"]) != 12 || snip["side"] != "RIGHT" {
		t.Fatalf("snippet %+v, want lineStart=12 side=RIGHT", snip)
	}
	qs, _ := got.Data["reviewQuestions"].([]any)
	q := qs[0].(map[string]any)
	if asInt(q["lineStart"]) != 12 || q["side"] != "RIGHT" {
		t.Fatalf("question %+v, want lineStart=12 side=RIGHT", q)
	}
}
