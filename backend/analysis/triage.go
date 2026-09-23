package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const triageModel = "claude-haiku-4-5-20251001"

// TriageResult maps category ID to the list of filenames assigned to it.
type TriageResult map[string][]string

var knownTriageCategories = map[string]bool{
	"security": true, "api": true, "database": true, "migrations": true,
	"performance": true, "logic": true, "refactor": true, "tests": true,
	"dependencies": true, "config": true, "infra": true, "docs": true,
}

// Triage classifies changed files into categories using a fast Haiku call.
func Triage(ctx context.Context, apiKey string, files []PRFile) (TriageResult, error) {
	if len(files) == 0 {
		return TriageResult{}, nil
	}
	if strings.TrimSpace(apiKey) == "" {
		return HeuristicTriage(files), nil
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(triageModel),
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: triageSystemPrompt()}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(buildTriagePrompt(files)))},
	})
	if err != nil {
		return nil, fmt.Errorf("triage call failed: %w", err)
	}
	if len(msg.Content) == 0 {
		return nil, fmt.Errorf("triage: empty response")
	}
	text, ok := msg.Content[0].AsAny().(anthropic.TextBlock)
	if !ok {
		return nil, fmt.Errorf("triage: unexpected content type")
	}
	return parseTriageResponse(text.Text)
}

func triageSystemPrompt() string {
	return `You are a code classification assistant. Given a list of files changed in a pull request, classify each file into exactly one category.

Available categories:
- security: auth, tokens, input validation, injection risks
- api: route changes, request/response shapes, middleware
- database: queries, schema, connections, transactions
- migrations: schema migrations, data migrations
- performance: N+1 queries, memory, concurrency, caching
- logic: core domain logic, state machines, workflow rules, validation
- refactor: code organization, naming, patterns
- tests: test coverage, test quality, missing tests
- dependencies: package changes, version bumps (go.mod, go.sum, package.json, Gemfile, etc.)
- config: application-level config: env vars, feature flags, app settings files (*.yml, *.env, *.toml that configure the app itself)
- infra: infrastructure and deployment: Helm charts, Kubernetes manifests, Docker, Terraform, CI/CD pipelines, GitHub Actions, Makefile
- docs: documentation, comments, README

Every file must be assigned to exactly one category. Helm/Kubernetes/Docker/CI files go to "infra", not "config". Use "docs" only for pure documentation.

Respond with ONLY a valid JSON object mapping category names to arrays of filenames.
Example: {"dependencies":["go.mod","go.sum"],"infra":["helm/values.yaml","helm/templates/config.yaml"],"logic":["pkg/worker.go"]}
No explanation. No markdown. Only the JSON object.`
}

func buildTriagePrompt(files []PRFile) string {
	var b strings.Builder
	b.WriteString("Classify these changed files into categories:\n\n")
	for _, f := range files {
		fmt.Fprintf(&b, "- %s (%s)\n", f.Filename, f.Status)
	}
	return b.String()
}

func parseTriageResponse(raw string) (TriageResult, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		var inner []string
		for _, l := range strings.Split(raw, "\n") {
			if !strings.HasPrefix(l, "```") {
				inner = append(inner, l)
			}
		}
		raw = strings.TrimSpace(strings.Join(inner, "\n"))
	}

	var parsed TriageResult
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("triage: parse failed: %w", err)
	}

	result := make(TriageResult, len(parsed))
	for cat, names := range parsed {
		if !knownTriageCategories[cat] {
			continue
		}
		result[cat] = names
	}
	return result, nil
}

// HeuristicTriage assigns files to categories from path patterns when a model
// triage call is unavailable (e.g. Bedrock-only deployments).
func HeuristicTriage(files []PRFile) TriageResult {
	out := make(TriageResult)
	assign := func(cat, name string) {
		out[cat] = append(out[cat], name)
	}
	for _, f := range files {
		n := strings.ToLower(f.Filename)
		switch {
		case strings.Contains(n, "migrat"):
			assign("migrations", f.Filename)
		case strings.HasSuffix(n, "_test.go"), strings.Contains(n, "/test/"), strings.Contains(n, "/tests/"),
			strings.HasSuffix(n, ".test.ts"), strings.HasSuffix(n, ".test.tsx"),
			strings.HasSuffix(n, ".spec.ts"), strings.HasSuffix(n, ".spec.tsx"):
			assign("tests", f.Filename)
		case strings.HasSuffix(n, "go.mod"), strings.HasSuffix(n, "go.sum"),
			strings.HasSuffix(n, "package.json"), strings.HasSuffix(n, "package-lock.json"),
			strings.HasSuffix(n, "yarn.lock"), strings.HasSuffix(n, "pnpm-lock.yaml"),
			strings.HasSuffix(n, "gemfile"), strings.HasSuffix(n, "gemfile.lock"),
			strings.HasSuffix(n, "cargo.toml"), strings.HasSuffix(n, "cargo.lock"),
			strings.HasSuffix(n, "poetry.lock"), strings.HasSuffix(n, "requirements.txt"):
			assign("dependencies", f.Filename)
		case strings.Contains(n, ".github/"), strings.Contains(n, "dockerfile"),
			strings.Contains(n, "helm/"), strings.HasSuffix(n, ".tf"),
			strings.Contains(n, "kubernetes"), strings.Contains(n, "/k8s/"),
			strings.Contains(n, "cloudbuild"), strings.Contains(n, "terraform"):
			assign("infra", f.Filename)
		case strings.Contains(n, "auth"), strings.Contains(n, "security"), strings.Contains(n, "secret"):
			assign("security", f.Filename)
		case strings.Contains(n, "/api/"), strings.Contains(n, "handler"), strings.Contains(n, "route"):
			assign("api", f.Filename)
		case strings.Contains(n, ".sql"), strings.Contains(n, "schema"), strings.Contains(n, "/repo"):
			assign("database", f.Filename)
		case strings.HasSuffix(n, ".md"), strings.Contains(n, "readme"):
			assign("docs", f.Filename)
		case strings.Contains(n, ".env"), strings.Contains(n, "config"), strings.HasSuffix(n, ".toml"):
			assign("config", f.Filename)
		default:
			assign("logic", f.Filename)
		}
	}
	return out
}
