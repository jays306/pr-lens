const DEFAULT_BACKEND_URL = "http://localhost:8080";

const dot = document.getElementById("statusDot");
const text = document.getElementById("statusText");
const urlInput = document.getElementById("urlInput");
const tokenInput = document.getElementById("tokenInput");
const saveBtn = document.getElementById("saveBtn");
const hint = document.getElementById("hint");

// ── Load saved settings ─────────────────────────────────────
chrome.storage.local.get(["backendUrl", "githubToken"], (result) => {
  const saved = result["backendUrl"] || DEFAULT_BACKEND_URL;
  urlInput.value = saved;
  if (result["githubToken"]) {
    tokenInput.placeholder = "•••••••• (saved — enter new to replace)";
  }
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

// ── Save URL + optional token ──────────────────────────────────
saveBtn.addEventListener("click", () => {
  const raw = urlInput.value.trim();

  if (!raw) {
    urlInput.classList.add("invalid");
    hint.textContent = "Please enter a backend URL.";
    return;
  }

  try {
    new URL(raw);
  } catch {
    urlInput.classList.add("invalid");
    hint.textContent = "Enter a valid URL, e.g. http://localhost:8080";
    return;
  }

  urlInput.classList.remove("invalid");
  const clean = raw.replace(/\/$/, "");

  const token = tokenInput.value.trim();
  const hasExistingToken = tokenInput.placeholder.startsWith("••••");

  const finishSave = (tokenWasSaved) => {
    saveBtn.textContent = "Saved ✓";
    saveBtn.classList.add("saved");
    hint.textContent = "Saved. Reload GitHub PR pages for token changes to apply.";
    if (tokenWasSaved) {
      tokenInput.value = "";
      tokenInput.placeholder = "•••••••• (saved — enter new to replace)";
    } else if (!hasExistingToken) {
      tokenInput.placeholder = "GitHub PAT (ghp_…)";
    }
    checkHealth(clean);

    setTimeout(() => {
      saveBtn.textContent = "Save settings";
      saveBtn.classList.remove("saved");
      hint.textContent =
        "Backend URL is used for all requests. Token is sent as Bearer auth; leave blank if the server sets GITHUB_TOKEN.";
    }, 2800);
  };

  chrome.storage.local.set({ backendUrl: clean }, () => {
    if (token) {
      // New token entered — save it.
      chrome.storage.local.set({ githubToken: token }, () => finishSave(true));
    } else if (hasExistingToken) {
      // Field left blank but a token was already saved — keep it.
      finishSave(false);
    } else {
      // No token, none saved — clear any remnant.
      chrome.storage.local.remove("githubToken", () => finishSave(false));
    }
  });
});

// ── Re-check health on Enter in URL field ───────────────────
urlInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") saveBtn.click();
});
tokenInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") saveBtn.click();
});
