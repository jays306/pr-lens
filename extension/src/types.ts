export type RiskLevel = "low" | "medium" | "high" | "critical";

export interface PRCategory {
  id: string;
  icon: string;
  label: string;
  summary: string;
  snippets: CodeSnippet[];
  riskLevel: RiskLevel;
  reviewQuestions: string[];
  fileCount: number;
}

export interface CodeSnippet {
  file: string;
  language: string;
  before: string;       // removed lines (shown in left/red column)
  after: string;        // added lines (shown in right/green column)
  lineStart: number;
  explanation: string;  // why this change matters
}

export interface PRAnalysis {
  title: string;
  author: string;
  riskScore: number; // 0-100
  riskLevel: RiskLevel;
  summary: string;
  criticalityLabel: string;
  affectedSystems: string[];
  suggestedReviewOrder: string[];
  categories: PRCategory[];
  finalRecommendation?: "approve" | "request_changes" | "needs_review";
}

// SSE event types streamed from backend
export type SSEEvent =
  | { type: "meta"; data: { title: string; author: string } }
  | { type: "risk"; data: { score: number; level: RiskLevel; label: string } }
  | { type: "summary"; data: { text: string } }
  | { type: "systems"; data: { affected: string[]; reviewOrder: string[] } }
  | { type: "category"; data: PRCategory }
  | { type: "recommendation"; data: { action: "approve" | "request_changes" | "needs_review"; reason: string } }
  | { type: "done"; data: Record<string, never> }
  | { type: "error"; data: { message: string } };
