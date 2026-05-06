import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, relative, sep } from "node:path";
import { promisify } from "node:util";
import { readdir, stat } from "node:fs/promises";
import type { FileContent } from "../analysis/types.js";

const execFileAsync = promisify(execFile);

const MAX_SINGLE_FILE_BYTES = 100_000;

export interface CloneResult {
  dir: string;
  cleanup: () => Promise<void>;
}

export async function shallowClone(
  token: string,
  owner: string,
  repo: string,
  headSHA: string,
): Promise<CloneResult> {
  const dir = await mkdtemp(join(tmpdir(), "pr-lens-clone-"));
  const cleanup = () => rm(dir, { recursive: true, force: true });

  const cloneURL = `https://x-access-token:${token}@github.com/${owner}/${repo}.git`;

  try {
    await execFileAsync("git", [
      "clone", "--depth=1", "--no-tags", "--single-branch", cloneURL, dir,
    ], { timeout: 60_000 });
  } catch (err) {
    await cleanup();
    const msg = String(err).replace(token, "<token>");
    throw new Error(`git clone failed: ${msg}`);
  }

  // Best-effort: checkout the exact SHA (handles fork PRs where HEAD may differ)
  await execFileAsync("git", ["-C", dir, "checkout", "--detach", headSHA], { timeout: 10_000 })
    .catch(() => { /* non-fatal */ });

  return { dir, cleanup };
}

function isBinary(data: Buffer): boolean {
  return data.includes(0);
}

interface FileEntry {
  path: string;
  size: number;
}

async function walkDir(dir: string): Promise<FileEntry[]> {
  const entries: FileEntry[] = [];

  async function walk(current: string): Promise<void> {
    const items = await readdir(current, { withFileTypes: true });
    await Promise.all(items.map(async (item) => {
      if (item.name === ".git") return;
      const full = join(current, item.name);
      if (item.isDirectory()) {
        await walk(full);
      } else if (item.isFile()) {
        const s = await stat(full).catch(() => null);
        if (s && s.size > 0 && s.size <= MAX_SINGLE_FILE_BYTES) {
          entries.push({ path: full, size: s.size });
        }
      }
    }));
  }

  await walk(dir);
  return entries;
}

export async function readAllFiles(dir: string, maxTotalBytes: number): Promise<FileContent[]> {
  const entries = await walkDir(dir);
  // Sort smallest-first to maximise file count within budget
  entries.sort((a, b) => a.size - b.size);

  const out: FileContent[] = [];
  let total = 0;
  const prefix = dir + sep;

  for (const entry of entries) {
    if (total >= maxTotalBytes) break;
    const data = await readFile(entry.path).catch(() => null);
    if (!data || isBinary(data)) continue;
    const relPath = entry.path.startsWith(prefix)
      ? entry.path.slice(prefix.length)
      : relative(dir, entry.path);
    total += data.length;
    out.push({ path: relPath, content: data.toString("utf8") });
  }

  return out;
}
