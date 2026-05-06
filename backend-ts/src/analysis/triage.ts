import type { PRFile, TriageResult } from "./types.js";
import { makeAnthropicClient } from "../providers/anthropic-client.js";

const TRIAGE_MODEL = "claude-haiku-4-5-20251001";

const VALID_CATEGORIES = new Set([
  "security", "api", "database", "migrations", "performance",
  "logic", "refactor", "tests", "dependencies", "config", "infra", "docs",
]);

export async function triage(apiKey: string, files: PRFile[]): Promise<TriageResult> {
  if (files.length === 0) return {};

  void apiKey; // kept for signature compatibility; client is built from env
  const client = makeAnthropicClient();

  const msg = await client.messages.create({
    model: TRIAGE_MODEL,
    max_tokens: 1024,
    system: triageSystemPrompt(),
    messages: [{ role: "user", content: buildTriagePrompt(files) }],
  });

  const block = msg.content[0];
  if (block.type !== "text") throw new Error("triage: unexpected content type");

  return parseTriageResponse(block.text);
}

function triageSystemPrompt(): string {
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
- config: application-level config: env vars, feature flags, app settings files
- infra: infrastructure and deployment: Helm charts, Kubernetes manifests, Docker, Terraform, CI/CD pipelines, GitHub Actions, Makefile
- docs: documentation, comments, README

Every file must be assigned to exactly one category. Helm/Kubernetes/Docker/CI files go to "infra". Use "docs" only for pure documentation.

Respond with ONLY a valid JSON object mapping category names to arrays of filenames.
Example: {"dependencies":["go.mod","go.sum"],"logic":["pkg/worker.go"]}
No explanation. No markdown. Only the JSON object.`;
}

function buildTriagePrompt(files: PRFile[]): string {
  const lines = ["Classify these changed files into categories:\n"];
  for (const f of files) lines.push(`- ${f.filename} (${f.status})`);
  return lines.join("\n");
}

function parseTriageResponse(raw: string): TriageResult {
  let text = raw.trim();
  // Strip markdown code fences if present
  if (text.startsWith("```")) {
    text = text
      .split("\n")
      .filter((l) => !l.startsWith("```"))
      .join("\n")
      .trim();
  }
  const result: Record<string, string[]> = JSON.parse(text);
  // Filter out unknown categories
  const out: TriageResult = {};
  for (const [cat, files] of Object.entries(result)) {
    if (VALID_CATEGORIES.has(cat)) out[cat] = files;
  }
  return out;
}
