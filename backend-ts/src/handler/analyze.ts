import type { Context } from "hono";
import { streamSSE } from "hono/streaming";
import { GitHubClient, parsePRURL } from "../github/client.js";
import { shallowClone } from "../gitclone/clone.js";
import { Cache, cacheKey } from "../cache/cache.js";
import { runPipeline } from "../analysis/pipeline.js";
import { analyzeWithAgentSDK } from "../providers/agent.js";
import { resolveGitHubToken } from "../token.js";
import type { StreamEvent, ExistingComment } from "../analysis/types.js";

const ANTHROPIC_API_KEY = process.env.ANTHROPIC_API_KEY ?? "";
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

      // Fetch diff, PR info, files, and comments concurrently
      const [diffResult, prInfoResult, commentsResult] = await Promise.allSettled([
        ghClient.fetchDiff(ref),
        ghClient.fetchPRInfo(ref),
        ghClient.fetchReviewComments(ref),
      ]);

      if (diffResult.status === "rejected") {
        await emit({ type: "error", data: { message: `Failed to fetch diff: ${diffResult.reason}` } });
        return;
      }

      const diff = diffResult.value;
      const prInfo = prInfoResult.status === "fulfilled" ? prInfoResult.value : null;
      const existingComments: ExistingComment[] =
        commentsResult.status === "fulfilled" ? commentsResult.value : [];

      // Emit existing comments before AI results
      if (existingComments.length > 0) {
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
          const result = await shallowClone(token, ref.owner, ref.repo, prInfo.head.sha);
          cloneDir = result.dir;
          cleanup = result.cleanup;
          console.log(`[analyze] shallow clone done in ${Date.now() - t}ms cwd=${cloneDir}`);
        } catch (err) {
          console.warn(`[analyze] shallow clone failed (non-fatal): ${err}`);
        }
      }

      // If clone failed or disabled, use a temp dir that just has a placeholder
      // (Agent SDK will still work, just without repo context)
      const cwd = cloneDir ?? process.cwd();

      try {
        const collected: StreamEvent[] = [];
        const collectingEmit = async (ev: StreamEvent) => {
          collected.push(ev);
          await emit(ev);
        };

        const prFilesResult = await ghClient.fetchPRFiles(ref).catch(() => []);

        if (ANTHROPIC_API_KEY && prFilesResult.length > 0) {
          await runPipeline(
            ANTHROPIC_API_KEY,
            diff,
            prFilesResult,
            cwd,
            existingComments,
            collectingEmit,
          );
        } else {
          await analyzeWithAgentSDK(diff, cwd, existingComments, collectingEmit);
        }

        cache.set(key, collected);
        await emit({ type: "done", data: {} });
        console.log(`[analyze] done total=${Date.now() - start}ms`);
      } finally {
        if (cleanup) await cleanup().catch(() => {});
      }
    });
  };
}
