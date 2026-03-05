import type { SSEEvent, PRCategory, RiskLevel, CodeSnippet } from "./types";
import { overlayCSS } from "./__generated_css";
import { marked } from "marked";
import hljs from "highlight.js/lib/core";
import typescript from "highlight.js/lib/languages/typescript";
import javascript from "highlight.js/lib/languages/javascript";
import python from "highlight.js/lib/languages/python";
import go from "highlight.js/lib/languages/go";
import java from "highlight.js/lib/languages/java";
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
hljs.registerLanguage("xml", xml);
hljs.registerLanguage("html", xml);
hljs.registerLanguage("bash", bash);
hljs.registerLanguage("shell", bash);
hljs.registerLanguage("sql", sql);
hljs.registerLanguage("css", css);
hljs.registerLanguage("json", json);
hljs.registerLanguage("yaml", yaml);

const BACKEND_URL = "http://localhost:8080";

interface AnalysisState {
  riskScore: number;
  riskLevel: RiskLevel;
  riskLabel: string;
  summary: string;
  affected: string[];
  reviewOrder: string[];
  categories: PRCategory[];
  recommendation?: { action: "approve" | "request_changes" | "needs_review"; reason: string };
}

type Screen = "idle" | "loading" | "overview" | "step" | "decision";

export class JustPROverlay {
  private host: HTMLElement;        // the shadow host in document.body
  private shadow: ShadowRoot;       // isolated shadow DOM
  private panel: HTMLElement;
  private toggle: HTMLElement;
  private screenEl!: HTMLElement;

  private isOpen = false;
  private isAnalyzing = false;
  private prUrl: string;

  private state: AnalysisState = {
    riskScore: 0, riskLevel: "low", riskLabel: "",
    summary: "", affected: [], reviewOrder: [], categories: [],
  };

  // Step navigation
  private currentStep = 0; // 0 = overview, 1..n = categories, n+1 = decision

  // Comments keyed by "catIdx:snippetIdx" or "catIdx:section"
  private comments: Map<string, string> = new Map();

  constructor(prUrl: string) {
    this.prUrl = prUrl;

    // Mount inside Shadow DOM so GitHub's CSS cannot leak in
    this.host = document.createElement("div");
    this.host.id = "just-pr-host";
    this.host.style.cssText = "all:initial;position:fixed;top:0;left:0;z-index:2147483647;pointer-events:none;";
    this.shadow = this.host.attachShadow({ mode: "open" });

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
    // Inject Google Fonts via <link> (works in shadow DOM; @import in <style> does not)
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "https://fonts.googleapis.com/css2?family=DM+Mono:wght@400;500&family=Fraunces:ital,opsz,wght@0,9..144,300;0,9..144,700;0,9..144,900;1,9..144,300&display=swap";
    this.shadow.appendChild(link);

    const style = document.createElement("style");
    style.textContent = overlayCSS;
    this.shadow.appendChild(style);
  }

  // ── Panel skeleton ─────────────────────────────────────────

  private buildToggle(): HTMLElement {
    const btn = document.createElement("button");
    btn.id = "just-pr-toggle";
    btn.textContent = "JUST·PR";
    btn.style.pointerEvents = "auto";
    btn.addEventListener("click", () => this.togglePanel());
    return btn;
  }

  private buildPanel(): HTMLElement {
    const panel = document.createElement("div");
    panel.id = "just-pr-panel";
    panel.style.pointerEvents = "auto";

    // Resize handle
    const handle = document.createElement("div");
    handle.id = "just-pr-resize-handle";
    panel.appendChild(handle);
    this.initResize(handle, panel);

    // Header
    const header = document.createElement("div");
    header.className = "jp-header";
    header.innerHTML = `
      <div class="jp-logo">Just-PR <span class="jp-logo-sub">AI Review</span></div>
      <div class="jp-header-right">
        <button class="jp-btn-icon jp-btn-restart" title="Start over">↺</button>
        <button class="jp-btn-icon jp-btn-close" title="Close">✕</button>
      </div>
    `;
    panel.appendChild(header);

    header.querySelector(".jp-btn-close")!.addEventListener("click", () => this.togglePanel());
    header.querySelector(".jp-btn-restart")!.addEventListener("click", () => {
      if (!this.isAnalyzing) this.reset();
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

    switch (screen) {
      case "idle":     return this.renderIdle();
      case "loading":  return this.renderLoading();
      case "overview": return this.renderOverview();
      case "step":     return this.renderStep(this.currentStep - 1);
      case "decision": return this.renderDecision();
    }
  }

  private renderIdle(): void {
    const el = document.createElement("div");
    el.className = "jp-idle";
    el.innerHTML = `
      <div class="jp-idle-badge">AI-Native Review</div>
      <div class="jp-idle-heading">Understand before<br>you read.</div>
      <div class="jp-idle-url">${this.prUrl}</div>
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

    // Risk section
    const riskSection = document.createElement("div");
    riskSection.className = "jp-section";
    riskSection.innerHTML = `
      <div class="jp-section-label">Risk Assessment</div>
      <div class="jp-risk-hero">
        <div class="jp-risk-score" data-level="${s.riskLevel}">${s.riskScore}</div>
        <div class="jp-risk-aside">
          <div class="jp-risk-badge" data-level="${s.riskLevel}">
            <span class="jp-risk-badge-dot"></span>
            <span>${s.riskLabel || "Analyzing"}</span>
          </div>
          <div class="jp-risk-subtext">out of 100</div>
        </div>
      </div>
      <div class="jp-risk-track">
        <div class="jp-risk-fill" data-level="${s.riskLevel}" style="width:0%"></div>
      </div>
    `;
    el.appendChild(riskSection);

    // Animate risk bar
    requestAnimationFrame(() => {
      const fill = riskSection.querySelector<HTMLElement>(".jp-risk-fill");
      if (fill) fill.style.width = `${s.riskScore}%`;
    });

    // Summary section
    const summarySection = document.createElement("div");
    summarySection.className = "jp-section";
    summarySection.innerHTML = `
      <div class="jp-section-label">Summary</div>
      <div class="jp-summary-text jp-md">${md(s.summary)}<span class="jp-cursor jp-done"></span></div>
    `;
    el.appendChild(summarySection);

    // Affected systems
    if (s.affected.length > 0) {
      const sysSection = document.createElement("div");
      sysSection.className = "jp-section";
      const chips = s.affected.map(a => `<span class="jp-chip">${a}</span>`).join("");
      sysSection.innerHTML = `
        <div class="jp-section-label">Affected Systems</div>
        <div class="jp-chips">${chips}</div>
      `;
      el.appendChild(sysSection);
    }

    // Review order / section list
    if (s.categories.length > 0) {
      const orderSection = document.createElement("div");
      orderSection.className = "jp-section";
      const items = s.categories.map((cat, i) => `
        <div class="jp-order-item" data-idx="${i}">
          <span class="jp-order-num">${String(i + 1).padStart(2, "0")}</span>
          <span class="jp-order-icon">${cat.icon}</span>
          <span class="jp-order-label">${cat.label}</span>
          <span class="jp-order-risk" data-level="${cat.riskLevel}">${cat.riskLevel}</span>
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
    footer.innerHTML = `<button class="jp-start-btn">Start Review <span class="jp-start-btn-count">· ${s.categories.length} sections</span></button>`;
    footer.querySelector(".jp-start-btn")!.addEventListener("click", () => {
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

    // Left rail
    const rail = document.createElement("div");
    rail.className = "jp-step-rail";
    rail.dataset.level = cat.riskLevel;
    el.appendChild(rail);

    // Header
    const header = document.createElement("div");
    header.className = "jp-step-header";
    header.innerHTML = `
      <div class="jp-step-eyebrow">Section ${catIdx + 1} of ${total} · ${cat.fileCount} file${cat.fileCount !== 1 ? "s" : ""}</div>
      <div class="jp-step-title-row">
        <span class="jp-step-icon">${cat.icon}</span>
        <span class="jp-step-title">${cat.label}</span>
        <span class="jp-step-risk-pill" data-level="${cat.riskLevel}">${cat.riskLevel}</span>
      </div>
    `;
    el.appendChild(header);

    // Body
    const body = document.createElement("div");
    body.className = "jp-step-body";

    body.innerHTML = `<div class="jp-step-summary jp-md">${md(cat.summary)}</div>`;

    cat.snippets.forEach((s, snippetIdx) => {
      body.appendChild(this.buildSnippet(s, catIdx, snippetIdx));
    });

    if (cat.reviewQuestions.length > 0) {
      const qTitle = document.createElement("div");
      qTitle.className = "jp-questions-label";
      qTitle.textContent = "Review Questions";
      body.appendChild(qTitle);

      const qList = document.createElement("div");
      qList.className = "jp-questions";
      cat.reviewQuestions.forEach(q => {
        const qEl = document.createElement("div");
        qEl.className = "jp-question";
        qEl.innerHTML = `<span class="jp-question-mark">?</span><span class="jp-md">${md(q)}</span>`;
        qList.appendChild(qEl);
      });
      body.appendChild(qList);
    }

    // Section-level comment
    const sectionCommentKey = `${catIdx}:section`;
    const sectionCommentWrap = document.createElement("div");
    sectionCommentWrap.className = "jp-section-comment-wrap";
    const existingSectionComment = this.comments.get(sectionCommentKey);
    if (existingSectionComment) {
      sectionCommentWrap.appendChild(this.buildCommentNote(existingSectionComment, sectionCommentKey, sectionCommentWrap, true));
    } else {
      const sectionCommentBtn = document.createElement("button");
      sectionCommentBtn.className = "jp-section-comment-btn";
      sectionCommentBtn.textContent = "+ Add section note";
      sectionCommentBtn.addEventListener("click", () => {
        sectionCommentWrap.innerHTML = "";
        sectionCommentWrap.appendChild(this.buildCommentForm(sectionCommentKey, sectionCommentWrap, true));
      });
      sectionCommentWrap.appendChild(sectionCommentBtn);
    }
    body.appendChild(sectionCommentWrap);

    el.appendChild(body);

    // Footer nav
    const nav = document.createElement("div");
    nav.className = "jp-step-nav";

    const prevBtn = document.createElement("button");
    prevBtn.className = "jp-nav-btn";
    prevBtn.innerHTML = "← Back";
    prevBtn.addEventListener("click", () => {
      if (catIdx === 0) {
        this.currentStep = 0;
        this.renderScreen("overview");
      } else {
        this.currentStep = catIdx; // catIdx is 0-based, step is 1-based
        this.renderScreen("step");
      }
    });
    nav.appendChild(prevBtn);

    const nextBtn = document.createElement("button");
    nextBtn.className = "jp-nav-btn jp-primary";
    if (isLast) {
      nextBtn.innerHTML = "Finish Review →";
      nextBtn.addEventListener("click", () => {
        this.currentStep = this.state.categories.length + 1;
        this.renderScreen("decision");
      });
    } else {
      nextBtn.innerHTML = "Next →";
      nextBtn.addEventListener("click", () => {
        this.currentStep = catIdx + 2; // next step (1-based)
        this.renderScreen("step");
      });
    }
    nav.appendChild(nextBtn);

    el.appendChild(nav);
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
    header.innerHTML = `
      <div class="jp-decision-eyebrow">Review Complete</div>
      <div class="jp-decision-title">Your Decision</div>
    `;
    el.appendChild(header);

    const body = document.createElement("div");
    body.className = "jp-decision-body";

    if (s.recommendation) {
      const icons: Record<string, string> = {
        approve: "✓", request_changes: "✕", needs_review: "⚑",
      };
      const titles: Record<string, string> = {
        approve: "Approve", request_changes: "Request Changes", needs_review: "Needs Review",
      };
      body.innerHTML = `
        <div class="jp-rec-card" data-action="${s.recommendation.action}">
          <div class="jp-rec-icon">${icons[s.recommendation.action]}</div>
          <div class="jp-rec-content">
            <div class="jp-rec-title">${titles[s.recommendation.action]}</div>
            <div class="jp-rec-reason jp-md">${md(s.recommendation.reason)}</div>
          </div>
        </div>
      `;
    }

    // Section flags summary
    if (s.categories.length > 0) {
      const flagsTitle = document.createElement("div");
      flagsTitle.className = "jp-sections-label";
      flagsTitle.textContent = "Sections Reviewed";
      body.appendChild(flagsTitle);

      s.categories.forEach((cat, i) => {
        const row = document.createElement("div");
        row.className = "jp-flag-row";
        row.innerHTML = `
          <span class="jp-flag-icon">${cat.icon}</span>
          <span class="jp-flag-name">${cat.label}</span>
          <span class="jp-flag-risk" data-level="${cat.riskLevel}">${cat.riskLevel}</span>
        `;
        row.addEventListener("click", () => {
          this.currentStep = i + 1;
          this.renderScreen("step");
        });
        body.appendChild(row);
      });
    }

    el.appendChild(body);

    const footer = document.createElement("div");
    footer.className = "jp-decision-footer";
    footer.innerHTML = `<button class="jp-nav-btn">← Back to Overview</button>`;
    footer.querySelector("button")!.addEventListener("click", () => {
      this.currentStep = 0;
      this.renderScreen("overview");
    });
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
        <span class="jp-progress-step-name">${stepLabel}</span>
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

    const highlight = (code: string, lang: string): string => {
      if (!code || !code.trim()) return "";
      try {
        const supported = hljs.getLanguage(lang) ? lang : "plaintext";
        if (supported === "plaintext") return escapeHtml(code);
        return hljs.highlight(code, { language: supported }).value;
      } catch { return escapeHtml(code); }
    };

    // Before column
    const beforeCol = document.createElement("div");
    beforeCol.className = `jp-diff-col jp-diff-col--before${!hasBefore ? " jp-diff-col--empty" : ""}`;
    beforeCol.innerHTML = `
      <div class="jp-diff-col-header">− Before</div>
      <pre class="jp-diff-code">${hasBefore ? highlight(s.before, s.language) : "pure addition"}</pre>
    `;
    diff.appendChild(beforeCol);

    // After column
    const afterCol = document.createElement("div");
    afterCol.className = `jp-diff-col jp-diff-col--after${!hasAfter ? " jp-diff-col--empty" : ""}`;
    afterCol.innerHTML = `
      <div class="jp-diff-col-header">+ After</div>
      <pre class="jp-diff-code">${hasAfter ? highlight(s.after, s.language) : "pure deletion"}</pre>
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

  // ── Analysis / SSE ─────────────────────────────────────────

  private reset(): void {
    this.state = {
      riskScore: 0, riskLevel: "low", riskLabel: "",
      summary: "", affected: [], reviewOrder: [], categories: [],
    };
    this.currentStep = 0;
    this.comments.clear();
    this.progressBarEl?.remove();
    this.progressBarEl = null;
    this.renderScreen("idle");
  }

  async startAnalysis(): Promise<void> {
    if (this.isAnalyzing) return;
    this.isAnalyzing = true;
    if (!this.isOpen) this.togglePanel(true);
    this.renderScreen("loading");

    try {
      const response = await fetch(`${BACKEND_URL}/analyze`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: this.prUrl }),
      });

      if (!response.ok) throw new Error(`Backend returned ${response.status}`);
      await this.consumeStream(response);
    } catch (err) {
      this.screenEl.innerHTML = "";
      const errEl = document.createElement("div");
      errEl.className = "jp-error";
      errEl.innerHTML = `<div class="jp-error-label">Error</div>${err instanceof Error ? err.message : "Failed to connect to backend"}`;
      this.screenEl.appendChild(errEl);
    } finally {
      this.isAnalyzing = false;
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

        switch (event.type) {
          case "risk": {
            this.state.riskScore = event.data.score;
            this.state.riskLevel = event.data.level;
            this.state.riskLabel = event.data.label;
            this.updateLoadingProgress("risk", `Risk: ${event.data.label}`);
            break;
          }
          case "summary": {
            this.state.summary += event.data.text;
            this.updateLoadingProgress("summary", "Writing summary");
            break;
          }
          case "systems": {
            this.state.affected = event.data.affected;
            this.state.reviewOrder = event.data.reviewOrder;
            this.updateLoadingProgress("systems", `${event.data.affected.length} systems identified`);
            break;
          }
          case "category": {
            categoryCount++;
            this.state.categories.push(event.data);
            // Interpolate progress between 50% and 90% based on categories
            const catPct = Math.min(88, 50 + categoryCount * 5);
            const pctEl = this.screenEl.querySelector<HTMLElement>(".jp-loading-pct");
            const phaseEl = this.screenEl.querySelector<HTMLElement>(".jp-loading-phase");
            const fillEl = this.screenEl.querySelector<HTMLElement>(".jp-loading-fill");
            if (pctEl) pctEl.textContent = `${catPct}%`;
            if (phaseEl) phaseEl.textContent = `Analyzing: ${event.data.label} (${event.data.fileCount} files)`;
            if (fillEl) fillEl.style.width = `${catPct}%`;
            // Mark categories step as active
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
            this.state.recommendation = event.data;
            this.updateLoadingProgress("recommendation", "Finalizing recommendation");
            break;
          }
          case "done": {
            this.updateLoadingProgress("done", "Complete");
            // Brief pause to show 100%, then switch to overview
            await new Promise(r => setTimeout(r, 400));
            this.renderScreen("overview");
            break;
          }
          case "error": {
            const errEl = document.createElement("div");
            errEl.className = "jp-error";
            errEl.innerHTML = `<div class="jp-error-label">Error</div>${event.data.message}`;
            this.screenEl.appendChild(errEl);
            break;
          }
        }
      }
    }
  }

  private refreshRisk(): void {
    const score = this.screenEl.querySelector<HTMLElement>(".jp-risk-score");
    const fill = this.screenEl.querySelector<HTMLElement>(".jp-risk-fill");
    const badge = this.screenEl.querySelector<HTMLElement>(".jp-risk-badge");
    if (score) { score.textContent = String(this.state.riskScore); score.dataset.level = this.state.riskLevel; }
    if (fill)  { fill.dataset.level = this.state.riskLevel; requestAnimationFrame(() => { fill.style.width = `${this.state.riskScore}%`; }); }
    if (badge) { badge.dataset.level = this.state.riskLevel; badge.querySelector("span:last-child")!.textContent = this.state.riskLabel; }
  }

  // ── Toggle / Resize / Destroy ──────────────────────────────

  private togglePanel(skipReset = false): void {
    this.isOpen = !this.isOpen;
    this.panel.classList.toggle("jp-open", this.isOpen);
    this.toggle.classList.toggle("jp-active", this.isOpen);
    if (this.isOpen && !skipReset && !this.isAnalyzing) this.reset();
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

  destroy(): void {
    this.host.remove();
  }
}

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, "&amp;").replace(/</g, "&lt;")
    .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function md(text: string): string {
  return marked.parse(text, { async: false }) as string;
}
