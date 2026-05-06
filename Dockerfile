# Multi-stage build for the TypeScript backend.
# Runtime listens on PORT (default 8080).
# Configure secrets and env (GITHUB_TOKEN, ANTHROPIC_API_KEY, etc.) in the task definition.
#
# ECS Fargate: image platform must match the task CpuArchitecture. For X86_64 tasks, build and push:
#   docker buildx build --platform linux/amd64 -t <ecr-uri>:tag --push .
# Apple-silicon builds without --platform are often linux/arm64; use TaskCpuArchitecture=ARM64 in ecs-mickey.yaml.

# ── Stage 1: install dependencies ────────────────────────────────────────────
FROM node:22-bookworm-slim AS deps

WORKDIR /app

COPY backend-ts/package.json backend-ts/package-lock.json ./
# Force linux platform so optional native binaries (Agent SDK CLI) are
# installed for the container OS, not the build machine's OS.
RUN npm ci --legacy-peer-deps \
    --os=linux --cpu=x64 \
    --ignore-scripts

# ── Stage 2: build (transpile check + prune dev deps) ────────────────────────
FROM node:22-bookworm-slim AS builder

WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY backend-ts/ ./

# Type-check only — we run with tsx at runtime (no separate compile step needed)
RUN npx tsc --noEmit

# tsx is a devDependency but needed at runtime for transpilation.
# Keep it — the node_modules bulk is dominated by the Agent SDK binary anyway.

# ── Stage 3: runtime ──────────────────────────────────────────────────────────
FROM node:22-bookworm-slim AS runtime

# Install git — required by shallowClone() for repo context
RUN apt-get update && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy pruned node_modules + source
COPY --from=builder /app/node_modules ./node_modules
COPY backend-ts/src ./src
COPY backend-ts/package.json ./
COPY backend-ts/tsconfig.json ./

USER node

ENV PORT=8080
ENV NODE_ENV=production
EXPOSE 8080

# tsx runs TypeScript directly without a separate compile step
ENTRYPOINT ["node", "--import", "tsx/esm", "src/server.ts"]
