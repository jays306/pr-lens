export function filterDiffByFiles(diff: string, filenames: string[]): string {
  if (!diff || filenames.length === 0) return "";

  const wanted = new Set(filenames);
  const out: string[] = [];
  let current: string[] = [];
  let currentFile = "";

  const flush = () => {
    if (currentFile && wanted.has(currentFile)) {
      out.push(...current);
    }
    current = [];
    currentFile = "";
  };

  for (const line of diff.split("\n")) {
    if (line.startsWith("diff --git ")) {
      flush();
      const parts = line.split(" ");
      if (parts.length >= 4) {
        currentFile = parts[3].replace(/^b\//, "");
      }
    }
    current.push(line);
  }
  flush();

  return out.join("\n");
}

/**
 * Parse unified-diff hunk headers and build a per-file list of hunk starts
 * (the `+` side start line from `@@ -x,y +a,b @@`).
 */
export function hunksByFile(diff: string): Record<string, Array<{ start: number; afterSnippet: string }>> {
  if (!diff) return {};
  const result: Record<string, Array<{ start: number; afterSnippet: string }>> = {};
  let currentFile = "";
  let currentHunks: Array<{ start: number; afterSnippet: string }> = [];
  let currentHunkStart = 0;
  let currentHunkAfter: string[] = [];

  const flushHunk = () => {
    if (currentHunkStart > 0) {
      currentHunks.push({ start: currentHunkStart, afterSnippet: currentHunkAfter.join("\n") });
    }
    currentHunkStart = 0;
    currentHunkAfter = [];
  };

  const flushFile = () => {
    flushHunk();
    if (currentFile && currentHunks.length > 0) {
      result[currentFile] = currentHunks;
    }
    currentHunks = [];
    currentFile = "";
  };

  for (const line of diff.split("\n")) {
    if (line.startsWith("diff --git ")) {
      flushFile();
      const parts = line.split(" ");
      if (parts.length >= 4) currentFile = parts[3].replace(/^b\//, "");
    } else if (line.startsWith("@@")) {
      flushHunk();
      // @@ -orig,n +new,m @@  — we only care about +new
      const m = line.match(/\+(\d+)/);
      if (m) currentHunkStart = parseInt(m[1], 10);
    } else if (line.startsWith("+") && !line.startsWith("+++")) {
      currentHunkAfter.push(line.slice(1));
    }
  }
  flushFile();
  return result;
}

/**
 * Given a snippet's `after` text and the file's hunks, find the best-matching
 * hunk start line. Returns 0 if no match can be found.
 */
export function inferLineStart(
  afterText: string,
  hunks: Array<{ start: number; afterSnippet: string }>,
): number {
  if (!afterText || hunks.length === 0) return 0;

  // Match on the first non-empty added line — most stable identifier
  const firstLine = afterText.split("\n").map((l) => l.trim()).find((l) => l.length > 0);
  if (!firstLine) return hunks[0].start;

  for (const h of hunks) {
    if (h.afterSnippet.includes(firstLine)) return h.start;
  }
  return hunks[0].start;
}
