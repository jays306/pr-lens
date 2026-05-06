import Anthropic from "@anthropic-ai/sdk";

const AI_PROVIDER = process.env.AI_PROVIDER?.toLowerCase() ?? "claude";
const BEDROCK_API_KEY = process.env.BEDROCK_API_KEY ?? "";
const BEDROCK_REGION = process.env.BEDROCK_REGION ?? "us-east-1";
const ANTHROPIC_BASE_URL = process.env.ANTHROPIC_BASE_URL ?? "";
const ANTHROPIC_API_KEY = process.env.ANTHROPIC_API_KEY ?? "";

/** Add "us." cross-region prefix to bare "anthropic.*" Bedrock model IDs. */
function bedrockModelID(model: string): string {
  if (
    model.startsWith("anthropic.") &&
    !model.startsWith("us.") &&
    !model.startsWith("eu.") &&
    !model.startsWith("ap.")
  ) {
    return `us.${model}`;
  }
  return model;
}

/**
 * Resolve the model name.
 * - If a LiteLLM proxy is in use (ANTHROPIC_BASE_URL set), pass the model as-is
 *   since LiteLLM maps it internally.
 * - Bedrock direct: ensure the "us." cross-region prefix is present.
 * - Direct Anthropic API: strip any Bedrock vendor/region prefixes.
 */
export function resolveModel(): string {
  const raw = process.env.ANTHROPIC_MODEL ?? "";

  // LiteLLM proxy: pass model name through unchanged
  if (ANTHROPIC_BASE_URL) {
    return raw || "claude-sonnet-4-6";
  }

  if (AI_PROVIDER === "bedrock") {
    return bedrockModelID(raw || "anthropic.claude-sonnet-4-6");
  }

  // Direct Anthropic API: strip Bedrock prefixes
  let m = raw || "claude-sonnet-4-6";
  m = m.replace(/^(?:us|eu|ap)\./, "");
  m = m.replace(/^anthropic\./, "");
  return m;
}

/**
 * Build an Anthropic SDK client.
 * - LiteLLM proxy (ANTHROPIC_BASE_URL set): use ANTHROPIC_API_KEY + baseURL.
 * - Bedrock direct: Bearer token auth against bedrock-runtime endpoint.
 * - Default: direct Anthropic API with ANTHROPIC_API_KEY.
 */
export function makeAnthropicClient(): Anthropic {
  if (ANTHROPIC_BASE_URL) {
    return new Anthropic({ apiKey: ANTHROPIC_API_KEY, baseURL: ANTHROPIC_BASE_URL });
  }
  if (AI_PROVIDER === "bedrock") {
    const endpoint = `https://bedrock-runtime.${BEDROCK_REGION}.amazonaws.com`;
    return new Anthropic({
      apiKey: "bedrock",
      authToken: BEDROCK_API_KEY || null,
      baseURL: endpoint,
    });
  }
  return new Anthropic({ apiKey: ANTHROPIC_API_KEY });
}

/**
 * Env vars for the Agent SDK subprocess (Claude Code binary).
 *
 * Bedrock: use CLAUDE_CODE_USE_BEDROCK=1 + AWS_BEARER_TOKEN_BEDROCK.
 * This is the official way — the subprocess handles Bedrock auth natively.
 * See: https://code.claude.com/docs/en/amazon-bedrock
 *
 * Direct/proxy: pass ANTHROPIC_API_KEY (+ ANTHROPIC_BASE_URL if set).
 */
export function agentEnv(): Record<string, string> {
  if (AI_PROVIDER === "bedrock" && BEDROCK_API_KEY) {
    return {
      CLAUDE_CODE_USE_BEDROCK: "1",
      AWS_REGION: BEDROCK_REGION,
      AWS_BEARER_TOKEN_BEDROCK: BEDROCK_API_KEY,
      ...(process.env.ANTHROPIC_MODEL ? { ANTHROPIC_MODEL: process.env.ANTHROPIC_MODEL } : {}),
    };
  }
  if (ANTHROPIC_BASE_URL) {
    return { ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL };
  }
  return { ANTHROPIC_API_KEY };
}

export const MODEL = resolveModel();
