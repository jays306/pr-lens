import type { PRFile, TriageResult } from "./types.js";
import { makeAnthropicClient } from "../providers/anthropic-client.js";

const USE_BEDROCK = process.env.CLAUDE_CODE_USE_BEDROCK === "1";
// Bedrock requires the us. cross-region prefix and the -v1:0 suffix.
const TRIAGE_MODEL = USE_BEDROCK
  ? "us.anthropic.claude-haiku-4-5-20251001-v1:0"
  : "claude-haiku-4-5-20251001";

const VALID_CATEGORIES = new Set([
  "security", "api", "database", "migrations", "performance",
  "logic", "refactor", "tests", "dependencies", "config", "infra", "docs",
]);

export async function triage(apiKey: string, files: PRFile[]): Promise<TriageResult> {
  if (files.length === 0) {
    console.log("[triage] skipped: 0 files");
    return {};
  }

  void apiKey; // kept for signature compatibility; client is built from env
  const client = makeAnthropicClient();

  const t = Date.now();
  console.log(`[triage] calling ${TRIAGE_MODEL} for ${files.length} files`);
  const msg = await client.messages.create({
    model: TRIAGE_MODEL,
    max_tokens: 1024,
    system: triageSystemPrompt(),
    messages: [{ role: "user", content: buildTriagePrompt(files) }],
  });
  console.log(`[triage] model responded in ${Date.now() - t}ms`);

  if (!msg.content || msg.content.length === 0) {
    console.error("[triage] unexpected response shape:", JSON.stringify(msg).slice(0, 500));
    throw new Error("triage: empty content array");
  }
  const block = msg.content[0];
  if (block.type !== "text") throw new Error(`triage: unexpected content type: ${block.type}`);

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

Your entire response must be the single JSON object — starting with \`{\` and ending with \`}\`. No preamble, no explanation after, no markdown fences, no extra paragraphs. If you find yourself tempted to write commentary, stop and just emit the JSON.`;
}

function buildTriagePrompt(files: PRFile[]): string {
  const lines = ["Classify these changed files into categories:\n"];
  for (const f of files) lines.push(`- ${f.filename} (${f.status})`);
  return lines.join("\n");
}

function parseTriageResponse(raw: string): TriageResult {
  let text = raw.trim();

  // Strip markdown code fences if present.
  if (text.startsWith("```")) {
    text = text
      .split("\n")
      .filter((l) => !l.startsWith("```"))
      .join("\n")
      .trim();
  }

  // Haiku sometimes appends explanatory prose after the JSON (e.g. a second
  // paragraph describing the classification). Extract just the first
  // balanced top-level JSON object by tracking brace depth and string state.
  const jsonBody = extractFirstJsonObject(text);
  if (!jsonBody) {
    console.error("[triage] no JSON object found in response:", text.slice(0, 300));
    throw new Error("triage: no JSON object in response");
  }

  const result: Record<string, string[]> = JSON.parse(jsonBody);
  const out: TriageResult = {};
  for (const [cat, files] of Object.entries(result)) {
    if (VALID_CATEGORIES.has(cat) && Array.isArray(files)) out[cat] = files;
  }
  return out;
}

function extractFirstJsonObject(text: string): string | null {
  const start = text.indexOf("{");
  if (start === -1) return null;
  let depth = 0;
  let inString = false;
  let escape = false;
  for (let i = start; i < text.length; i++) {
    const ch = text[i];
    if (escape) { escape = false; continue; }
    if (inString) {
      if (ch === "\\") { escape = true; continue; }
      if (ch === '"') inString = false;
      continue;
    }
    if (ch === '"') { inString = true; continue; }
    if (ch === "{") depth++;
    else if (ch === "}") {
      depth--;
      if (depth === 0) return text.slice(start, i + 1);
    }
  }
  return null;
}
