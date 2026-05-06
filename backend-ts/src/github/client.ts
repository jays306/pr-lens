import type { PRRef, PRFile, PRInfo, FileContent, ExistingComment } from "../analysis/types.js";

export function parsePRURL(url: string): PRRef {
  const m = url.match(/github\.com\/([^/]+)\/([^/]+)\/pull\/(\d+)/);
  if (!m) throw new Error(`Not a recognized GitHub PR URL: ${url}`);
  return { owner: m[1], repo: m[2], number: parseInt(m[3], 10) };
}

export class GitHubClient {
  constructor(private token: string) {}

  private async req(url: string, accept: string): Promise<Response> {
    const res = await fetch(url, {
      headers: {
        Authorization: `Bearer ${this.token}`,
        Accept: accept,
        "X-GitHub-Api-Version": "2022-11-28",
      },
    });
    return res;
  }

  async fetchDiff(ref: PRRef): Promise<string> {
    const t = Date.now();
    const url = `https://api.github.com/repos/${ref.owner}/${ref.repo}/pulls/${ref.number}`;
    const res = await this.req(url, "application/vnd.github.v3.diff");
    if (!res.ok) {
      const body = await res.text();
      throw new Error(`GitHub API ${res.status}: ${body.trim()}`);
    }
    const diff = await res.text();
    console.log(`[github] fetchDiff ${ref.owner}/${ref.repo}#${ref.number} ${Date.now() - t}ms (${diff.length} chars)`);
    return diff;
  }

  async fetchPRInfo(ref: PRRef): Promise<PRInfo> {
    const t = Date.now();
    const url = `https://api.github.com/repos/${ref.owner}/${ref.repo}/pulls/${ref.number}`;
    const res = await this.req(url, "application/vnd.github+json");
    if (!res.ok) throw new Error(`GitHub API ${res.status}`);
    const info = (await res.json()) as PRInfo;
    console.log(`[github] fetchPRInfo ${ref.owner}/${ref.repo}#${ref.number} ${Date.now() - t}ms ` +
      `(head=${info.head?.sha?.slice(0, 8)})`);
    return info;
  }

  async fetchPRFiles(ref: PRRef): Promise<PRFile[]> {
    const t = Date.now();
    const all: PRFile[] = [];
    let pages = 0;
    for (let page = 1; ; page++) {
      const url = `https://api.github.com/repos/${ref.owner}/${ref.repo}/pulls/${ref.number}/files?per_page=100&page=${page}`;
      const res = await this.req(url, "application/vnd.github+json");
      if (!res.ok) throw new Error(`GitHub API ${res.status}`);
      const batch: PRFile[] = await res.json();
      pages++;
      all.push(...batch);
      if (batch.length < 100) break;
    }
    console.log(`[github] fetchPRFiles ${ref.owner}/${ref.repo}#${ref.number} ${Date.now() - t}ms ` +
      `(${all.length} files, ${pages} pages)`);
    return all;
  }

  async fetchFileContents(ref: PRRef, sha: string, filenames: string[]): Promise<FileContent[]> {
    const MAX_CONCURRENCY = 10;
    const MAX_FILE_SIZE = 50_000;
    const results: (FileContent | null)[] = new Array(filenames.length).fill(null);

    async function fetchOne(idx: number, fname: string, token: string): Promise<void> {
      const url = `https://api.github.com/repos/${ref.owner}/${ref.repo}/contents/${fname}?ref=${sha}`;
      const res = await fetch(url, {
        headers: {
          Authorization: `Bearer ${token}`,
          Accept: "application/vnd.github.raw+json",
          "X-GitHub-Api-Version": "2022-11-28",
        },
      });
      if (!res.ok) return;
      const text = await res.text();
      if (text.length <= MAX_FILE_SIZE) {
        results[idx] = { path: fname, content: text };
      }
    }

    // Process in batches of MAX_CONCURRENCY
    for (let i = 0; i < filenames.length; i += MAX_CONCURRENCY) {
      const batch = filenames.slice(i, i + MAX_CONCURRENCY);
      await Promise.all(batch.map((f, j) => fetchOne(i + j, f, this.token)));
    }

    return results.filter((r): r is FileContent => r !== null);
  }

  async fetchReviewComments(ref: PRRef): Promise<ExistingComment[]> {
    const t = Date.now();
    const [inline, reviews, issueComments] = await Promise.all([
      this.fetchInlineReviewComments(ref),
      this.fetchReviewBodies(ref),
      this.fetchIssueComments(ref),
    ]);
    console.log(`[github] fetchReviewComments ${ref.owner}/${ref.repo}#${ref.number} ${Date.now() - t}ms ` +
      `(inline=${inline.length}, reviews=${reviews.length}, issue=${issueComments.length})`);
    return [...inline, ...reviews, ...issueComments];
  }

  private async fetchInlineReviewComments(ref: PRRef): Promise<ExistingComment[]> {
    const all: ExistingComment[] = [];
    for (let page = 1; ; page++) {
      const url = `https://api.github.com/repos/${ref.owner}/${ref.repo}/pulls/${ref.number}/comments?per_page=100&page=${page}`;
      const res = await this.req(url, "application/vnd.github+json");
      if (!res.ok) break;
      const batch: Array<{
        id: number; path: string; line?: number; original_line?: number;
        body: string; user: { login: string };
      }> = await res.json();
      for (const c of batch) {
        const line = c.line ?? 0;
        const originalLine = c.original_line ?? 0;
        all.push({
          id: c.id, path: c.path, line, originalLine,
          author: c.user.login, body: c.body,
          anchor: "inline", source: "review_comment",
          outdated: line === 0 && originalLine > 0,
        });
      }
      if (batch.length < 100) break;
    }

    const resolvedIDs = await this.fetchResolvedThreadIDs(ref).catch(() => new Set<number>());
    for (const c of all) {
      if (c.id && resolvedIDs.has(c.id)) c.resolved = true;
    }
    return all;
  }

  private async fetchReviewBodies(ref: PRRef): Promise<ExistingComment[]> {
    const all: ExistingComment[] = [];
    for (let page = 1; ; page++) {
      const url = `https://api.github.com/repos/${ref.owner}/${ref.repo}/pulls/${ref.number}/reviews?per_page=100&page=${page}`;
      const res = await this.req(url, "application/vnd.github+json");
      if (!res.ok) break;
      const batch: Array<{ body: string; user: { login: string } }> = await res.json();
      for (const r of batch) {
        if (r.body.trim()) {
          all.push({ author: r.user.login, body: r.body, anchor: "pr", source: "review" });
        }
      }
      if (batch.length < 100) break;
    }
    return all;
  }

  private async fetchIssueComments(ref: PRRef): Promise<ExistingComment[]> {
    const all: ExistingComment[] = [];
    for (let page = 1; ; page++) {
      const url = `https://api.github.com/repos/${ref.owner}/${ref.repo}/issues/${ref.number}/comments?per_page=100&page=${page}`;
      const res = await this.req(url, "application/vnd.github+json");
      if (!res.ok) break;
      const batch: Array<{ body: string; user: { login: string } }> = await res.json();
      for (const c of batch) {
        if (c.body.trim()) {
          all.push({ author: c.user.login, body: c.body, anchor: "pr", source: "issue_comment" });
        }
      }
      if (batch.length < 100) break;
    }
    return all;
  }

  private async fetchResolvedThreadIDs(ref: PRRef): Promise<Set<number>> {
    const query = `query($owner: String!, $repo: String!, $number: Int!) {
      repository(owner: $owner, name: $repo) {
        pullRequest(number: $number) {
          reviewThreads(first: 100) {
            nodes {
              isResolved
              comments(first: 100) { nodes { databaseId } }
            }
          }
        }
      }
    }`;
    const res = await fetch("https://api.github.com/graphql", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${this.token}`,
        "Content-Type": "application/json",
        "X-GitHub-Api-Version": "2022-11-28",
      },
      body: JSON.stringify({ query, variables: { owner: ref.owner, repo: ref.repo, number: ref.number } }),
    });
    if (!res.ok) return new Set();
    const data: {
      data: {
        repository: {
          pullRequest: {
            reviewThreads: {
              nodes: Array<{
                isResolved: boolean;
                comments: { nodes: Array<{ databaseId: number }> };
              }>;
            };
          };
        };
      };
    } = await res.json();
    const ids = new Set<number>();
    for (const thread of data.data.repository.pullRequest.reviewThreads.nodes) {
      if (thread.isResolved) {
        for (const c of thread.comments.nodes) ids.add(c.databaseId);
      }
    }
    return ids;
  }

  async postReview(
    ref: PRRef,
    event: string,
    body: string,
    comments: Array<{ path: string; line: number; body: string }>,
  ): Promise<void> {
    const t = Date.now();
    const validComments = comments.filter((c) => c.path && c.body);
    const url = `https://api.github.com/repos/${ref.owner}/${ref.repo}/pulls/${ref.number}/reviews`;
    const res = await fetch(url, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${this.token}`,
        Accept: "application/vnd.github+json",
        "Content-Type": "application/json",
        "X-GitHub-Api-Version": "2022-11-28",
      },
      body: JSON.stringify({
        body, event,
        comments: validComments.map((c) => ({ path: c.path, line: c.line, body: c.body, side: "RIGHT" })),
      }),
    });
    if (!res.ok) {
      const text = await res.text();
      console.error(`[github] postReview ${ref.owner}/${ref.repo}#${ref.number} FAILED ${res.status}: ${text.trim()}`);
      throw new Error(`GitHub API ${res.status}: ${text.trim()}`);
    }
    console.log(`[github] postReview ${ref.owner}/${ref.repo}#${ref.number} ${Date.now() - t}ms ` +
      `(event=${event}, inline=${validComments.length})`);
  }
}
