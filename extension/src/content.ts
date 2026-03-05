import { JustPROverlay } from "./overlay";

/**
 * Detects whether the current page is a PR/MR page and extracts the canonical URL.
 */
function detectPRUrl(): string | null {
  const url = window.location.href;

  // GitHub: /owner/repo/pull/123
  if (/github\.com\/.+\/pull\/\d+/.test(url)) {
    return url.split("?")[0];
  }

  // GitLab: /group/project/-/merge_requests/123
  if (/gitlab\.com\/.+\/-\/merge_requests\/\d+/.test(url)) {
    return url.split("?")[0];
  }

  // Bitbucket: /workspace/repo/pull-requests/123
  if (/bitbucket\.org\/.+\/pull-requests\/\d+/.test(url)) {
    return url.split("?")[0];
  }

  return null;
}

let overlay: JustPROverlay | null = null;
let currentUrl = "";

function init(): void {
  const prUrl = detectPRUrl();
  if (!prUrl) return;

  if (prUrl === currentUrl && overlay) return;

  // Clean up previous instance (SPA navigation)
  if (overlay) {
    overlay.destroy();
    overlay = null;
  }

  currentUrl = prUrl;
  overlay = new JustPROverlay(prUrl);
}

// Initial load
init();

// Handle SPA navigation (GitHub uses pushState heavily)
let lastHref = window.location.href;
const observer = new MutationObserver(() => {
  if (window.location.href !== lastHref) {
    lastHref = window.location.href;
    init();
  }
});

observer.observe(document.body, { childList: true, subtree: true });
