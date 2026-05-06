import Anthropic from "@anthropic-ai/sdk";

const AI_PROVIDER = process.env.AI_PROVIDER?.toLowerCase() ?? "claude";
const BEDROCK_API_KEY = process.env.BEDROCK_API_KEY ?? "";
const BEDROCK_REGION = process.env.BEDROCK_REGION ?? "us-east-1";

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

/** Resolve the model name appropriate for the configured provider. */
export function resolveModel(): string {
  const raw = process.env.ANTHROPIC_MODEL ?? "";
  if (AI_PROVIDER === "bedrock") {
    return bedrockModelID(raw || "anthropic.claude-sonnet-4-6");
  }
  // For direct Anthropic API or LiteLLM proxy: strip Bedrock prefixes
  let m = raw || "claude-sonnet-4-6";
  m = m.replace(/^(?:us|eu|ap)\./, "");
  m = m.replace(/^anthropic\./, "");
  return m;
}

/** Build an Anthropic SDK client for the configured provider. */
export function makeAnthropicClient(): Anthropic {
  if (AI_PROVIDER === "bedrock") {
    const endpoint = `https://bedrock-runtime.${BEDROCK_REGION}.amazonaws.com`;
    return new Anthropic({
      apiKey: "bedrock",            // placeholder; auth is via Bearer token below
      authToken: BEDROCK_API_KEY || null,
      baseURL: endpoint,
    });
  }
  // Default: direct Anthropic API or LiteLLM proxy via ANTHROPIC_BASE_URL
  const opts: ConstructorParameters<typeof Anthropic>[0] = {
    apiKey: process.env.ANTHROPIC_API_KEY ?? "",
  };
  const baseURL = process.env.ANTHROPIC_BASE_URL;
  if (baseURL) opts.baseURL = baseURL;
  return new Anthropic(opts);
}

/**
 * Environment variables to pass to the Agent SDK subprocess so it
 * connects to the same backend the main process uses.
 */
export function agentEnv(): Record<string, string> {
  if (AI_PROVIDER === "bedrock") {
    const endpoint = `https://bedrock-runtime.${BEDROCK_REGION}.amazonaws.com`;
    return {
      ANTHROPIC_API_KEY: BEDROCK_API_KEY || "bedrock",
      ANTHROPIC_BASE_URL: endpoint,
      ...(BEDROCK_API_KEY ? { BEDROCK_API_KEY } : {}),
    };
  }
  const env: Record<string, string> = {
    ANTHROPIC_API_KEY: process.env.ANTHROPIC_API_KEY ?? "",
  };
  if (process.env.ANTHROPIC_BASE_URL) {
    env.ANTHROPIC_BASE_URL = process.env.ANTHROPIC_BASE_URL;
  }
  return env;
}

export const MODEL = resolveModel();
