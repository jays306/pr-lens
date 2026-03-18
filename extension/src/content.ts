import { JustPROverlay, setBackendUrl } from "./overlay";

const DEFAULT_BACKEND_URL = "http://localhost:8080";

function detectPRUrl(): string | null {
  const url = window.location.href;

  if (/github\.com\/.+\/pull\/\d+/.test(url)) return url.split("?")[0];
  if (/gitlab\.com\/.+\/-\/merge_requests\/\d+/.test(url)) return url.split("?")[0];
  if (/bitbucket\.org\/.+\/pull-requests\/\d+/.test(url)) return url.split("?")[0];

  return null;
}

let overlay: JustPROverlay | null = null;
let currentUrl = "";

function init(backendUrl: string): void {
  setBackendUrl(backendUrl);

  const prUrl = detectPRUrl();
  if (!prUrl) return;

  if (prUrl === currentUrl && overlay) return;

  if (overlay) {
    overlay.destroy();
    overlay = null;
  }

  currentUrl = prUrl;
  overlay = new JustPROverlay(prUrl);
}

// Load backend URL from storage then boot, re-init on storage changes
chrome.storage.local.get("backendUrl", (result) => {
  const url: string = result["backendUrl"] || DEFAULT_BACKEND_URL;
  init(url);
});

chrome.storage.onChanged.addListener((changes, area) => {
  if (area === "local" && changes["backendUrl"]) {
    const url: string = changes["backendUrl"].newValue || DEFAULT_BACKEND_URL;
    setBackendUrl(url);
  }
});

// Handle SPA navigation (GitHub uses pushState heavily)
let lastHref = window.location.href;
const observer = new MutationObserver(() => {
  if (window.location.href !== lastHref) {
    lastHref = window.location.href;
    chrome.storage.local.get("backendUrl", (result) => {
      init(result["backendUrl"] || DEFAULT_BACKEND_URL);
    });
  }
});

observer.observe(document.body, { childList: true, subtree: true });
