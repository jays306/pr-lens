import { marked } from "marked";
import DOMPurify from "dompurify";
import type { ReviewQuestion } from "./types";

marked.setOptions({ gfm: true, breaks: false });

export function escapeHtml(str: string): string {
  return String(str ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

export function md(text: string): string {
  if (!text) return "";
  const html = marked.parse(text, { async: false }) as string;
  return DOMPurify.sanitize(html, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ["style", "script", "iframe", "object", "embed", "form", "link", "meta"],
    FORBID_ATTR: ["style"],
  });
}

export function normalizeReviewQuestions(raw: unknown): ReviewQuestion[] {
  if (!Array.isArray(raw)) return [];
  const out: ReviewQuestion[] = [];
  for (const q of raw) {
    if (typeof q === "string" && q.trim()) {
      out.push({ text: q.trim() });
      continue;
    }
    if (q && typeof q === "object" && typeof (q as ReviewQuestion).text === "string") {
      const item = q as ReviewQuestion;
      const text = item.text.trim();
      if (!text) continue;
      const lineStart = typeof item.lineStart === "number" && item.lineStart > 0 ? item.lineStart : undefined;
      const file = typeof item.file === "string" && item.file.trim() ? item.file : undefined;
      const side = item.side === "LEFT" || item.side === "RIGHT" ? item.side : undefined;
      out.push({ text, file, lineStart, side });
    }
  }
  return out;
}

export function isRiskLevel(value: unknown): value is "low" | "medium" | "high" | "critical" {
  return value === "low" || value === "medium" || value === "high" || value === "critical";
}
