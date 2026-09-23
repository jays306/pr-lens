import { JustPROverlay } from "./overlay";

let overlay = new JustPROverlay("https://github.com/owner/repo/pull/42");

// Allow the test page to swap the PR URL without a full reload
document.addEventListener("pr-lens-reinit", (e) => {
  const url = (e as CustomEvent<{ url: string }>).detail.url;
  overlay.destroy();
  overlay = new JustPROverlay(url);
  maybePreview();
});

function maybePreview(): void {
  const screen = new URLSearchParams(location.search).get("preview");
  if (!screen) return;
  overlay.previewAnalysis({
    riskScore: 72,
    riskLevel: "high",
    riskLabel: "Needs careful review",
    summary: "Template fetch now bypasses generated response parsing. Tests dropped Fields payloads while still asserting upsert.",
    categories: [
      {
        id: "logic",
        icon: "⚙",
        label: "Logic",
        summary: "GetTemplate unmarshals a hand-rolled raw struct and can return a zero Template on a 200 with no template key.",
        riskLevel: "high",
        fileCount: 2,
        snippets: [
          {
            file: "internal/service/dropbox-sign.go",
            language: "go",
            before: "resp, err := d.client.TemplateGetWithResponse(ctx, templateID)",
            after: "httpResp, err := d.rawClient.TemplateGet(ctx, templateID)\nvar raw struct {\n  Template struct {\n    TemplateID *string `json:\"template_id\"`\n  } `json:\"template\"`\n}",
            lineStart: 233,
            explanation: "A 200 with no template key returns a zero value and no error.",
            riskLevel: "high",
          },
          {
            file: "internal/business/webhook_test.go",
            language: "go",
            before: "template: service.Template{\n  ID: \"tmpl_poa\",\n  Fields: map[string]service.TemplateDocFields{},\n}",
            after: "template: service.Template{ID: \"tmpl_poa\", Title: \"NY_774_POA\"},",
            lineStart: 402,
            explanation: "Fixtures lost Fields while still asserting expectUpsert.",
            riskLevel: "medium",
          },
        ],
        reviewQuestions: [
          {
            text: "The raw struct returns a zero Template when a 200 has no template key.",
            file: "internal/service/dropbox-sign.go",
            lineStart: 233,
          },
          {
            text: "What happens on an empty Fields map after this fixture change?",
            file: "internal/business/webhook_test.go",
            lineStart: 402,
          },
        ],
      },
      {
        id: "tests",
        icon: "🧪",
        label: "Tests",
        summary: "Webhook fixtures were compacted.",
        riskLevel: "medium",
        fileCount: 1,
        snippets: [
          {
            file: "internal/business/webhook_test.go",
            language: "go",
            before: "Fields: map[string]service.TemplateDocFields{},",
            after: "template: service.Template{ID: \"tmpl_poa\", Title: \"NY_774_POA\"},",
            lineStart: 402,
            explanation: "POA fixture no longer carries Fields.",
            riskLevel: "medium",
          },
        ],
        reviewQuestions: [
          {
            text: "Should these cases still assert Fields after the compact fixture?",
            file: "internal/business/webhook_test.go",
            lineStart: 402,
          },
        ],
      },
    ],
    existingComments: [
      {
        author: "reviewer",
        body: "Can we keep the Fields assertion?",
        path: "internal/business/webhook_test.go",
        line: 402,
        anchor: "inline",
        source: "review_comment",
      },
    ],
    recommendation: {
      action: "request_changes",
      reason: "Handle a missing template key before merge.",
    },
  }, screen === "step" || screen === "decision" ? screen : "overview");
}

maybePreview();
