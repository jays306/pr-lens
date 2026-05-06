import "dotenv/config";
import { Hono } from "hono";
import { cors } from "hono/cors";
import { serve } from "@hono/node-server";
import { Cache } from "./cache/cache.js";
import { makeAnalyzeHandler } from "./handler/analyze.js";
import { handleReview } from "./handler/review.js";

const PORT = parseInt(process.env.PORT ?? "8080", 10);
const CORS_ORIGINS = process.env.CORS_ORIGINS ?? "*";
const GITHUB_TOKEN = process.env.GITHUB_TOKEN ?? "";

const cache = new Cache(24 * 60 * 60 * 1000);

const app = new Hono();

app.use(
  "*",
  cors({
    origin: CORS_ORIGINS === "*" ? "*" : CORS_ORIGINS.split(",").map((s) => s.trim()),
    allowMethods: ["GET", "POST", "OPTIONS"],
    allowHeaders: ["Content-Type", "Authorization", "X-GitHub-Token"],
  }),
);

app.get("/health", (c) => c.json({ status: "ok" }));
app.post("/analyze", makeAnalyzeHandler(cache, GITHUB_TOKEN));
app.post("/review", (c) => handleReview(c, GITHUB_TOKEN));

serve({ fetch: app.fetch, port: PORT }, () => {
  console.log(`PR-LENS TypeScript backend listening on :${PORT}`);
  console.log(`Using AI provider: claude-agent-sdk (cwd mode)`);
});
