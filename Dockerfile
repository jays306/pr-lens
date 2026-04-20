# Multi-stage build for AWS ECS (Fargate or EC2). Runtime listens on PORT (default 8080).
# Configure secrets and env (GITHUB_TOKEN, ANTHROPIC_API_KEY, etc.) in the task definition.
#
# ECS Fargate: image platform must match the task CpuArchitecture. For X86_64 tasks, build and push:
#   docker buildx build --platform linux/amd64 -t <ecr-uri>:tag --push .
# Apple-silicon builds without --platform are often linux/arm64; use TaskCpuArchitecture=ARM64 in ecs-mickey.yaml (default).

FROM golang:1.26-bookworm AS builder

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./

# Default amd64 for plain `docker build`; buildx sets this per --platform (e.g. arm64 on Graviton).
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /pr-lens .

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

COPY --from=builder /pr-lens /pr-lens

USER nonroot:nonroot

ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/pr-lens"]
