import type { Context } from "hono";
import { GitHubClient, parsePRURL } from "../github/client.js";
import { resolveGitHubToken } from "../token.js";

const VALID_EVENTS = new Set(["APPROVE", "REQUEST_CHANGES", "COMMENT"]);

export async function handleReview(c: Context, defaultToken: string): Promise<Response> {
  const token = resolveGitHubToken(c, defaultToken);
  if (!token) return c.json({ error: "GitHub token required" }, 401);

  let body: {
    url?: string;
    event?: string;
    body?: string;
    comments?: Array<{ path: string; line: number; body: string }>;
  };
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON body" }, 400);
  }

  if (!body.url) return c.json({ error: "url is required" }, 400);
  if (!body.event || !VALID_EVENTS.has(body.event)) {
    return c.json({ error: "event must be one of APPROVE, REQUEST_CHANGES, COMMENT" }, 400);
  }

  let ref;
  try {
    ref = parsePRURL(body.url);
  } catch (err) {
    return c.json({ error: String(err) }, 400);
  }

  const t = Date.now();
  const commentCount = body.comments?.length ?? 0;
  console.log(`[review] start ${ref.owner}/${ref.repo} #${ref.number} ` +
    `event=${body.event} inline=${commentCount}`);

  const ghClient = new GitHubClient(token);
  try {
    await ghClient.postReview(ref, body.event, body.body ?? "", body.comments ?? []);
  } catch (err) {
    console.error(`[review] error: ${err}`);
    return c.json({ error: `failed to post review: ${err}` }, 502);
  }

  console.log(`[review] posted in ${Date.now() - t}ms`);
  return c.json({ ok: true });
}
