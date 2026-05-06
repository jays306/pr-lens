export interface StreamEvent {
  type: string;
  data: Record<string, unknown>;
}

export interface PRRef {
  owner: string;
  repo: string;
  number: number;
}

export interface PRFile {
  filename: string;
  status: "added" | "removed" | "modified" | "renamed";
  patch?: string;
}

export interface PRInfo {
  base: { sha: string; ref: string };
  head: { sha: string; ref: string };
}

export interface FileContent {
  path: string;
  content: string;
}

export interface ExistingComment {
  id?: number;
  path?: string;
  line?: number;
  originalLine?: number;
  author: string;
  body: string;
  anchor: "inline" | "file" | "pr";
  source: "review_comment" | "review" | "issue_comment";
  outdated?: boolean;
  resolved?: boolean;
}

export type Emit = (ev: StreamEvent) => void | Promise<void>;

export type TriageResult = Record<string, string[]>; // categoryID → filenames
