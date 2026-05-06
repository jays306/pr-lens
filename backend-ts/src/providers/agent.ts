import { query } from "@anthropic-ai/claude-agent-sdk";
import type { StreamEvent, Emit, ExistingComment } from "../analysis/types.js";
import { systemPrompt, userPrompt } from "../analysis/prompt.js";
import { agentEnv, MODEL } from "./anthropic-client.js";

const DEFAULT_MAX_TURNS = Number(process.env.AGENT_MAX_TURNS ?? 40);
const SPECIALIST_MAX_TURNS = Number(process.env.AGENT_SPECIALIST_MAX_TURNS ?? 30);

/** Parse newline-delimited JSON lines from a text chunk and emit valid events. */
export function parseAndEmit(text: string, buf: { value: string }, emit: Emit): void {
  buf.value += text;
  const lines = buf.value.split("\n");
  buf.value = lines.pop() ?? "";
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      const ev = JSON.parse(trimmed) as StreamEvent;
      if (ev.type && ev.data !== undefined) emit(ev);
    } catch {
      // not a JSON event line — ignore
    }
  }
}

/**
 * Extract incremental text from a partial (stream_event) SDK message.
 * These arrive as raw Anthropic API streaming events; we only care about text deltas.
 */
function extractPartialText(msg: unknown): string | null {
  if (typeof msg !== "object" || msg === null) return null;
  const m = msg as {
    type?: string;
    event?: { type?: string; delta?: { type?: string; text?: string } };
  };
  if (m.type !== "stream_event" || !m.event) return null;
  if (m.event.type !== "content_block_delta") return null;
  if (m.event.delta?.type !== "text_delta") return null;
  return m.event.delta.text ?? null;
}

async function runAgentQuery(
  prompt: string,
  sysPrompt: string,
  cloneDir: string,
  maxTurns: number,
  emit: Emit,
): Promise<void> {
  const buf = { value: "" };
  let sawPartial = false;

  const stream = query({
    prompt,
    options: {
      cwd: cloneDir,
      allowedTools: ["Read", "Grep", "Glob"],
      systemPrompt: sysPrompt,
      model: MODEL,
      maxTurns,
      maxThinkingTokens: 0,
      permissionMode: "bypassPermissions",
      settingSources: [],
      includePartialMessages: true,
      env: agentEnv(),
    },
  });

  for await (const msg of stream) {
    // Real-time text deltas as the model generates them
    const delta = extractPartialText(msg);
    if (delta !== null) {
      sawPartial = true;
      parseAndEmit(delta, buf, emit);
      continue;
    }

    // Fallback: if the backend didn't emit partials, parse completed turns
    if (!sawPartial && (msg as { type?: string }).type === "assistant") {
      const content = (msg as { message?: { content?: Array<{ type: string; text?: string }> } })
        .message?.content;
      if (content) {
        for (const block of content) {
          if (block.type === "text" && block.text) {
            parseAndEmit(block.text, buf, emit);
          }
        }
      }
    }
  }

  // Flush any remaining buffered content
  if (buf.value.trim()) {
    try {
      const ev = JSON.parse(buf.value.trim()) as StreamEvent;
      if (ev.type && ev.data !== undefined) await emit(ev);
    } catch {
      // ignore partial line
    }
  }
}

export async function analyzeWithAgentSDK(
  diff: string,
  cloneDir: string,
  existingComments: ExistingComment[],
  emit: Emit,
): Promise<void> {
  await runAgentQuery(
    userPrompt(diff, existingComments),
    systemPrompt(),
    cloneDir,
    DEFAULT_MAX_TURNS,
    emit,
  );
}

/** Specialist variant: filtered diff + specialist system prompt. */
export async function analyzeSpecialistWithAgentSDK(
  _categoryID: string,
  filteredDiff: string,
  cloneDir: string,
  existingComments: ExistingComment[],
  specialistSysPrompt: string,
  emit: Emit,
): Promise<void> {
  await runAgentQuery(
    userPrompt(filteredDiff, existingComments),
    specialistSysPrompt,
    cloneDir,
    SPECIALIST_MAX_TURNS,
    emit,
  );
}
