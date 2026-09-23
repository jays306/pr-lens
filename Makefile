BUN := $(shell command -v bun 2>/dev/null || echo ~/.bun/bin/bun)

.PHONY: help backend extension dev clean cli review

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ─── Backend ──────────────────────────────────────────────────────────────────

backend-deps: ## Download Go dependencies
	cd backend && go mod tidy

backend-build: ## Build the Go backend binary
	cd backend && go build -o bin/pr-lens-backend .

backend-run: ## Run the backend (loads .env)
	cd backend && go run .

cli-build: ## Build the pr-lens CLI
	cd backend && go build -o bin/pr-lens ./cmd/pr-lens

review: ## Review a PR: make review URL=https://github.com/owner/repo/pull/123
	cd backend && go run ./cmd/pr-lens $(URL) $(FLAGS)

backend-test: ## Run backend tests
	cd backend && go test ./...

## ─── Extension ────────────────────────────────────────────────────────────────

extension-deps: ## Install extension dependencies via Bun
	cd extension && $(BUN) install

extension-build: ## Build the Chrome extension
	cd extension && $(BUN) build.ts

extension-dev: ## Watch mode for extension development
	cd extension && NODE_ENV=development $(BUN) --watch build.ts

## ─── Combined ─────────────────────────────────────────────────────────────────

setup: backend-deps extension-deps ## Install all dependencies
	@echo ""
	@echo "Setup complete. Next steps:"
	@echo "  1. cp backend/.env.example backend/.env"
	@echo "  2. Fill in GITHUB_TOKEN and ANTHROPIC_API_KEY in backend/.env"
	@echo "  3. make dev"

dev: extension-build ## Build extension + run backend
	@echo "Extension built. Load extension/dist in Chrome (chrome://extensions → Load unpacked)"
	$(MAKE) backend-run

## ─── Deploy ───────────────────────────────────────────────────────────────────

ECR_IMAGE    ?= 200033215230.dkr.ecr.us-east-1.amazonaws.com/mickey/pr-lens:latest
AWS_REGION   ?= us-east-1
AWS_PROFILE  ?= vibe-sandbox
ECS_CLUSTER  ?= mickey
ECS_SERVICE  ?= pr-lens-v2

docker-login: ## Authenticate Docker with ECR
	aws ecr get-login-password --profile $(AWS_PROFILE) --region $(AWS_REGION) | \
	  docker login --username AWS --password-stdin 200033215230.dkr.ecr.us-east-1.amazonaws.com

docker-build: ## Build linux/amd64 image and push to ECR
	docker buildx build --platform linux/amd64 -t $(ECR_IMAGE) . --push

cfn-deploy: ## Deploy CloudFormation stack (picks up ecs-mickey.yaml changes)
	AWS_PROFILE=$(AWS_PROFILE) TASK_CPU_ARCH=X86_64 \
	  VPC_ID=vpc-00ee6870649e4e463 \
	  SUBNET_OVERRIDES="subnet-0f2bf9e872c5dee5c,subnet-0b9768d4d82db3bfe" \
	  ./infra/deploy-ecs.sh

deploy-ecs: ## Force new ECS deployment (pull latest image, no CFN changes)
	aws ecs update-service --cluster $(ECS_CLUSTER) --service $(ECS_SERVICE) \
	  --force-new-deployment --profile $(AWS_PROFILE) --region $(AWS_REGION) \
	  --query 'service.deployments[0].{status:status,running:runningCount,desired:desiredCount}' \
	  --output table

deploy: docker-login docker-build cfn-deploy ## Build, push image, and update CloudFormation stack

deploy-ecs-only: docker-login docker-build deploy-ecs

clean: ## Remove build artifacts
	rm -rf backend/bin extension/dist
