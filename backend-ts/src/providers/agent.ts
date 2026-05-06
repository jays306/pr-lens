import { query } from "@anthropic-ai/claude-agent-sdk";
import type { StreamEvent, Emit, ExistingComment } from "../analysis/types.js";
import { systemPrompt, userPrompt } from "../analysis/prompt.js";

const MODEL = process.env.ANTHROPIC_MODEL ?? "claude-sonnet-4-6";

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

export async function analyzeWithAgentSDK(
  diff: string,
  cloneDir: string,
  existingComments: ExistingComment[],
  emit: Emit,
): Promise<void> {
  const prompt = userPrompt(diff, existingComments);
  const buf = { value: "" };

  const stream = query({
    prompt,
    options: {
      cwd: cloneDir,
      allowedTools: ["Read", "Grep", "Glob"],
      systemPrompt: systemPrompt(),
      model: MODEL,
      maxTurns: 25,
      permissionMode: "bypassPermissions",
      settingSources: [],
    },
  });

  for await (const msg of stream) {
    if (msg.type === "assistant") {
      const content = (msg.message as { content?: Array<{ type: string; text?: string }> })?.content;
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

/** Specialist variant: same agent but with filtered diff and specialist system prompt. */
export async function analyzeSpecialistWithAgentSDK(
  categoryID: string,
  filteredDiff: string,
  cloneDir: string,
  existingComments: ExistingComment[],
  specialistSysPrompt: string,
  emit: Emit,
): Promise<void> {
  const prompt = userPrompt(filteredDiff, existingComments);
  const buf = { value: "" };

  const stream = query({
    prompt,
    options: {
      cwd: cloneDir,
      allowedTools: ["Read", "Grep", "Glob"],
      systemPrompt: specialistSysPrompt,
      model: MODEL,
      maxTurns: 15,
      permissionMode: "bypassPermissions",
      settingSources: [],
    },
  });

  for await (const msg of stream) {
    if (msg.type === "assistant") {
      const content = (msg.message as { content?: Array<{ type: string; text?: string }> })?.content;
      if (content) {
        for (const block of content) {
          if (block.type === "text" && block.text) {
            parseAndEmit(block.text, buf, emit);
          }
        }
      }
    }
  }
}
