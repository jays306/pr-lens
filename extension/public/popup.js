const DEFAULT_BACKEND_URL = "http://localhost:8080";

const dot     = document.getElementById("statusDot");
const text    = document.getElementById("statusText");
const urlInput = document.getElementById("urlInput");
const saveBtn  = document.getElementById("saveBtn");
const hint     = document.getElementById("hint");

// ── Load saved URL ─────────────────────────────────────────────
chrome.storage.local.get("backendUrl", (result) => {
  const saved = result["backendUrl"] || DEFAULT_BACKEND_URL;
  urlInput.value = saved;
  checkHealth(saved);
});

// ── Health check ───────────────────────────────────────────────
function checkHealth(baseUrl) {
  dot.className = "status-dot checking";
  text.textContent = "Checking…";

  const url = baseUrl.replace(/\/$/, "");
  fetch(`${url}/health`, { signal: AbortSignal.timeout(2500) })
    .then((r) => {
      if (r.ok) {
        dot.className = "status-dot";
        text.textContent = "Backend connected";
      } else {
        throw new Error(`HTTP ${r.status}`);
      }
    })
    .catch(() => {
      dot.className = "status-dot offline";
      text.textContent = "Backend offline";
    });
}

// ── Save URL ───────────────────────────────────────────────────
saveBtn.addEventListener("click", () => {
  const raw = urlInput.value.trim();

  if (!raw) {
    urlInput.classList.add("invalid");
    hint.textContent = "Please enter a URL.";
    return;
  }

  try { new URL(raw); } catch {
    urlInput.classList.add("invalid");
    hint.textContent = "Enter a valid URL, e.g. http://localhost:8080";
    return;
  }

  urlInput.classList.remove("invalid");
  const clean = raw.replace(/\/$/, "");

  chrome.storage.local.set({ backendUrl: clean }, () => {
    saveBtn.textContent = "Saved ✓";
    saveBtn.classList.add("saved");
    hint.textContent = "URL saved. Changes take effect on the next page load.";
    checkHealth(clean);

    setTimeout(() => {
      saveBtn.textContent = "Save";
      saveBtn.classList.remove("saved");
      hint.textContent = "Used for all analysis and review requests.";
    }, 2500);
  });
});

// ── Re-check health on Enter ───────────────────────────────────
urlInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") saveBtn.click();
});
