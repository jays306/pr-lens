import { JustPROverlay, setBackendUrl, setGithubToken } from "./overlay";

const DEFAULT_BACKEND_URL = "http://localhost:8080";

function detectPRUrl(): string | null {
  const url = window.location.href;
  if (/^https:\/\/(www\.)?github\.com\/[^/]+\/[^/]+\/pull\/\d+/.test(url)) {
    return url.split("?")[0];
  }
  return null;
}

let overlay: JustPROverlay | null = null;
let currentUrl = "";

function init(backendUrl: string, githubToken: string): void {
  setBackendUrl(backendUrl);
  setGithubToken(githubToken);

  const prUrl = detectPRUrl();
  if (!prUrl) {
    if (overlay) {
      overlay.destroy();
      overlay = null;
      currentUrl = "";
    }
    return;
  }

  if (prUrl === currentUrl && overlay) return;

  if (overlay) {
    overlay.destroy();
    overlay = null;
  }

  currentUrl = prUrl;
  overlay = new JustPROverlay(prUrl);
}

function bootFromStorage(): void {
  chrome.storage.local.get(["backendUrl", "githubToken"], (result) => {
    init(result["backendUrl"] || DEFAULT_BACKEND_URL, result["githubToken"] || "");
  });
}

chrome.storage.local.get(["backendUrl", "githubToken"], (result) => {
  init(result["backendUrl"] || DEFAULT_BACKEND_URL, result["githubToken"] || "");
});

chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== "local") return;
  if (changes["backendUrl"]) {
    setBackendUrl(changes["backendUrl"].newValue || DEFAULT_BACKEND_URL);
  }
  if (changes["githubToken"]) {
    setGithubToken(changes["githubToken"].newValue || "");
  }
});

let lastHref = window.location.href;
const checkNav = (): void => {
  if (window.location.href === lastHref) return;
  lastHref = window.location.href;
  bootFromStorage();
};

window.addEventListener("popstate", checkNav);
setInterval(checkNav, 400);
