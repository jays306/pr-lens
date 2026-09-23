import { describe, expect, test } from "bun:test";
import { escapeHtml, isRiskLevel, normalizeReviewQuestions } from "./sanitize";

describe("escapeHtml", () => {
  test("escapes markup", () => {
    expect(escapeHtml(`<img src=x onerror="alert(1)">`)).toBe(
      "&lt;img src=x onerror=&quot;alert(1)&quot;&gt;",
    );
  });
});

describe("normalizeReviewQuestions", () => {
  test("accepts string items", () => {
    expect(normalizeReviewQuestions(["Have you considered X?"])).toEqual([
      { text: "Have you considered X?" },
    ]);
  });

  test("accepts objects and drops invalid lines", () => {
    expect(
      normalizeReviewQuestions([
        { text: "Why?", file: "a.go", lineStart: 12, side: "RIGHT" },
        { text: "Skip", file: "a.go", lineStart: 0 },
        { text: "" },
        42,
      ]),
    ).toEqual([
      { text: "Why?", file: "a.go", lineStart: 12, side: "RIGHT" },
      { text: "Skip", file: "a.go", lineStart: undefined, side: undefined },
    ]);
  });

  test("returns empty for non-arrays", () => {
    expect(normalizeReviewQuestions(null)).toEqual([]);
  });
});

describe("isRiskLevel", () => {
  test("accepts known levels only", () => {
    expect(isRiskLevel("high")).toBe(true);
    expect(isRiskLevel("nope")).toBe(false);
  });
});
