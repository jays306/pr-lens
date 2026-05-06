import type { Context } from "hono";
import { streamSSE } from "hono/streaming";
import { GitHubClient, parsePRURL } from "../github/client.js";
import { shallowClone } from "../gitclone/clone.js";
import { Cache, cacheKey } from "../cache/cache.js";
import { runPipeline } from "../analysis/pipeline.js";
import { analyzeWithAgentSDK } from "../providers/agent.js";
import { resolveGitHubToken } from "../token.js";
import { hunksByFile, inferLineStart } from "../analysis/diff.js";
import type { StreamEvent, ExistingComment } from "../analysis/types.js";

const ANTHROPIC_API_KEY = process.env.ANTHROPIC_API_KEY ?? "";
const USE_BEDROCK = process.env.CLAUDE_CODE_USE_BEDROCK === "1";
const HAS_AI_BACKEND = Boolean(ANTHROPIC_API_KEY || USE_BEDROCK);
const CLONE_REPOS = process.env.CLONE_REPOS !== "false"; // default true

export function makeAnalyzeHandler(cache: Cache, defaultToken: string) {
  return async (c: Context): Promise<Response> => {
    const token = resolveGitHubToken(c, defaultToken);
    if (!token) return c.json({ error: "GitHub token required" }, 401);

    let body: { url?: string };
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    if (!body.url) return c.json({ error: "url is required" }, 400);

    let ref;
    try {
      ref = parsePRURL(body.url);
    } catch (err) {
      return c.json({ error: String(err) }, 400);
    }

    const ghClient = new GitHubClient(token);

    return streamSSE(c, async (stream) => {
      const emit = async (ev: StreamEvent) => {
        await stream.writeSSE({ data: JSON.stringify(ev) });
      };

      const start = Date.now();
      console.log(`[analyze] start ${ref.owner}/${ref.repo} #${ref.number}`);

      // Fetch diff, PR info, and comments concurrently
      const ghFetchStart = Date.now();
      console.log(`[analyze] fetching diff + PR info + comments from GitHub…`);
      const [diffResult, prInfoResult, commentsResult] = await Promise.allSettled([
        ghClient.fetchDiff(ref),
        ghClient.fetchPRInfo(ref),
        ghClient.fetchReviewComments(ref),
      ]);
      console.log(`[analyze] GitHub fetch done in ${Date.now() - ghFetchStart}ms ` +
        `(diff=${diffResult.status}, info=${prInfoResult.status}, comments=${commentsResult.status})`);

      if (diffResult.status === "rejected") {
        console.error(`[analyze] fatal: fetchDiff failed: ${diffResult.reason}`);
        await emit({ type: "error", data: { message: `Failed to fetch diff: ${diffResult.reason}` } });
        return;
      }

      const diff = diffResult.value;
      const prInfo = prInfoResult.status === "fulfilled" ? prInfoResult.value : null;
      const existingComments: ExistingComment[] =
        commentsResult.status === "fulfilled" ? commentsResult.value : [];

      console.log(`[analyze] diff=${diff.length} chars, head=${prInfo?.head.sha?.slice(0, 8) ?? "?"}, ` +
        `existingComments=${existingComments.length}`);

      // Emit existing comments before AI results
      if (existingComments.length > 0) {
        console.log(`[analyze] emitting ${existingComments.length} existing comments`);
        await emit({
          type: "comments",
          data: {
            comments: existingComments.map((c) => ({
              path: c.path, line: c.line, originalLine: c.originalLine,
              author: c.author, body: c.body, anchor: c.anchor,
              source: c.source, resolved: c.resolved,
            })),
          },
        });
      }

      // Cache check
      const commentsMaterial = existingComments.length > 0
        ? JSON.stringify(existingComments)
        : "";
      const key = cacheKey(diff, commentsMaterial);
      const cached = cache.get(key);
      if (cached) {
        cache.logStats(key, true);
        console.log(`[analyze] replaying ${cached.length} cached events`);
        await cache.replay(cached, emit);
        await emit({ type: "done", data: {} });
        console.log(`[analyze] done (cache hit) total=${Date.now() - start}ms`);
        return;
      }
      cache.logStats(key, false);

      // Clone repo for context
      let cloneDir: string | null = null;
      let cleanup: (() => Promise<void>) | null = null;

      if (CLONE_REPOS && prInfo) {
        try {
          const t = Date.now();
          console.log(`[analyze] shallow cloning ${ref.owner}/${ref.repo}@${prInfo.head.sha.slice(0, 8)}…`);
          const result = await shallowClone(token, ref.owner, ref.repo, prInfo.head.sha);
          cloneDir = result.dir;
          cleanup = result.cleanup;
          console.log(`[analyze] shallow clone done in ${Date.now() - t}ms cwd=${cloneDir}`);
        } catch (err) {
          console.warn(`[analyze] shallow clone failed (non-fatal): ${err}`);
        }
      } else if (!CLONE_REPOS) {
        console.log(`[analyze] CLONE_REPOS disabled — agents will run without repo context`);
      } else {
        console.log(`[analyze] no PR info — skipping clone`);
      }

      // If clone failed or disabled, use a temp dir that just has a placeholder
      // (Agent SDK will still work, just without repo context)
      const cwd = cloneDir ?? process.cwd();

      // Pre-compute hunk ranges once so we can backfill missing lineStart
      // on snippets from any path (pipeline, specialist, or fallback).
      const hunkMap = hunksByFile(diff);

      try {
        const collected: StreamEvent[] = [];
        const collectingEmit = async (ev: StreamEvent) => {
          // Handler owns the final "done" event — suppress any upstream ones.
          if (ev.type === "done") return;
          const enriched = ev.type === "category" ? enrichCategory(ev, hunkMap) : ev;
          collected.push(enriched);
          await emit(enriched);
        };

        const filesFetchStart = Date.now();
        console.log(`[analyze] fetching PR files list…`);
        const prFilesResult = await ghClient.fetchPRFiles(ref).catch(() => []);
        console.log(`[analyze] PR files fetch done in ${Date.now() - filesFetchStart}ms ` +
          `(${prFilesResult.length} files)`);

        const aiStart = Date.now();
        if (HAS_AI_BACKEND && prFilesResult.length > 0) {
          console.log(`[analyze] starting pipeline (triage + specialists) for ${prFilesResult.length} files`);
          await runPipeline(
            ANTHROPIC_API_KEY,
            diff,
            prFilesResult,
            cwd,
            existingComments,
            collectingEmit,
          );
        } else {
          console.log(`[analyze] starting single-agent fallback ` +
            `(HAS_AI_BACKEND=${HAS_AI_BACKEND}, files=${prFilesResult.length})`);
          await analyzeWithAgentSDK(diff, cwd, existingComments, collectingEmit);
        }
        console.log(`[analyze] AI phase done in ${Date.now() - aiStart}ms ` +
          `(${collected.length} events emitted)`);

        cache.set(key, collected);
        console.log(`[analyze] cached ${collected.length} events under key=${key.slice(0, 20)}…`);
        await emit({ type: "done", data: {} });
        console.log(`[analyze] done total=${Date.now() - start}ms`);
      } finally {
        if (cleanup) {
          console.log(`[analyze] cleaning up clone dir ${cloneDir}`);
          await cleanup().catch(() => {});
        }
      }
    });
  };
}

/**
 * Backfill missing snippet fields that models sometimes omit:
 * - lineStart — infer from matching the snippet's `after` text against the
 *   diff's hunk headers for that file; prevents frontend "Lundefined" / NaN.
 * - riskLevel — default to the category's riskLevel.
 * - reviewQuestions.lineStart — copy from the first snippet with matching file.
 * Idempotent: already-valid values are preserved.
 */
function enrichCategory(
  ev: StreamEvent,
  hunkMap: Record<string, Array<{ start: number; afterSnippet: string }>>,
): StreamEvent {
  const data = ev.data as {
    snippets?: Array<Record<string, unknown>>;
    riskLevel?: string;
    reviewQuestions?: Array<Record<string, unknown>>;
  };
  const categoryRisk = typeof data.riskLevel === "string" ? data.riskLevel : "medium";

  const snippets = (data.snippets ?? []).map((s) => {
    const out = { ...s };
    const rawLine = out.lineStart;
    const lineNum =
      typeof rawLine === "number" && Number.isFinite(rawLine) ? rawLine : NaN;
    if (!Number.isFinite(lineNum) || lineNum <= 0) {
      const file = typeof out.file === "string" ? out.file : "";
      const after = typeof out.after === "string" ? out.after : "";
      const hunks = hunkMap[file] ?? [];
      const inferred = inferLineStart(after, hunks);
      out.lineStart = inferred > 0 ? inferred : 1;
    }
    if (typeof out.riskLevel !== "string") out.riskLevel = categoryRisk;
    return out;
  });

  const reviewQuestions = (data.reviewQuestions ?? []).map((q) => {
    const out = { ...q };
    const lineNum =
      typeof out.lineStart === "number" && Number.isFinite(out.lineStart)
        ? out.lineStart
        : NaN;
    if (!Number.isFinite(lineNum) || lineNum <= 0) {
      const file = typeof out.file === "string" ? out.file : "";
      const match = snippets.find((s) => s.file === file);
      if (match && typeof match.lineStart === "number") out.lineStart = match.lineStart;
    }
    return out;
  });

  return {
    type: ev.type,
    data: { ...data, snippets, reviewQuestions },
  };
}
