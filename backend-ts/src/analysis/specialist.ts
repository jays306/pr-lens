import { filterDiffByFiles } from "./diff.js";
import { specialistSystemPrompt } from "./prompt.js";
import { analyzeSpecialistWithAgentSDK } from "../providers/agent.js";
import type { StreamEvent, ExistingComment, Emit } from "./types.js";

export async function runSpecialist(
  categoryID: string,
  files: string[],
  diff: string,
  cloneDir: string,
  existingComments: ExistingComment[],
): Promise<StreamEvent[]> {
  const filteredDiff = filterDiffByFiles(diff, files);
  if (!filteredDiff.trim()) {
    return [{
      type: "category",
      data: {
        id: categoryID, icon: "?", label: categoryID,
        summary: "No changes found for this category.",
        riskLevel: "low", fileCount: 0, snippets: [], reviewQuestions: [],
      },
    }];
  }

  const fileSet = new Set(files);
  const filteredComments = existingComments.filter(
    (c) => c.path && fileSet.has(c.path) && c.anchor !== "pr",
  );

  const collected: StreamEvent[] = [];
  const emit: Emit = (ev) => { collected.push(ev); };

  await analyzeSpecialistWithAgentSDK(
    categoryID,
    filteredDiff,
    cloneDir,
    filteredComments,
    specialistSystemPrompt(categoryID),
    emit,
  );

  return collected.filter((ev) => ev.type === "category");
}
