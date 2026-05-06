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
