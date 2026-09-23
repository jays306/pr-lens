import type { SSEEvent, PRCategory, RiskLevel, CodeSnippet, ExistingComment } from "./types";
import { overlayCSS } from "./__generated_css";
import { escapeHtml, isRiskLevel, md, normalizeReviewQuestions } from "./sanitize";
import hljs from "highlight.js/lib/core";
import typescript from "highlight.js/lib/languages/typescript";
import javascript from "highlight.js/lib/languages/javascript";
import python from "highlight.js/lib/languages/python";
import go from "highlight.js/lib/languages/go";
import java from "highlight.js/lib/languages/java";
import kotlin from "highlight.js/lib/languages/kotlin";
import swift from "highlight.js/lib/languages/swift";
import ruby from "highlight.js/lib/languages/ruby";
import rust from "highlight.js/lib/languages/rust";
import cpp from "highlight.js/lib/languages/cpp";
import c from "highlight.js/lib/languages/c";
import scala from "highlight.js/lib/languages/scala";
import php from "highlight.js/lib/languages/php";
import protobuf from "highlight.js/lib/languages/protobuf";
import xml from "highlight.js/lib/languages/xml";
import bash from "highlight.js/lib/languages/bash";
import sql from "highlight.js/lib/languages/sql";
import css from "highlight.js/lib/languages/css";
import json from "highlight.js/lib/languages/json";
import yaml from "highlight.js/lib/languages/yaml";

hljs.registerLanguage("typescript", typescript);
hljs.registerLanguage("javascript", javascript);
hljs.registerLanguage("python", python);
hljs.registerLanguage("go", go);
hljs.registerLanguage("java", java);
hljs.registerLanguage("kotlin", kotlin);
hljs.registerLanguage("swift", swift);
hljs.registerLanguage("ruby", ruby);
hljs.registerLanguage("rust", rust);
hljs.registerLanguage("cpp", cpp);
hljs.registerLanguage("c", c);
hljs.registerLanguage("scala", scala);
hljs.registerLanguage("php", php);
hljs.registerLanguage("protobuf", protobuf);
hljs.registerLanguage("xml", xml);
hljs.registerLanguage("html", xml);
hljs.registerLanguage("bash", bash);
hljs.registerLanguage("shell", bash);
hljs.registerLanguage("sql", sql);
hljs.registerLanguage("css", css);
hljs.registerLanguage("json", json);
hljs.registerLanguage("yaml", yaml);

const DEFAULT_BACKEND_URL = "http://localhost:8080";
let BACKEND_URL = DEFAULT_BACKEND_URL;
/** GitHub PAT from extension storage; sent as Authorization Bearer on /analyze and /review. */
let GITHUB_TOKEN = "";

export function setGithubToken(token: string): void {
  GITHUB_TOKEN = token.trim();
}

/** Headers for backend JSON requests; includes Bearer token when configured. */
function backendHeaders(): HeadersInit {
  const h: Record<string, string> = { "Content-Type": "application/json" };
  if (GITHUB_TOKEN) {
    h["Authorization"] = `Bearer ${GITHUB_TOKEN}`;
  }
  return h;
}

interface AnalysisState {
  riskScore: number;
  riskLevel: RiskLevel;
  riskLabel: string;
  summary: string;
  affected: string[];
  reviewOrder: string[];
  categories: PRCategory[];
  existingComments: ExistingComment[];
  recommendation?: { action: "approve" | "request_changes" | "needs_review"; reason: string };
}

type Screen = "idle" | "loading" | "overview" | "step" | "decision";

interface FileTreeNode {
  name: string;
  path: string;
  isFile: boolean;
  fullPath?: string;
  children: Map<string, FileTreeNode>;
}

export function setBackendUrl(url: string): void {
  BACKEND_URL = url.replace(/\/$/, "") || DEFAULT_BACKEND_URL;
}

export class JustPROverlay {
  private host: HTMLElement;        // the shadow host in document.body
  private shadow: ShadowRoot;       // isolated shadow DOM
  private panel: HTMLElement;
  private toggle: HTMLElement;
  private screenEl!: HTMLElement;

  private isOpen = false;
  private isAnalyzing = false;
  private analysisGen = 0;
  private abort: AbortController | null = null;
  private prUrl: string;

  private state: AnalysisState = {
    riskScore: 0, riskLevel: "low", riskLabel: "",
    summary: "", affected: [], reviewOrder: [], categories: [], existingComments: [],
  };

  // Step navigation
  private currentStep = 0; // 0 = overview, 1..n = categories, n+1 = decision

  // Comments keyed by "catIdx:snippetIdx" or "catIdx:section"
  private comments: Map<string, string> = new Map();

  // PR-level review decision (replaces per-file approve/deny)
  private reviewDecision: "APPROVE" | "REQUEST_CHANGES" | "COMMENT" | null = null;

  // Feedback action states keyed by "catIdx:questionIdx"
  // noted = added to snippet note; post = post as standalone inline comment; dismissed = suppressed
  private feedbackActions: Map<string, "noted" | "post" | "dismissed"> = new Map();

  // Track whether this session already submitted (keyed by PR URL in sessionStorage)
  private get submittedKey() { return `pr-lens:submitted:${this.prUrl}`; }

  constructor(prUrl: string) {
    this.prUrl = prUrl;

    // Mount inside Shadow DOM so GitHub's CSS cannot leak in
    this.host = document.createElement("div");
    this.host.id = "pr-lens-host";
    this.host.style.cssText = "all:initial;position:fixed;top:0;left:0;z-index:2147483647;pointer-events:none;";
    this.shadow = this.host.attachShadow({ mode: "closed" });

    // Inject our stylesheet into the shadow root
    this.injectStyles();

    this.toggle = this.buildToggle();
    this.panel = this.buildPanel();

    this.shadow.appendChild(this.toggle);
    this.shadow.appendChild(this.panel);
    document.body.appendChild(this.host);

    this.renderScreen("idle");
  }

  private injectStyles(): void {
    const style = document.createElement("style");
    style.textContent = overlayCSS;
    this.shadow.appendChild(style);
  }

  // ── Panel skeleton ─────────────────────────────────────────

  private buildToggle(): HTMLElement {
    const btn = document.createElement("button");
    btn.id = "pr-lens-toggle";
    btn.textContent = "JUST·PR";
    btn.style.pointerEvents = "auto";
    btn.addEventListener("click", () => this.togglePanel());
    return btn;
  }

  private buildPanel(): HTMLElement {
    const panel = document.createElement("div");
    panel.id = "pr-lens-panel";
    panel.style.pointerEvents = "auto";

    // Resize handle
    const handle = document.createElement("div");
    handle.id = "pr-lens-resize-handle";
    panel.appendChild(handle);
    this.initResize(handle, panel);

    // Header
    const header = document.createElement("div");
    header.className = "jp-header";
    header.innerHTML = `
      <div class="jp-logo">PR-LENS <span class="jp-logo-sub">AI Review</span></div>
      <div class="jp-header-right">
        <button class="jp-btn-icon jp-btn-restart" title="Re-analyze latest PR state">↺</button>
        <button class="jp-btn-icon jp-btn-close" title="Close">✕</button>
      </div>
    `;
    panel.appendChild(header);

    header.querySelector(".jp-btn-close")!.addEventListener("click", () => this.togglePanel());
    header.querySelector(".jp-btn-restart")!.addEventListener("click", () => {
      if (!this.isAnalyzing) void this.startAnalysis();
    });

    // Screen container (swapped per state)
    this.screenEl = document.createElement("div");
    this.screenEl.className = "jp-screen";
    panel.appendChild(this.screenEl);

    return panel;
  }

  // ── Screen rendering ───────────────────────────────────────

  private renderScreen(screen: Screen): void {
    this.screenEl.innerHTML = "";

    try {
      switch (screen) {
        case "idle":     return this.renderIdle();
        case "loading":  return this.renderLoading();
        case "overview": return this.renderOverview();
        case "step":     return this.renderStep(this.currentStep - 1);
        case "decision": return this.renderDecision();
      }
    } catch (err) {
      const errEl = document.createElement("div");
      errEl.className = "jp-error";
      this.showError(err instanceof Error ? err.message : String(err), "Render Error");
    }
  }

  private renderIdle(): void {
    const el = document.createElement("div");
    el.className = "jp-idle";
    el.innerHTML = `
      <div class="jp-idle-badge">AI-Native Review</div>
      <div class="jp-idle-heading">Understand before<br>you read.</div>
      <div class="jp-idle-url">${escapeHtml(this.prUrl)}</div>
      <button class="jp-analyze-btn">Analyze Pull Request</button>
    `;
    el.querySelector(".jp-analyze-btn")!.addEventListener("click", () => this.startAnalysis());
    this.screenEl.appendChild(el);
  }

  private renderLoading(): void {
    const el = document.createElement("div");
    el.className = "jp-loading";
    el.innerHTML = `
      <div class="jp-loading-mark"><div class="jp-loading-ring"></div></div>
      <div class="jp-loading-pct">0%</div>
      <div class="jp-loading-phase">Connecting to backend</div>
      <div class="jp-loading-steps">
        <div class="jp-loading-step" data-phase="risk">Risk assessment</div>
        <div class="jp-loading-step" data-phase="summary">Summary</div>
        <div class="jp-loading-step" data-phase="systems">Affected systems</div>
        <div class="jp-loading-step" data-phase="categories">Analyzing files</div>
        <div class="jp-loading-step" data-phase="recommendation">Recommendation</div>
      </div>
      <div class="jp-loading-track">
        <div class="jp-loading-fill"></div>
      </div>
    `;
    this.screenEl.appendChild(el);
  }

  private updateLoadingProgress(phase: string, detail?: string): void {
    const pctMap: Record<string, number> = {
      connected: 5,
      risk: 15,
      summary: 35,
      systems: 50,
      category: 65, // will be interpolated
      recommendation: 90,
      done: 100,
    };

    const pct = pctMap[phase] ?? 50;
    const pctEl = this.screenEl.querySelector<HTMLElement>(".jp-loading-pct");
    const phaseEl = this.screenEl.querySelector<HTMLElement>(".jp-loading-phase");
    const fillEl = this.screenEl.querySelector<HTMLElement>(".jp-loading-fill");

    if (pctEl) pctEl.textContent = `${pct}%`;
    if (phaseEl) phaseEl.textContent = detail ?? phase;
    if (fillEl) fillEl.style.width = `${pct}%`;

    // Mark completed steps
    const phaseToStep: Record<string, string> = {
      risk: "risk", summary: "summary", systems: "systems",
      category: "categories", recommendation: "recommendation", done: "recommendation",
    };
    const stepPhase = phaseToStep[phase];
    if (stepPhase) {
      const steps = this.screenEl.querySelectorAll<HTMLElement>(".jp-loading-step");
      let reached = false;
      steps.forEach(s => {
        if (s.dataset.phase === stepPhase) {
          s.classList.add("jp-loading-step--active");
          reached = true;
        } else if (!reached) {
          s.classList.remove("jp-loading-step--active");
          s.classList.add("jp-loading-step--done");
        }
      });
    }
  }

  private renderOverview(): void {
    const s = this.state;
    const el = document.createElement("div");
    el.className = "jp-overview";

    const hero = document.createElement("div");
    hero.className = "jp-section jp-section--hero";
    hero.innerHTML = `
      <div class="jp-risk-hero">
        <div class="jp-risk-score" data-level="${escapeHtml(s.riskLevel)}">${escapeHtml(String(s.riskScore))}</div>
        <div class="jp-risk-aside">
          <div class="jp-risk-badge" data-level="${escapeHtml(s.riskLevel)}">
            <span class="jp-risk-badge-dot"></span>
            <span>${escapeHtml(s.riskLevel)}</span>
          </div>
          <div class="jp-risk-subtext">${escapeHtml(s.riskLabel || "Analyzing")}</div>
        </div>
      </div>
      <div class="jp-risk-track">
        <div class="jp-risk-fill" data-level="${escapeHtml(s.riskLevel)}" style="width:0%"></div>
      </div>
      <div class="jp-summary-text jp-md">${md(s.summary)}</div>
    `;
    el.appendChild(hero);
    requestAnimationFrame(() => {
      const fill = hero.querySelector<HTMLElement>(".jp-risk-fill");
      if (fill) fill.style.width = `${s.riskScore}%`;
    });

    const prComments = this.getPRDiscussionComments();
    if (prComments.length > 0) {
      const discussionSection = document.createElement("div");
      discussionSection.className = "jp-section";
      discussionSection.appendChild(this.buildExistingCommentsBlock("PR Discussion", prComments));
      el.appendChild(discussionSection);
    }

    // Review order / section list
    if (s.categories.length > 0) {
      const orderSection = document.createElement("div");
      orderSection.className = "jp-section";
      const items = s.categories.map((cat, i) => `
        <div class="jp-order-item" data-idx="${i}">
          <span class="jp-order-num">${String(i + 1).padStart(2, "0")}</span>
          <span class="jp-order-icon">${escapeHtml(cat.icon)}</span>
          <span class="jp-order-label">${escapeHtml(cat.label)}</span>
          <span class="jp-order-risk" data-level="${escapeHtml(cat.riskLevel)}">${escapeHtml(cat.riskLevel)}</span>
        </div>
      `).join("");
      orderSection.innerHTML = `
        <div class="jp-section-label">Review Sections</div>
        <div class="jp-order-list">${items}</div>
      `;
      orderSection.querySelectorAll<HTMLElement>(".jp-order-item").forEach(item => {
        item.addEventListener("click", () => {
          this.currentStep = parseInt(item.dataset.idx ?? "0") + 1;
          this.renderScreen("step");
        });
      });
      el.appendChild(orderSection);
    }

    // Spacer to push button to bottom
    const spacer = document.createElement("div");
    spacer.style.flex = "1";
    el.appendChild(spacer);

    // Start review button
    const footer = document.createElement("div");
    footer.className = "jp-start-btn-wrap";
    footer.innerHTML = `<button class="jp-start-btn"${s.categories.length === 0 ? " disabled" : ""}>Start Review <span class="jp-start-btn-count">· ${s.categories.length} sections</span></button>`;
    footer.querySelector(".jp-start-btn")!.addEventListener("click", () => {
      if (s.categories.length === 0) return;
      this.currentStep = 1;
      this.renderScreen("step");
    });
    el.appendChild(footer);

    this.screenEl.appendChild(el);
    this.updateProgressBar();
  }

  private renderStep(catIdx: number): void {
    const cat = this.state.categories[catIdx];
    if (!cat) return;

    const total = this.state.categories.length;
    const isLast = catIdx === total - 1;

    const el = document.createElement("div");
    el.className = "jp-step";

    // ── Slim section header ───────────────────────────────────────
    const header = document.createElement("div");
    header.className = "jp-step-header";
    header.innerHTML = `
      <div class="jp-step-header-left">
        <span class="jp-step-rail-dot" data-level="${escapeHtml(cat.riskLevel)}"></span>
        <span class="jp-step-icon">${escapeHtml(cat.icon)}</span>
        <span class="jp-step-title">${escapeHtml(cat.label)}</span>
        <span class="jp-step-risk-pill" data-level="${escapeHtml(cat.riskLevel)}">${escapeHtml(cat.riskLevel)}</span>
      </div>
      <div class="jp-step-header-nav">
        <button class="jp-step-nav-btn" data-action="prev">Back</button>
        <button class="jp-step-nav-btn" data-action="next"${isLast ? " disabled" : ""}>Next</button>
        <button class="jp-step-nav-btn jp-step-nav-btn--finish">Finish</button>
      </div>
    `;
    header.querySelector("[data-action='prev']")!.addEventListener("click", () => {
      this.currentStep = catIdx === 0 ? 0 : catIdx;
      this.renderScreen(catIdx === 0 ? "overview" : "step");
    });
    header.querySelector("[data-action='next']")!.addEventListener("click", () => {
      if (isLast) return;
      this.currentStep = catIdx + 2;
      this.renderScreen("step");
    });
    header.querySelector(".jp-step-nav-btn--finish")!.addEventListener("click", () => {
      this.currentStep = total + 1;
      this.renderScreen("decision");
    });
    el.appendChild(header);

    // ── 2-column body ─────────────────────────────────────────────
    const cols = document.createElement("div");
    cols.className = "jp-step-cols";

    // ── LEFT: file navigator ──────────────────────────────────────
    const leftCol = document.createElement("div");
    leftCol.className = "jp-step-col-files";

    // Group snippets by file
    const byFile = new Map<string, { snippetIdx: number; lineStart: number }[]>();
    cat.snippets.forEach((s, i) => {
      if (!byFile.has(s.file)) byFile.set(s.file, []);
      byFile.get(s.file)!.push({ snippetIdx: i, lineStart: s.lineStart });
    });
    const fileTree = this.buildFileTree([...byFile.keys()]);
    const treeRoot = document.createElement("div");
    treeRoot.className = "jp-file-tree";
    const snippetFeedbackBadges = new Map<number, HTMLElement>();

    const getPendingFeedbackCountForSnippet = (snippetIdx: number): number => {
      const snippet = cat.snippets[snippetIdx];
      if (!snippet) return 0;
      const snippetQuestions = cat.reviewQuestions.filter(
        q => q.file === snippet.file && (q.lineStart === undefined || q.lineStart === snippet.lineStart)
      );
      let pendingCount = 0;
      snippetQuestions.forEach((q) => {
        const qIdx = cat.reviewQuestions.indexOf(q);
        const action = this.feedbackActions.get(`${catIdx}:${qIdx}`);
        if (!action) pendingCount += 1;
      });
      return pendingCount;
    };

    const updateSnippetFeedbackBadges = (): void => {
      snippetFeedbackBadges.forEach((badge, snippetIdx) => {
        const pendingCount = getPendingFeedbackCountForSnippet(snippetIdx);
        if (pendingCount <= 0) {
          badge.style.display = "none";
          return;
        }
        const countEl = badge.querySelector<HTMLElement>(".jp-file-feedback-count");
        if (countEl) countEl.textContent = String(pendingCount);
        badge.style.display = "";
      });
    };

    const renderTreeNode = (node: FileTreeNode, depth: number) => {
      const children = [...node.children.values()].sort((a, b) => {
        if (a.isFile !== b.isFile) return a.isFile ? 1 : -1;
        return a.name.localeCompare(b.name);
      });

      children.forEach((child) => {
        if (!child.isFile) {
          const dirRow = document.createElement("div");
          dirRow.className = "jp-tree-dir-row";
          dirRow.style.paddingLeft = `${depth * 12}px`;
          dirRow.innerHTML = `
            <span class="jp-tree-dir-caret">▾</span>
            <span class="jp-tree-dir-name">${escapeHtml(child.name)}</span>
          `;
          treeRoot.appendChild(dirRow);
          renderTreeNode(child, depth + 1);
          return;
        }

        const fullPath = child.fullPath ?? child.path;
        const entries = byFile.get(fullPath) ?? [];

        const fileGroup = document.createElement("div");
        fileGroup.className = "jp-file-group jp-file-group--tree";

        const fileName = document.createElement("div");
        fileName.className = "jp-file-group-name";
        fileName.title = fullPath;
        fileName.textContent = child.name;
        fileName.style.paddingLeft = `${depth * 12}px`;
        fileGroup.appendChild(fileName);

        const rangeList = document.createElement("div");
        rangeList.className = "jp-file-range-list";
        rangeList.style.marginLeft = `${depth * 12}px`;
        entries.forEach(({ snippetIdx, lineStart }) => {
          const s = cat.snippets[snippetIdx];
          const btn = document.createElement("button");
          btn.className = "jp-file-range-btn";
          btn.dataset.snippetIdx = String(snippetIdx);
          const groupLabel = s.explanation?.split(" ").slice(0, 4).join(" ") || "Change";
          const snippetRisk = s.riskLevel ?? cat.riskLevel;
          btn.dataset.level = snippetRisk;
          btn.innerHTML = `
            <div class="jp-file-range-group">${escapeHtml(groupLabel)}</div>
            <div class="jp-file-range-footer">
              <span class="jp-file-range-line">L${lineStart}</span>
              <span class="jp-file-range-footer-right">
                <span class="jp-file-feedback-badge" style="display:none">
                  <span class="jp-file-feedback-icon">💬</span>
                  <span class="jp-file-feedback-count">0</span>
                </span>
              </span>
            </div>
          `;
          const badgeEl = btn.querySelector<HTMLElement>(".jp-file-feedback-badge");
          if (badgeEl) snippetFeedbackBadges.set(snippetIdx, badgeEl);
          rangeList.appendChild(btn);
        });

        fileGroup.appendChild(rangeList);

        treeRoot.appendChild(fileGroup);
      });
    };

    renderTreeNode(fileTree, 0);
    updateSnippetFeedbackBadges();
    leftCol.appendChild(treeRoot);

    // ── Section summary strip ─────────────────────────────────────
    if (cat.summary?.trim()) {
      const summaryStrip = document.createElement("div");
      summaryStrip.className = "jp-step-summary jp-step-summary--collapsed";
      summaryStrip.innerHTML = `
        <button type="button" class="jp-step-summary-toggle">About this section</button>
        <div class="jp-step-summary-text jp-md">${md(cat.summary)}</div>
      `;
      summaryStrip.querySelector(".jp-step-summary-toggle")!.addEventListener("click", () => {
        summaryStrip.classList.toggle("jp-step-summary--collapsed");
      });
      el.appendChild(summaryStrip);
    }

    cols.appendChild(leftCol);

    // ── CENTER: single focused diff + findings inline ─────────────
    const centerCol = document.createElement("div");
    centerCol.className = "jp-step-col-diffs";
    const feedbackContainer = document.createElement("div");
    feedbackContainer.className = "jp-feedback-container";

    // Active diff container — swapped when left nav is clicked
    const diffFocus = document.createElement("div");
    diffFocus.className = "jp-diff-focus";

    const renderFocusedSnippet = (snippetIdx: number) => {
      const s = cat.snippets[snippetIdx];
      if (!s) {
        diffFocus.innerHTML = `<div class="jp-diff-focus-empty">No code snippets for this section.</div>`;
        feedbackContainer.innerHTML = "";
        return;
      }
      const key = `${catIdx}:${snippetIdx}`;
      const hasBefore = s.before && s.before.trim().length > 0;
      const hasAfter = s.after && s.after.trim().length > 0;

      diffFocus.innerHTML = "";

      // File + location bar
      const cardHeader = document.createElement("div");
      cardHeader.className = "jp-diff-card-header";
      cardHeader.innerHTML = `
        <div class="jp-diff-card-meta">
          <span class="jp-diff-card-file">${escapeHtml(s.file)} · L${s.lineStart}</span>
        </div>
      `;
      diffFocus.appendChild(cardHeader);

      // AI explanation — above the diff so reader has context before looking at code
      if (s.explanation) {
        const expEl = document.createElement("div");
        expEl.className = "jp-diff-focus-summary";
        expEl.textContent = s.explanation;
        diffFocus.appendChild(expEl);
      }

      // Side-by-side diff
      const diffGrid = document.createElement("div");
      diffGrid.className = "jp-diff-card-diff";

      const makeGutter = (code: string, startLine: number): string => {
        const count = code.split("\n").length;
        return Array.from({ length: count }, (_, i) =>
          `<span>${startLine + i}</span>`
        ).join("\n");
      };

      const beforeCol = document.createElement("div");
      beforeCol.className = `jp-diff-col jp-diff-col--before${!hasBefore ? " jp-diff-col--empty" : ""}`;
      if (hasBefore) {
        beforeCol.innerHTML = `<div class="jp-diff-col-header">− Before</div>`;
        const wrap = document.createElement("div");
        wrap.className = "jp-diff-code-wrap";
        wrap.innerHTML = `
          <pre class="jp-diff-gutter">${makeGutter(s.before, s.lineStart)}</pre>
          <pre class="jp-diff-code">${this.highlight(s.before, s.language)}</pre>
        `;
        beforeCol.appendChild(wrap);
      } else {
        beforeCol.innerHTML = `
          <div class="jp-diff-col-header">− Before</div>
          <pre class="jp-diff-code">pure addition</pre>
        `;
      }

      const afterCol = document.createElement("div");
      afterCol.className = `jp-diff-col jp-diff-col--after${!hasAfter ? " jp-diff-col--empty" : ""}`;
      if (hasAfter) {
        afterCol.innerHTML = `<div class="jp-diff-col-header">+ After</div>`;
        const wrap = document.createElement("div");
        wrap.className = "jp-diff-code-wrap";
        wrap.innerHTML = `
          <pre class="jp-diff-gutter">${makeGutter(s.after, s.lineStart)}</pre>
          <pre class="jp-diff-code">${this.highlight(s.after, s.language)}</pre>
        `;
        afterCol.appendChild(wrap);
      } else {
        afterCol.innerHTML = `
          <div class="jp-diff-col-header">+ After</div>
          <pre class="jp-diff-code">pure deletion</pre>
        `;
      }
      diffGrid.appendChild(beforeCol);
      diffGrid.appendChild(afterCol);
      diffFocus.appendChild(diffGrid);

      // Inline comments — after the diff, resolved ones collapsed by default
      const snippetLineEnd = s.lineStart + Math.max(
        s.before ? s.before.split("\n").length : 0,
        s.after ? s.after.split("\n").length : 0,
      );
      const inlineComments = this.getInlineCommentsForSnippet(s.file, s.lineStart, snippetLineEnd);
      const fileComments = this.getFileAnchorComments(s.file);
      const existing = [...inlineComments, ...fileComments];
      if (existing.length > 0) {
        diffFocus.appendChild(this.buildExistingCommentsBlock("", existing));
      }

      // Per-snippet note
      const noteWrap = document.createElement("div");
      noteWrap.className = "jp-diff-card-note-wrap";
      const renderNoteWrap = () => {
        noteWrap.innerHTML = "";
        const existingNote = this.comments.get(key);
        if (existingNote) {
          noteWrap.appendChild(this.buildCommentNote(existingNote, key, noteWrap, false));
          return;
        }
        const noteBtn = document.createElement("button");
        noteBtn.className = "jp-snippet-comment-btn";
        noteBtn.textContent = "+ Note";
        noteBtn.addEventListener("click", () => {
          if (noteWrap.querySelector(".jp-comment-box, .jp-comment-note")) return;
          noteBtn.remove();
          noteWrap.appendChild(this.buildCommentForm(key, noteWrap, false));
        });
        noteWrap.appendChild(noteBtn);
      };
      renderNoteWrap();
      diffFocus.appendChild(noteWrap);

      // Feedback questions anchored to this snippet
      feedbackContainer.innerHTML = "";
      const snippetQuestions = cat.reviewQuestions.filter(
        q => q.file === s.file && (q.lineStart === undefined || q.lineStart === s.lineStart)
      );
      updateSnippetFeedbackBadges();
      if (snippetQuestions.length > 0) {
        const feedbackCard = document.createElement("div");
        feedbackCard.className = "jp-feedback-card";

        const list = document.createElement("div");
        list.className = "jp-feedback-list";
        snippetQuestions.forEach((q) => {
          const qIdx = cat.reviewQuestions.indexOf(q);
          const actionKey = `${catIdx}:${qIdx}`;

          const qCard = document.createElement("div");
          qCard.className = "jp-finding-card";

          const textEl = document.createElement("p");
          textEl.className = "jp-finding-text";
          textEl.textContent = q.text;
          qCard.appendChild(textEl);

          const btnsRow = document.createElement("div");
          btnsRow.className = "jp-finding-btns";

          const addToNoteBtn = document.createElement("button");
          addToNoteBtn.className = "jp-finding-btn jp-finding-btn--note";
          addToNoteBtn.textContent = "Draft";
          addToNoteBtn.title = "Add to a draft note. Posts when you submit the review.";

          const postBtn = document.createElement("button");
          postBtn.className = "jp-finding-btn";
          postBtn.textContent = "Post";
          postBtn.title = "Queue as a GitHub inline comment on submit.";

          const dismissBtn = document.createElement("button");
          dismissBtn.className = "jp-finding-btn";
          dismissBtn.textContent = "Skip";
          dismissBtn.title = "Dismiss this finding";

          const applyFeedbackState = () => {
            const action = this.feedbackActions.get(actionKey);
            addToNoteBtn.className = "jp-finding-btn jp-finding-btn--note" + (action === "noted"     ? " jp-finding-btn--noted"     : "");
            postBtn.className      = "jp-finding-btn"                       + (action === "post"      ? " jp-finding-btn--post"      : "");
            dismissBtn.className   = "jp-finding-btn"                       + (action === "dismissed" ? " jp-finding-btn--dismissed" : "");
            qCard.classList.toggle("jp-finding-card--noted",     action === "noted");
            qCard.classList.toggle("jp-finding-card--post",      action === "post");
            qCard.classList.toggle("jp-finding-card--dismissed", action === "dismissed");
          };
          applyFeedbackState();

          addToNoteBtn.addEventListener("click", () => {
            const current = this.feedbackActions.get(actionKey);
            if (current === "noted") {
              // Un-note: just clear the action state; leave the note text as-is since user may have edited it
              this.feedbackActions.delete(actionKey);
              applyFeedbackState();
              updateSnippetFeedbackBadges();
              return;
            }
            // Append question text to the snippet note. This is mutually
            // exclusive with direct posting to avoid duplicate comments.
            const existingNote = this.comments.get(key) ?? "";
            const newNote = existingNote ? `${existingNote}\n\n${q.text}` : q.text;
            this.comments.set(key, newNote);
            this.feedbackActions.set(actionKey, "noted");
            // Update noteWrap immediately
            noteWrap.innerHTML = "";
            noteWrap.appendChild(this.buildCommentNote(newNote, key, noteWrap, false));
            applyFeedbackState();
            updateSnippetFeedbackBadges();
          });

          postBtn.addEventListener("click", () => {
            const current = this.feedbackActions.get(actionKey);
            if (current === "post") {
              this.feedbackActions.delete(actionKey);
            } else {
              if (current === "noted") {
                this.removeFindingFromDraftNote(key, q.text);
                renderNoteWrap();
              }
              this.feedbackActions.set(actionKey, "post");
            }
            applyFeedbackState();
            updateSnippetFeedbackBadges();
          });

          dismissBtn.addEventListener("click", () => {
            const current = this.feedbackActions.get(actionKey);
            if (current === "dismissed") {
              this.feedbackActions.delete(actionKey);
            } else {
              if (current === "noted") {
                this.removeFindingFromDraftNote(key, q.text);
                renderNoteWrap();
              }
              this.feedbackActions.set(actionKey, "dismissed");
            }
            applyFeedbackState();
            updateSnippetFeedbackBadges();
          });

          btnsRow.appendChild(addToNoteBtn);
          btnsRow.appendChild(postBtn);
          btnsRow.appendChild(dismissBtn);
          qCard.appendChild(btnsRow);
          list.appendChild(qCard);
        });
        feedbackCard.appendChild(list);
        feedbackContainer.appendChild(feedbackCard);
      }

      // Update active state in left col
      leftCol.querySelectorAll<HTMLElement>(".jp-file-range-btn").forEach((b) => {
        b.classList.toggle("jp-file-range-btn--active", parseInt(b.dataset.snippetIdx ?? "-1", 10) === snippetIdx);
      });
    };

    centerCol.appendChild(diffFocus);

    // Feedback card container — re-populated by renderFocusedSnippet
    centerCol.appendChild(feedbackContainer);

    cols.appendChild(centerCol);
    el.appendChild(cols);

    // ── Wire up left nav → swap focused diff ─────────────────────
    // Render first snippet by default
    renderFocusedSnippet(0);

    leftCol.querySelectorAll<HTMLElement>(".jp-file-range-btn").forEach((btn) => {
      btn.addEventListener("click", () => {
        const idx = parseInt(btn.dataset.snippetIdx ?? "0", 10);
        renderFocusedSnippet(idx);
      });
    });

    this.screenEl.appendChild(el);
    this.updateProgressBar();
  }

  private renderDecision(): void {
    const s = this.state;
    const el = document.createElement("div");
    el.className = "jp-decision";

    const rail = document.createElement("div");
    rail.className = "jp-decision-rail";
    el.appendChild(rail);

    const header = document.createElement("div");
    header.className = "jp-decision-header";
    header.innerHTML = `<div class="jp-decision-title">Finish Review</div>`;
    el.appendChild(header);

    const body = document.createElement("div");
    body.className = "jp-decision-body";

    // AI recommendation card
    if (s.recommendation) {
      const icons: Record<string, string> = {
        approve: "✓", request_changes: "✕", needs_review: "⚑",
      };
      const titles: Record<string, string> = {
        approve: "Approve", request_changes: "Request Changes", needs_review: "Needs Review",
      };
      body.innerHTML = `
        <div class="jp-rec-card" data-action="${s.recommendation.action}">
          <div class="jp-rec-label">AI recommendation</div>
          <div class="jp-rec-icon">${icons[s.recommendation.action]}</div>
          <div class="jp-rec-content">
            <div class="jp-rec-title">${escapeHtml(titles[s.recommendation.action] ?? s.recommendation.action)}</div>
            <div class="jp-rec-reason jp-md">${md(s.recommendation.reason)}</div>
          </div>
        </div>
      `;
    }

    // ── PR-level decision picker ──────────────────────────────
    const decisionLabel = document.createElement("div");
    decisionLabel.className = "jp-sections-label";
    decisionLabel.textContent = "Your Review Decision";
    body.appendChild(decisionLabel);

    const decisionPicker = document.createElement("div");
    decisionPicker.className = "jp-decision-picker";

    const decisions: { value: "APPROVE" | "REQUEST_CHANGES" | "COMMENT"; label: string; desc: string }[] = [
      { value: "APPROVE",          label: "✓  Approve",          desc: "Looks good — approve the PR" },
      { value: "REQUEST_CHANGES",  label: "✕  Request Changes",  desc: "Changes needed before merge" },
      { value: "COMMENT",          label: "◎  Comment Only",     desc: "Leave comments without a verdict" },
    ];

    const updatePicker = () => {
      decisionPicker.querySelectorAll<HTMLElement>(".jp-decision-pick-btn").forEach(btn => {
        btn.classList.toggle("jp-decision-pick-btn--active", btn.dataset.value === this.reviewDecision);
      });
    };

    decisions.forEach(d => {
      const btn = document.createElement("button");
      btn.className = "jp-decision-pick-btn";
      btn.dataset.value = d.value;
      btn.innerHTML = `<span class="jp-decision-pick-label">${d.label}</span><span class="jp-decision-pick-desc">${d.desc}</span>`;
      btn.addEventListener("click", () => {
        this.reviewDecision = this.reviewDecision === d.value ? null : d.value;
        updatePicker();
        updateSubmitState();
      });
      decisionPicker.appendChild(btn);
    });
    updatePicker();
    body.appendChild(decisionPicker);

    // Section flags summary
    if (s.categories.length > 0) {
      const flagsTitle = document.createElement("div");
      flagsTitle.className = "jp-sections-label";
      flagsTitle.style.marginTop = "16px";
      flagsTitle.textContent = "Sections Reviewed";
      body.appendChild(flagsTitle);

      s.categories.forEach((cat, i) => {
        const row = document.createElement("div");
        row.className = "jp-flag-row";
        row.innerHTML = `
          <span class="jp-flag-icon">${escapeHtml(cat.icon)}</span>
          <span class="jp-flag-name">${escapeHtml(cat.label)}</span>
          <span class="jp-flag-risk" data-level="${escapeHtml(cat.riskLevel)}">${escapeHtml(cat.riskLevel)}</span>
        `;
        row.addEventListener("click", () => {
          this.currentStep = i + 1;
          this.renderScreen("step");
        });
        body.appendChild(row);
      });
    }

    // Comment summary — show a breakdown of what will be posted
    const postCount  = [...this.feedbackActions.values()].filter(v => v === "post").length;
    const noteCount  = this.comments.size;
    const totalCount = postCount + noteCount;
    if (totalCount > 0) {
      const commentSummary = document.createElement("div");
      commentSummary.className = "jp-comment-summary";
      const parts: string[] = [];
      if (noteCount  > 0) parts.push(`${noteCount} snippet note${noteCount  !== 1 ? "s" : ""}`);
      if (postCount  > 0) parts.push(`${postCount} inline post${postCount   !== 1 ? "s" : ""}`);
      commentSummary.textContent = `${parts.join(" · ")} queued for GitHub when you submit`;
      body.appendChild(commentSummary);
    }

    const prComments = this.getPRDiscussionComments();
    if (prComments.length > 0) {
      const discussionTitle = document.createElement("div");
      discussionTitle.className = "jp-sections-label";
      discussionTitle.style.marginTop = "16px";
      discussionTitle.textContent = "PR Discussion";
      body.appendChild(discussionTitle);
      body.appendChild(this.buildExistingCommentsBlock("", prComments));
    }

    el.appendChild(body);

    const footer = document.createElement("div");
    footer.className = "jp-decision-footer";
    footer.innerHTML = `<button class="jp-nav-btn">← Back to Overview</button>`;
    footer.querySelector("button")!.addEventListener("click", () => {
      this.currentStep = 0;
      this.renderScreen("overview");
    });

    // Check prior submission
    const priorRaw = sessionStorage.getItem(this.submittedKey);
    const prior = priorRaw ? JSON.parse(priorRaw) as { event: string; commentCount: number; ts: number } : null;

    const submitBtn = document.createElement("button");
    submitBtn.className = "jp-submit-review-btn";
    const statusEl = document.createElement("div");
    statusEl.className = "jp-submit-review-status";

    const updateSubmitState = () => {
      if (prior && !this.reviewDecision) {
        // Already submitted, no new decision selected
        const minsAgo = Math.round((Date.now() - prior.ts) / 60000);
        const when = minsAgo < 1 ? "just now" : `${minsAgo}m ago`;
        submitBtn.textContent = "Resubmit Review";
        submitBtn.disabled = false;
        statusEl.className = "jp-submit-review-status jp-submit-review-status--ok";
        statusEl.textContent = `${prior.event} submitted ${when} · ${prior.commentCount} comment${prior.commentCount !== 1 ? "s" : ""}`;
      } else if (!this.reviewDecision) {
        submitBtn.textContent = "Submit Review to GitHub";
        submitBtn.disabled = true;
        statusEl.textContent = "Select a decision above to submit";
        statusEl.className = "jp-submit-review-status";
      } else {
        submitBtn.textContent = prior ? "Resubmit Review" : "Submit Review to GitHub";
        submitBtn.disabled = false;
        statusEl.textContent = "";
        statusEl.className = "jp-submit-review-status";
      }
    };
    updateSubmitState();

    submitBtn.addEventListener("click", async () => {
      if (!this.reviewDecision) return;
      submitBtn.disabled = true;
      statusEl.className = "jp-submit-review-status";
      statusEl.textContent = "Submitting…";

      try {
        const commentCount = await this.submitReview();
        const record = { event: this.reviewDecision, commentCount, ts: Date.now() };
        sessionStorage.setItem(this.submittedKey, JSON.stringify(record));
        statusEl.className = "jp-submit-review-status jp-submit-review-status--ok";
        statusEl.textContent = `${this.reviewDecision.replace("_", " ")} posted to GitHub ✓ · ${commentCount} comment${commentCount !== 1 ? "s" : ""}`;
      } catch (err) {
        submitBtn.disabled = false;
        statusEl.className = "jp-submit-review-status jp-submit-review-status--err";
        statusEl.textContent = err instanceof Error ? err.message : "Failed to post review";
        updateSubmitState();
      }
    });

    footer.appendChild(submitBtn);
    footer.appendChild(statusEl);
    el.appendChild(footer);

    this.screenEl.appendChild(el);
    this.updateProgressBar();
  }

  // ── Progress bar ───────────────────────────────────────────

  private progressBarEl: HTMLElement | null = null;

  private ensureProgressBar(): void {
    if (this.progressBarEl) return;
    this.progressBarEl = document.createElement("div");
    this.progressBarEl.className = "jp-progress-bar";
    // Insert after header (second child after resize handle)
    const header = this.panel.querySelector(".jp-header");
    if (header?.nextSibling) {
      this.panel.insertBefore(this.progressBarEl, header.nextSibling);
    }
  }

  private updateProgressBar(): void {
    if (this.currentStep === 0 && this.state.categories.length === 0) return;
    this.ensureProgressBar();

    const total = this.state.categories.length;
    const totalSteps = total + 1; // categories + decision
    const bar = this.progressBarEl!;

    // overview = step 0, categories = 1..n, decision = n+1
    const stepLabel = this.currentStep === 0
      ? "Overview"
      : this.currentStep <= total
        ? `${this.state.categories[this.currentStep - 1]?.label}`
        : "Decision";

    bar.innerHTML = `
      <div class="jp-progress-meta">
        <span class="jp-progress-step-name">${escapeHtml(stepLabel)}</span>
        <span class="jp-progress-counter">${this.currentStep === 0 ? "—" : `${Math.min(this.currentStep, totalSteps)} / ${totalSteps}`}</span>
      </div>
      <div class="jp-progress-steps"></div>
    `;

    const stepsEl = bar.querySelector(".jp-progress-steps")!;
    for (let i = 1; i <= totalSteps; i++) {
      const step = document.createElement("div");
      step.className = "jp-progress-step";
      if (i < this.currentStep) step.classList.add("jp-done");
      else if (i === this.currentStep) step.classList.add("jp-current");

      const dot = document.createElement("div");
      dot.className = "jp-progress-step-dot";
      step.appendChild(dot);

      const idx = i;
      step.addEventListener("click", () => {
        if (idx <= total) {
          this.currentStep = idx;
          this.renderScreen("step");
        } else {
          this.currentStep = totalSteps;
          this.renderScreen("decision");
        }
      });

      stepsEl.appendChild(step);
    }
  }

  // ── Snippet builder ────────────────────────────────────────

  private static readonly LANG_ALIASES: Record<string, string> = {
    // TypeScript
    ts: "typescript", tsx: "typescript",
    // JavaScript
    js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript",
    // Python
    py: "python", py3: "python", python3: "python",
    // Go
    golang: "go",
    // Ruby
    rb: "ruby", rake: "ruby", gemfile: "ruby",
    // Rust
    rs: "rust",
    // C / C++
    "c++": "cpp", "c/c++": "cpp", cc: "cpp", cxx: "cpp", hpp: "cpp", hxx: "cpp",
    h: "c",
    // Kotlin
    kt: "kotlin", kts: "kotlin",
    // Swift
    swiftui: "swift",
    // Scala
    sc: "scala",
    // PHP
    php3: "php", php4: "php", php5: "php", phtml: "php",
    // Shell
    sh: "bash", zsh: "bash", ksh: "bash", fish: "bash", shell: "bash",
    // YAML
    yml: "yaml",
    // Protobuf
    proto: "protobuf", proto3: "protobuf",
    // Markup
    svg: "xml", xhtml: "xml", htm: "html",
  };

  private highlight(code: string, lang: string): string {
    if (!code || !code.trim()) return "";
    try {
      const normalized = JustPROverlay.LANG_ALIASES[lang] ?? lang;
      const supported = hljs.getLanguage(normalized) ? normalized : "plaintext";
      if (supported === "plaintext") return escapeHtml(code);
      return hljs.highlight(code, { language: supported }).value;
    } catch { return escapeHtml(code); }
  }

  private buildFileTree(paths: string[]): FileTreeNode {
    const root: FileTreeNode = {
      name: "",
      path: "",
      isFile: false,
      children: new Map<string, FileTreeNode>(),
    };

    const uniquePaths = [...new Set(paths)];
    uniquePaths.forEach((fullPath) => {
      const segments = fullPath.split("/").filter(Boolean);
      let cursor = root;
      let prefix = "";

      segments.forEach((seg, idx) => {
        prefix = prefix ? `${prefix}/${seg}` : seg;
        let next = cursor.children.get(seg);
        if (!next) {
          next = {
            name: seg,
            path: prefix,
            isFile: idx === segments.length - 1,
            children: new Map<string, FileTreeNode>(),
          };
          cursor.children.set(seg, next);
        }
        if (idx === segments.length - 1) {
          next.isFile = true;
          next.fullPath = fullPath;
        }
        cursor = next;
      });
    });

    return root;
  }

  private buildSnippet(s: CodeSnippet, catIdx: number, snippetIdx: number): HTMLElement {
    const key = `${catIdx}:${snippetIdx}`;
    const wrap = document.createElement("div");
    wrap.className = "jp-snippet";

    // File header
    const fileBar = document.createElement("div");
    fileBar.className = "jp-snippet-file";
    fileBar.innerHTML = `
      <span class="jp-snippet-file-name">${escapeHtml(s.file)}</span>
      <span class="jp-snippet-line">L${s.lineStart}</span>
    `;
    const commentBtn = document.createElement("button");
    commentBtn.className = "jp-snippet-comment-btn";
    commentBtn.textContent = "Note";
    commentBtn.addEventListener("click", () => {
      const existing = wrap.querySelector(".jp-comment-box, .jp-comment-note");
      if (existing) return;
      wrap.appendChild(this.buildCommentForm(key, wrap, false));
    });
    fileBar.appendChild(commentBtn);
    wrap.appendChild(fileBar);

    // Explanation
    if (s.explanation) {
      const exp = document.createElement("div");
      exp.className = "jp-snippet-explanation jp-md";
      exp.innerHTML = md(s.explanation);
      wrap.appendChild(exp);
    }

    // Diff view
    const hasBefore = s.before && s.before.trim().length > 0;
    const hasAfter = s.after && s.after.trim().length > 0;

    const diff = document.createElement("div");
    diff.className = "jp-diff";

    // Before column
    const beforeCol = document.createElement("div");
    beforeCol.className = `jp-diff-col jp-diff-col--before${!hasBefore ? " jp-diff-col--empty" : ""}`;
    beforeCol.innerHTML = `
      <div class="jp-diff-col-header">− Before</div>
      <pre class="jp-diff-code">${hasBefore ? this.highlight(s.before, s.language) : "pure addition"}</pre>
    `;
    diff.appendChild(beforeCol);

    // After column
    const afterCol = document.createElement("div");
    afterCol.className = `jp-diff-col jp-diff-col--after${!hasAfter ? " jp-diff-col--empty" : ""}`;
    afterCol.innerHTML = `
      <div class="jp-diff-col-header">+ After</div>
      <pre class="jp-diff-code">${hasAfter ? this.highlight(s.after, s.language) : "pure deletion"}</pre>
    `;
    diff.appendChild(afterCol);

    wrap.appendChild(diff);

    // Existing comment
    const existingComment = this.comments.get(key);
    if (existingComment) {
      wrap.appendChild(this.buildCommentNote(existingComment, key, wrap, false));
    }

    return wrap;
  }

  private buildCommentForm(key: string, container: HTMLElement, isSection: boolean): HTMLElement {
    const box = document.createElement("div");
    box.className = "jp-comment-box";

    const textarea = document.createElement("textarea");
    textarea.className = "jp-comment-textarea";
    textarea.placeholder = isSection ? "Add a note for this section..." : "Add a note for this snippet...";
    textarea.value = this.comments.get(key) ?? "";
    // Prevent host-page shortcut handlers (e.g. GitHub's "s" key) from firing
    // while the user is typing inside the shadow DOM textarea.
    textarea.addEventListener("keydown", (e) => e.stopPropagation());
    textarea.addEventListener("keypress", (e) => e.stopPropagation());

    const actions = document.createElement("div");
    actions.className = "jp-comment-actions";

    const cancel = document.createElement("button");
    cancel.className = "jp-comment-cancel";
    cancel.textContent = "Cancel";
    cancel.addEventListener("click", () => box.remove());

    const save = document.createElement("button");
    save.className = "jp-comment-save";
    save.textContent = "Save";
    save.addEventListener("click", () => {
      const text = textarea.value.trim();
      if (!text) { this.comments.delete(key); box.remove(); return; }
      this.comments.set(key, text);
      box.replaceWith(this.buildCommentNote(text, key, container, isSection));
    });

    actions.appendChild(cancel);
    actions.appendChild(save);
    box.appendChild(textarea);
    box.appendChild(actions);
    return box;
  }

  private buildCommentNote(text: string, key: string, container: HTMLElement, isSection: boolean): HTMLElement {
    const note = document.createElement("div");
    note.className = "jp-comment-note";
    const textEl = document.createElement("span");
    textEl.className = "jp-comment-note-text";
    textEl.textContent = text;
    const del = document.createElement("button");
    del.className = "jp-comment-note-del";
    del.textContent = "✕";
    del.addEventListener("click", () => {
      this.comments.delete(key);
      note.remove();
      // Re-show the add button for section comments
      if (isSection) {
        const btn = document.createElement("button");
        btn.className = "jp-section-comment-btn";
        btn.textContent = "+ Add section note";
        btn.addEventListener("click", () => {
          container.innerHTML = "";
          container.appendChild(this.buildCommentForm(key, container, true));
        });
        container.appendChild(btn);
      }
    });
    note.appendChild(textEl);
    note.appendChild(del);
    return note;
  }

  private getPRDiscussionComments(): ExistingComment[] {
    return this.state.existingComments.filter(c => c.anchor === "pr" || !c.path);
  }

  private getFileAnchorComments(path: string): ExistingComment[] {
    return this.state.existingComments.filter(c => c.path === path && c.anchor === "file");
  }

  private getInlineCommentsForSnippet(path: string, lineStart: number, lineEnd: number): ExistingComment[] {
    return this.state.existingComments.filter(c => {
      if (c.anchor !== "inline" || c.path !== path) return false;
      // Active inline: match on current line
      if (c.line && c.line >= lineStart && c.line <= lineEnd) return true;
      // Outdated inline: match on original line (best effort — same file)
      if (c.outdated && c.originalLine && c.originalLine >= lineStart && c.originalLine <= lineEnd) return true;
      return false;
    });
  }

  private buildExistingCommentsBlock(title: string, comments: ExistingComment[]): HTMLElement {
    const commentsEl = document.createElement("div");
    commentsEl.className = "jp-existing-comments";
    if (title) {
      const labelEl = document.createElement("div");
      labelEl.className = "jp-existing-comments-label";
      labelEl.textContent = title;
      commentsEl.appendChild(labelEl);
    }

    comments.forEach(c => {
      const item = document.createElement("div");
      item.className = "jp-existing-comment";
      item.dataset.anchor = c.anchor;
      if (c.outdated) item.dataset.outdated = "true";
      if (c.resolved) item.dataset.resolved = "true";

      const statusBadge = c.resolved
        ? `<span class="jp-comment-status-badge jp-comment-status-badge--resolved">resolved</span>`
        : c.outdated
          ? `<span class="jp-comment-status-badge jp-comment-status-badge--outdated">outdated</span>`
          : "";

      const location = this.existingCommentLocation(c);

      item.innerHTML = `
        <div class="jp-existing-comment-meta">
          <span class="jp-existing-comment-author">${escapeHtml(c.author)}</span>
          ${statusBadge}
          <span class="jp-existing-comment-line">${escapeHtml(location)}</span>
        </div>
        <div class="jp-existing-comment-body jp-md">${md(c.body)}</div>
      `;

      // Resolved comments: collapsed by default, click meta to expand
      if (c.resolved) {
        const body = item.querySelector<HTMLElement>(".jp-existing-comment-body")!;
        body.style.display = "none";
        const meta = item.querySelector<HTMLElement>(".jp-existing-comment-meta")!;
        meta.style.cursor = "pointer";
        meta.title = "Click to expand";
        meta.addEventListener("click", () => {
          const hidden = body.style.display === "none";
          body.style.display = hidden ? "" : "none";
          meta.title = hidden ? "Click to collapse" : "Click to expand";
        });
      }

      commentsEl.appendChild(item);
    });

    return commentsEl;
  }

  private existingCommentLocation(comment: ExistingComment): string {
    if (comment.outdated && comment.originalLine) return `was L${comment.originalLine}`;
    if (comment.anchor === "inline" && comment.line) return `L${comment.line}`;
    if (comment.path) return "file";
    return comment.source === "review" ? "review summary" : "conversation";
  }

  private removeFindingFromDraftNote(key: string, findingText: string): void {
    const existing = this.comments.get(key);
    if (!existing) return;

    const target = findingText.trim();
    const parts = existing
      .split(/\n{2,}/)
      .map(part => part.trim())
      .filter(Boolean);
    const remaining = parts.filter(part => part !== target);

    if (remaining.length === parts.length) return;
    if (remaining.length === 0) {
      this.comments.delete(key);
      return;
    }
    this.comments.set(key, remaining.join("\n\n"));
  }

  // ── GitHub review submission ────────────────────────────────

  // Returns the number of inline comments posted
  private async submitReview(): Promise<number> {
    interface ReviewComment { path: string; line: number; lineEnd?: number; body: string; side?: "LEFT" | "RIGHT"; }
    const comments: ReviewComment[] = [];

    // 1. Notes added via "+ Note" (key = "catIdx:snippetIdx")
    this.comments.forEach((text, key) => {
      const parts = key.split(":");
      if (parts.length !== 2) return;
      const catIdx = parseInt(parts[0], 10);
      const snippetIdx = parseInt(parts[1], 10);
      const snippet = this.state.categories[catIdx]?.snippets[snippetIdx];
      if (!snippet?.file || !snippet.lineStart || snippet.lineStart <= 0) return;
      const side = snippet.side ?? (snippet.after?.trim() ? "RIGHT" : "LEFT");
      comments.push({
        path: snippet.file,
        line: snippet.lineStart,
        lineEnd: snippet.lineEnd,
        body: text,
        side,
      });
    });

    // 2. "Post" feedback actions (key = "catIdx:questionIdx") — post as standalone inline comments
    this.feedbackActions.forEach((action, key) => {
      if (action !== "post") return;
      const parts = key.split(":");
      if (parts.length !== 2) return;
      const catIdx = parseInt(parts[0], 10);
      const qIdx = parseInt(parts[1], 10);
      const q = this.state.categories[catIdx]?.reviewQuestions[qIdx];
      if (!q?.file || !q.lineStart || q.lineStart <= 0) return;
      const body = `> ${q.text}\n\n*Posted via AI-assisted review.*`;
      comments.push({ path: q.file, line: q.lineStart, body, side: q.side });
    });

    const validComments = comments.filter(c => c.path && c.line > 0 && c.body.trim());
    const event = this.reviewDecision ?? "COMMENT";
    let body = "";
    if (event === "REQUEST_CHANGES" && validComments.length === 0) {
      body = "Changes requested via PR-LENS.";
    }

    const resp = await fetch(`${BACKEND_URL}/review`, {
      method: "POST",
      headers: backendHeaders(),
      signal: this.abort?.signal,
      body: JSON.stringify({ url: this.prUrl, event, body, comments: validComments }),
    });
    if (!resp.ok) {
      const msg = await resp.text().catch(() => `HTTP ${resp.status}`);
      throw new Error(msg || `HTTP ${resp.status}`);
    }
    return validComments.length;
  }

  // ── Analysis / SSE ─────────────────────────────────────────

  private reset(): void {
    this.state = {
      riskScore: 0, riskLevel: "low", riskLabel: "",
      summary: "", affected: [], reviewOrder: [], categories: [], existingComments: [],
    };
    this.currentStep = 0;
    this.comments.clear();
    this.reviewDecision = null;
    this.feedbackActions.clear();
    this.progressBarEl?.remove();
    this.progressBarEl = null;
    this.renderScreen("idle");
  }

  private showError(message: string, label = "Error"): void {
    this.screenEl.replaceChildren();
    const errEl = document.createElement("div");
    errEl.className = "jp-error";
    const labelEl = document.createElement("div");
    labelEl.className = "jp-error-label";
    labelEl.textContent = label;
    const body = document.createElement("div");
    body.textContent = message;
    errEl.appendChild(labelEl);
    errEl.appendChild(body);
    this.screenEl.appendChild(errEl);
  }

  async startAnalysis(): Promise<void> {
    this.abort?.abort();
    this.abort = new AbortController();
    const gen = ++this.analysisGen;
    const signal = this.abort.signal;
    this.isAnalyzing = true;
    this.state = {
      riskScore: 0, riskLevel: "low", riskLabel: "",
      summary: "", affected: [], reviewOrder: [], categories: [], existingComments: [],
    };
    this.currentStep = 0;
    this.comments.clear();
    this.reviewDecision = null;
    this.feedbackActions.clear();
    if (!this.isOpen) this.togglePanel(true);
    this.renderScreen("loading");

    try {
      const response = await fetch(`${BACKEND_URL}/analyze`, {
        method: "POST",
        headers: backendHeaders(),
        signal,
        body: JSON.stringify({ url: this.prUrl }),
      });

      if (!response.ok) {
        const msg = await response.text().catch(() => `HTTP ${response.status}`);
        throw new Error(msg.trim() || `Backend returned ${response.status}`);
      }
      await this.consumeStream(response);
    } catch (err) {
      if (signal.aborted) return;
      this.showError(err instanceof Error ? err.message : "Failed to connect to backend");
    } finally {
      if (gen === this.analysisGen) this.isAnalyzing = false;
    }
  }

  private async consumeStream(response: Response): Promise<void> {
    const reader = response.body!.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    this.currentStep = 0;
    this.updateLoadingProgress("connected", "Fetching diff & starting analysis");

    let categoryCount = 0;

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";

      for (const line of lines) {
        if (!line.startsWith("data: ")) continue;
        const raw = line.slice(6).trim();
        if (!raw) continue;

        let event: SSEEvent;
        try { event = JSON.parse(raw) as SSEEvent; }
        catch { continue; }

        try {
          switch (event.type) {
            case "risk": {
              this.state.riskScore = typeof event.data.score === "number" ? event.data.score : 0;
              this.state.riskLevel = isRiskLevel(event.data.level) ? event.data.level : "low";
              this.state.riskLabel = typeof event.data.label === "string" ? event.data.label : "";
              this.updateLoadingProgress("risk", `Risk: ${this.state.riskLabel}`);
              break;
            }
            case "summary": {
              if (typeof event.data.text === "string") this.state.summary += event.data.text;
              this.updateLoadingProgress("summary", "Writing summary");
              break;
            }
            case "systems": {
              this.state.affected = Array.isArray(event.data.affected) ? event.data.affected.map(String) : [];
              this.state.reviewOrder = Array.isArray(event.data.reviewOrder) ? event.data.reviewOrder.map(String) : [];
              this.updateLoadingProgress("systems", `${this.state.affected.length} systems identified: ${this.state.affected.join(", ")}`);
              break;
            }
            case "comments": {
              this.state.existingComments = Array.isArray(event.data.comments) ? event.data.comments : [];
              break;
            }
            case "category": {
              categoryCount++;
              const cat = { ...(event.data as PRCategory) };
              if (!Array.isArray(cat.snippets)) cat.snippets = [];
              cat.reviewQuestions = normalizeReviewQuestions(cat.reviewQuestions);
              if (!isRiskLevel(cat.riskLevel)) cat.riskLevel = "low";
              cat.label = typeof cat.label === "string" ? cat.label : cat.id || "Section";
              cat.icon = typeof cat.icon === "string" ? cat.icon : "";
              this.state.categories.push(cat);
              const catPct = Math.min(88, 50 + categoryCount * 5);
              const pctEl = this.screenEl.querySelector<HTMLElement>(".jp-loading-pct");
              const phaseEl = this.screenEl.querySelector<HTMLElement>(".jp-loading-phase");
              const fillEl = this.screenEl.querySelector<HTMLElement>(".jp-loading-fill");
              if (pctEl) pctEl.textContent = `${catPct}%`;
              if (phaseEl) phaseEl.textContent = `Analyzing: ${cat.label} (${cat.fileCount ?? cat.snippets.length} files)`;
              if (fillEl) fillEl.style.width = `${catPct}%`;
              const steps = this.screenEl.querySelectorAll<HTMLElement>(".jp-loading-step");
              steps.forEach(s => {
                if (s.dataset.phase === "categories") s.classList.add("jp-loading-step--active");
                else if (s.dataset.phase !== "recommendation") {
                  s.classList.remove("jp-loading-step--active");
                  s.classList.add("jp-loading-step--done");
                }
              });
              break;
            }
            case "recommendation": {
              const action = event.data.action;
              if (action === "approve" || action === "request_changes" || action === "needs_review") {
                this.state.recommendation = {
                  action,
                  reason: typeof event.data.reason === "string" ? event.data.reason : "",
                };
              }
              this.updateLoadingProgress("recommendation", "Finalizing recommendation");
              break;
            }
            case "warning": {
              break;
            }
            case "done": {
              const hasContent = Boolean(this.state.summary) || this.state.categories.length > 0;
              if (!hasContent) {
                this.showError("Analysis returned no content. Please try again.");
                break;
              }
              this.updateLoadingProgress("done", "Complete");
              await new Promise(r => setTimeout(r, 400));
              this.renderScreen("overview");
              break;
            }
            case "error": {
              this.showError(typeof event.data.message === "string" ? event.data.message : "Analysis failed");
              break;
            }
          }
        } catch {
          continue;
        }
      }
    }

    const hasContent = Boolean(this.state.summary) || this.state.categories.length > 0;
    if (hasContent && this.screenEl.querySelector(".jp-loading")) {
      this.renderScreen("overview");
    } else if (!hasContent && this.screenEl.querySelector(".jp-loading")) {
      this.showError("Analysis ended before results arrived. Please try again.");
    }
  }

  private refreshRisk(): void {
    const score = this.screenEl.querySelector<HTMLElement>(".jp-risk-score");
    const fill = this.screenEl.querySelector<HTMLElement>(".jp-risk-fill");
    const badge = this.screenEl.querySelector<HTMLElement>(".jp-risk-badge");
    if (score) { score.textContent = String(this.state.riskScore); score.dataset.level = this.state.riskLevel; }
    if (fill)  { fill.dataset.level = this.state.riskLevel; requestAnimationFrame(() => { fill.style.width = `${this.state.riskScore}%`; }); }
    if (badge) {
      badge.dataset.level = this.state.riskLevel;
      const label = badge.querySelector("span:last-child");
      if (label) label.textContent = this.state.riskLevel;
    }
    const sub = this.screenEl.querySelector<HTMLElement>(".jp-risk-subtext");
    if (sub) sub.textContent = this.state.riskLabel || "Analyzing";
  }

  // ── Toggle / Resize / Destroy ──────────────────────────────

  private togglePanel(skipReset = false): void {
    this.isOpen = !this.isOpen;
    this.panel.classList.toggle("jp-open", this.isOpen);
    this.toggle.classList.toggle("jp-active", this.isOpen);
    if (this.isOpen && !skipReset && !this.isAnalyzing) {
      if (this.state.categories.length === 0 && !this.state.summary) {
        void this.startAnalysis();
      }
    }
  }

  private initResize(handle: HTMLElement, panel: HTMLElement): void {
    const MIN = 320, MAX = () => Math.round(window.innerWidth * 0.85);
    handle.addEventListener("mousedown", (e: MouseEvent) => {
      e.preventDefault();
      handle.classList.add("jp-resizing");
      const startX = e.clientX, startW = panel.offsetWidth;
      const onMove = (e: MouseEvent) => {
        panel.style.width = `${Math.min(MAX(), Math.max(MIN, startW + (startX - e.clientX)))}px`;
      };
      const onUp = () => {
        handle.classList.remove("jp-resizing");
        document.removeEventListener("mousemove", onMove);
        document.removeEventListener("mouseup", onUp);
      };
      document.addEventListener("mousemove", onMove);
      document.addEventListener("mouseup", onUp);
    });
  }

  /** Local testbed only: open a finished analysis without calling the backend. */
  previewAnalysis(partial: Partial<AnalysisState>, screen: Screen = "overview"): void {
    this.state = { ...this.state, ...partial };
    this.currentStep = screen === "step" ? 1 : screen === "decision" ? this.state.categories.length + 1 : 0;
    this.isOpen = true;
    this.panel.classList.add("jp-open");
    this.toggle.classList.add("jp-active");
    this.renderScreen(screen);
  }

  destroy(): void {
    this.abort?.abort();
    this.host.remove();
  }
}
