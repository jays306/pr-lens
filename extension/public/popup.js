const BACKEND_URL = "http://localhost:8080";

const dot = document.getElementById("statusDot");
const text = document.getElementById("statusText");

fetch(`${BACKEND_URL}/health`, { signal: AbortSignal.timeout(2000) })
  .then((r) => {
    if (r.ok) {
      dot.classList.remove("offline");
      text.textContent = "Backend connected";
    } else {
      throw new Error();
    }
  })
  .catch(() => {
    dot.classList.add("offline");
    text.textContent = "Backend offline";
  });
