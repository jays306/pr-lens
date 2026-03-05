/**
 * Bun build script for the JUST-PR Chrome extension.
 * Embeds the CSS as a string into content.js so it can be injected
 * synchronously into the Shadow DOM — no async loading, no timing issues.
 */
import { readFileSync, mkdirSync, writeFileSync } from "fs";
import { join } from "path";

const outdir = join(import.meta.dir, "dist");
mkdirSync(outdir, { recursive: true });

// 1. Read CSS and generate a module that exports it as a string
const css = readFileSync(join(import.meta.dir, "src/styles/overlay.css"), "utf8");
const cssModule = join(import.meta.dir, "src/__generated_css.ts");
writeFileSync(cssModule, `export const overlayCSS = ${JSON.stringify(css)};\n`);

// 2. Bundle TypeScript content script (which imports the generated CSS module)
const result = await Bun.build({
  entrypoints: [join(import.meta.dir, "src/content.ts")],
  outdir,
  target: "browser",
  minify: process.env.NODE_ENV === "production",
});

if (!result.success) {
  console.error("Build failed:");
  for (const msg of result.logs) {
    console.error(msg);
  }
  process.exit(1);
}

console.log("✓ Built extension to dist/");
console.log("  → dist/content.js (CSS embedded)");
