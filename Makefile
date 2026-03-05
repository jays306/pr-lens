BUN := $(shell command -v bun 2>/dev/null || echo ~/.bun/bin/bun)

.PHONY: help backend extension dev clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ─── Backend ──────────────────────────────────────────────────────────────────

backend-deps: ## Download Go dependencies
	cd backend && go mod tidy

backend-build: ## Build the Go backend binary
	cd backend && go build -o bin/just-pr-backend ./...

backend-run: ## Run the backend (loads .env)
	cd backend && go run main.go

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

clean: ## Remove build artifacts
	rm -rf backend/bin extension/dist
