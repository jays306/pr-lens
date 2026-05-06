import type { Context } from "hono";

export function resolveGitHubToken(c: Context, defaultToken: string): string {
  // Extension sends token in X-GitHub-Token or Authorization: Bearer <token>
  const xToken = c.req.header("x-github-token");
  if (xToken) return xToken;

  const auth = c.req.header("authorization");
  if (auth?.toLowerCase().startsWith("bearer ")) {
    return auth.slice(7).trim();
  }

  return defaultToken;
}
