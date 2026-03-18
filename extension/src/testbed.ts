import { JustPROverlay } from "./overlay";

let overlay = new JustPROverlay("https://github.com/owner/repo/pull/42");

// Allow the test page to swap the PR URL without a full reload
document.addEventListener("just-pr-reinit", (e) => {
  const url = (e as CustomEvent<{ url: string }>).detail.url;
  overlay.destroy();
  overlay = new JustPROverlay(url);
});
