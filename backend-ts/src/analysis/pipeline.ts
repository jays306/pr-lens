import { triage } from "./triage.js";
import { runSpecialist } from "./specialist.js";
import { analyzeWithAgentSDK } from "../providers/agent.js";
import { summarySystemPrompt } from "./prompt.js";
import type { PRFile, StreamEvent, ExistingComment, TriageResult, Emit } from "./types.js";
import { makeAnthropicClient, MODEL } from "../providers/anthropic-client.js";

function aboveThreshold(files: PRFile[], diff: string): boolean {
  if (files.length >= 2) return true;
  return diff.split("\n").length >= 50;
}

function first2000Lines(s: string): string {
  const lines = s.split("\n");
  if (lines.length <= 2000) return s;
  return lines.slice(0, 2000).join("\n") + "\n[truncated for summary]";
}

/** Run risk/summary/systems call (non-agent, no cwd needed). */
async function runSummaryCall(
  apiKey: string,
  triageResult: TriageResult,
  condensedDiff: string,
): Promise<StreamEvent[]> {
  void apiKey;
  const client = makeAnthropicClient();

  let prompt = "# Triage classification\n\n";
  for (const [cat, files] of Object.entries(triageResult)) {
    prompt += `- **${cat}**: ${files.join(", ")}\n`;
  }
  prompt += `\n# Diff (first 2000 lines)\n\n\`\`\`diff\n${condensedDiff}\n\`\`\`\n`;

  const stream = await client.messages.create({
    model: MODEL,
    max_tokens: 4096,
    system: summarySystemPrompt(),
    messages: [{ role: "user", content: prompt }],
    stream: true,
  });

  let buf = "";
  const events: StreamEvent[] = [];

  for await (const chunk of stream) {
    if (chunk.type === "content_block_delta" && chunk.delta.type === "text_delta") {
      buf += chunk.delta.text;
      const lines = buf.split("\n");
      buf = lines.pop() ?? "";
      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) continue;
        try {
          const ev = JSON.parse(trimmed) as StreamEvent;
          if (["risk", "summary", "systems"].includes(ev.type)) events.push(ev);
        } catch { /* ignore */ }
      }
    }
  }
  return events;
}

async function runRecommendationCall(
  apiKey: string,
  categoryEvents: StreamEvent[],
): Promise<StreamEvent[]> {
  void apiKey;
  const client = makeAnthropicClient();

  let prompt = "Based on the following category analysis, provide a final recommendation.\n\n";
  for (const ev of categoryEvents) {
    const id = ev.data.id as string;
    const summary = ev.data.summary as string;
    const riskLevel = ev.data.riskLevel as string;
    prompt += `## ${id} (risk: ${riskLevel})\n${summary}\n\n`;
  }

  const sysPrompt = `You are PR-LENS. Based on the category analysis provided, emit exactly one JSON line:
{"type":"recommendation","data":{"action":"approve|request_changes|needs_review","reason":"<markdown: 2-3 sentences>"}}
Then emit: {"type":"done","data":{}}`;

  const msg = await client.messages.create({
    model: MODEL,
    max_tokens: 512,
    system: sysPrompt,
    messages: [{ role: "user", content: prompt }],
  });

  const events: StreamEvent[] = [];
  for (const block of msg.content) {
    if (block.type !== "text") continue;
    for (const line of block.text.split("\n")) {
      try {
        const ev = JSON.parse(line.trim()) as StreamEvent;
        if (ev.type === "recommendation") events.push(ev);
      } catch { /* ignore */ }
    }
  }
  return events;
}

const RISK_RANK: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3 };

export async function runPipeline(
  apiKey: string,
  diff: string,
  prFiles: PRFile[],
  cloneDir: string,
  existingComments: ExistingComment[],
  emit: Emit,
): Promise<void> {
  if (!aboveThreshold(prFiles, diff)) {
    // Small PR: run single agent session
    await analyzeWithAgentSDK(diff, cloneDir, existingComments, emit);
    return;
  }

  // Stage 1: triage
  let triageResult: TriageResult;
  try {
    triageResult = await triage(apiKey, prFiles);
    console.log(`[pipeline] triage: ${Object.keys(triageResult).length} categories`);
  } catch (err) {
    console.error(`[pipeline] triage failed, using fallback: ${err}`);
    await analyzeWithAgentSDK(diff, cloneDir, existingComments, emit);
    return;
  }

  if (Object.keys(triageResult).length === 0) {
    await analyzeWithAgentSDK(diff, cloneDir, existingComments, emit);
    return;
  }

  // Stage 2: kick off summary + all specialists in parallel. Each specialist
  // emits its category event as soon as it finishes, so the frontend sees
  // categories populate progressively instead of all appearing at the end.
  const summaryStart = Date.now();
  const specialistStart = Date.now();

  const summaryPromise = runSummaryCall(apiKey, triageResult, first2000Lines(diff))
    .then(async (events) => {
      console.log(`[pipeline] summary done in ${Date.now() - summaryStart}ms`);
      for (const ev of events) await emit(ev);
    })
    .catch((err) => { console.error("[pipeline] summary error:", err); });

  const seenCategories = new Set<string>();
  const allCategoryEvents: StreamEvent[] = [];

  const specialistPromises = Object.entries(triageResult).map(([cat, files]) => {
    const t = Date.now();
    return runSpecialist(cat, files, diff, cloneDir, existingComments)
      .then(async (events) => {
        console.log(`[pipeline] specialist ${cat} done in ${Date.now() - t}ms (${files.length} files)`);
        for (const ev of events) {
          if (ev.type !== "category") continue;
          const id = ev.data.id as string;
          if (id && seenCategories.has(id)) continue;
          if (id) seenCategories.add(id);
          allCategoryEvents.push(ev);
          await emit(ev);
        }
      })
      .catch((err) => { console.error(`[pipeline] specialist ${cat} error:`, err); });
  });

  // Wait for summary + all specialists to complete (events already emitted).
  await Promise.all([summaryPromise, ...specialistPromises]);
  console.log(`[pipeline] all specialists done in ${Date.now() - specialistStart}ms`);

  // Recommendation (after all categories are known).
  if (allCategoryEvents.length > 0) {
    // Sort by risk so the recommendation call sees highest-risk categories first.
    const ranked = [...allCategoryEvents].sort((a, b) => {
      const ra = RISK_RANK[a.data.riskLevel as string] ?? 3;
      const rb = RISK_RANK[b.data.riskLevel as string] ?? 3;
      return ra - rb;
    });
    const recEvents = await runRecommendationCall(apiKey, ranked)
      .catch((err) => { console.error("[pipeline] recommendation error:", err); return [] as StreamEvent[]; });
    for (const ev of recEvents) await emit(ev);
  }
}
