import Anthropic from "@anthropic-ai/sdk";

const USE_BEDROCK = process.env.CLAUDE_CODE_USE_BEDROCK === "1";
const AWS_REGION = process.env.AWS_REGION ?? "us-east-1";
const AWS_BEARER_TOKEN_BEDROCK = process.env.AWS_BEARER_TOKEN_BEDROCK ?? "";
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
 * Resolve the model name for the configured backend.
 * - Bedrock: ensure us. cross-region prefix is present on anthropic.* IDs
 * - LiteLLM proxy (ANTHROPIC_BASE_URL): pass model as-is (proxy handles mapping)
 * - Direct Anthropic API: strip Bedrock prefixes
 */
export function resolveModel(): string {
  const raw = process.env.ANTHROPIC_MODEL ?? "";

  if (USE_BEDROCK) {
    return bedrockModelID(raw || "anthropic.claude-sonnet-4-6");
  }
  if (ANTHROPIC_BASE_URL) {
    return raw || "claude-sonnet-4-6";
  }
  let m = raw || "claude-sonnet-4-6";
  m = m.replace(/^(?:us|eu|ap)\./, "");
  m = m.replace(/^anthropic\./, "");
  return m;
}

/**
 * Build an Anthropic SDK client for direct (non-agent) calls.
 * - Bedrock: Bearer token auth against bedrock-runtime endpoint
 * - LiteLLM proxy: ANTHROPIC_API_KEY + baseURL
 * - Default: direct Anthropic API
 */
export function makeAnthropicClient(): Anthropic {
  if (USE_BEDROCK) {
    return new Anthropic({
      apiKey: "bedrock",
      authToken: AWS_BEARER_TOKEN_BEDROCK || null,
      baseURL: `https://bedrock-runtime.${AWS_REGION}.amazonaws.com`,
    });
  }
  if (ANTHROPIC_BASE_URL) {
    return new Anthropic({ apiKey: ANTHROPIC_API_KEY, baseURL: ANTHROPIC_BASE_URL });
  }
  return new Anthropic({ apiKey: ANTHROPIC_API_KEY });
}

/**
 * Env vars for the Agent SDK subprocess (Claude Code binary).
 *
 * The Agent SDK uses `env` as a REPLACEMENT for process.env in the subprocess,
 * not a merge. So we spread process.env first, then layer overrides on top.
 *
 * For Bedrock, the official env vars are CLAUDE_CODE_USE_BEDROCK=1 +
 * AWS_BEARER_TOKEN_BEDROCK + AWS_REGION. We also clear any SSO profile state
 * from ~/.aws/config to prevent the AWS SDK from preferring an expired SSO
 * session over the bearer token.
 */
export function agentEnv(): Record<string, string> {
  const base = Object.fromEntries(
    Object.entries(process.env).filter((e): e is [string, string] => e[1] !== undefined)
  );

  if (USE_BEDROCK && AWS_BEARER_TOKEN_BEDROCK) {
    return {
      ...base,
      CLAUDE_CODE_USE_BEDROCK: "1",
      AWS_REGION,
      AWS_BEARER_TOKEN_BEDROCK,
      // Prevent AWS SDK from loading ~/.aws/config profiles that might have
      // expired SSO sessions; force it to use the bearer token only.
      AWS_PROFILE: "",
      AWS_CONFIG_FILE: "/dev/null",
      AWS_SHARED_CREDENTIALS_FILE: "/dev/null",
      AWS_SDK_LOAD_CONFIG: "0",
    };
  }
  if (ANTHROPIC_BASE_URL) {
    return { ...base, ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL };
  }
  return { ...base, ANTHROPIC_API_KEY };
}

export const MODEL = resolveModel();
