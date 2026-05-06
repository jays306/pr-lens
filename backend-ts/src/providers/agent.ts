import { query } from "@anthropic-ai/claude-agent-sdk";
import type { StreamEvent, Emit, ExistingComment } from "../analysis/types.js";
import { systemPrompt, userPrompt } from "../analysis/prompt.js";
import { agentEnv, MODEL } from "./anthropic-client.js";

const DEFAULT_MAX_TURNS = Number(process.env.AGENT_MAX_TURNS ?? 40);
const SPECIALIST_MAX_TURNS = Number(process.env.AGENT_SPECIALIST_MAX_TURNS ?? 30);

/**
 * Parse JSON objects from a streaming text buffer and emit each complete one.
 *
 * The model is instructed to emit one JSON object per line, but may also
 * pretty-print multi-line JSON (especially for categories with multi-line
 * snippet strings). We scan for balanced `{...}` objects at the top level by
 * tracking brace depth and string state — this handles both one-per-line
 * and multi-line JSON without losing events.
 */
export function parseAndEmit(text: string, buf: { value: string }, emit: Emit): void {
  buf.value += text;
  const src = buf.value;

  let consumed = 0;
  let i = 0;
  while (i < src.length) {
    // Skip whitespace and any non-object junk between events
    while (i < src.length && src[i] !== "{") i++;
    if (i >= src.length) break;

    const start = i;
    let depth = 0;
    let inString = false;
    let escape = false;
    let end = -1;

    for (; i < src.length; i++) {
      const ch = src[i];
      if (escape) { escape = false; continue; }
      if (inString) {
        if (ch === "\\") { escape = true; continue; }
        if (ch === '"') { inString = false; continue; }
        continue;
      }
      if (ch === '"') { inString = true; continue; }
      if (ch === "{") depth++;
      else if (ch === "}") {
        depth--;
        if (depth === 0) { end = i + 1; break; }
      }
    }

    if (end === -1) {
      // Incomplete object — keep in buffer and wait for more chunks
      break;
    }

    const candidate = src.slice(start, end).trim();
    try {
      const ev = JSON.parse(candidate) as StreamEvent;
      if (ev.type && ev.data !== undefined) emit(ev);
    } catch (err) {
      // Not a valid JSON event — most commonly caused by the model
      // emitting raw newlines inside a string value. Log so we can
      // diagnose which events are being dropped.
      const preview = candidate.length > 200
        ? candidate.slice(0, 100) + "…" + candidate.slice(-80)
        : candidate;
      console.warn(`[agent] dropped invalid JSON event (${(err as Error).message}): ${preview}`);
    }
    consumed = end;
    i = end;
  }

  buf.value = src.slice(consumed);
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
  label: string = "agent",
): Promise<void> {
  const t = Date.now();
  const buf = { value: "" };
  let sawPartial = false;
  let toolUses = 0;
  let assistantTurns = 0;
  let emittedCount = 0;

  // Wrap emit to count how many events this run produced.
  const countingEmit: Emit = async (ev) => {
    emittedCount++;
    await emit(ev);
  };

  console.log(`[${label}] query start (model=${MODEL}, maxTurns=${maxTurns}, cwd=${cloneDir}, prompt=${prompt.length} chars)`);

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
      parseAndEmit(delta, buf, countingEmit);
      continue;
    }

    const msgType = (msg as { type?: string }).type;

    // Count tool-use blocks and assistant turns for visibility into how much
    // the agent explored the repo.
    if (msgType === "assistant") {
      assistantTurns++;
      const content = (msg as { message?: { content?: Array<{ type: string; text?: string; name?: string }> } })
        .message?.content;
      if (content) {
        for (const block of content) {
          if (block.type === "tool_use") {
            toolUses++;
            console.log(`[${label}] tool_use: ${block.name ?? "?"}`);
          }
        }
        if (!sawPartial) {
          // Fallback: parse completed turns if partials weren't emitted
          for (const block of content) {
            if (block.type === "text" && block.text) {
              parseAndEmit(block.text, buf, countingEmit);
            }
          }
        }
      }
    }
  }

  // Final flush: parseAndEmit handles any complete objects remaining in buf.
  // Anything left in buf.value after this is truly malformed/incomplete.
  parseAndEmit("", buf, countingEmit);

  console.log(`[${label}] query done in ${Date.now() - t}ms ` +
    `(turns=${assistantTurns}, tool_uses=${toolUses}, events=${emittedCount}, partial=${sawPartial})`);
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
    "agent-main",
  );
}

/** Specialist variant: filtered diff + specialist system prompt. */
export async function analyzeSpecialistWithAgentSDK(
  categoryID: string,
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
    `agent-${categoryID}`,
  );
}
