.PHONY: help dev dev-api dev-web build build-web build-go build-all run clean \
       docker-build docker-up docker-down lint test

BINARY  := testhooks
WEB_DIR := web
DIST    := dist

# OS/arch matrix for cross-compilation.
PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64

## —— Help ————————————————————————————————————————————————————————
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

## —— Development —————————————————————————————————————————————————
dev: ## Run Go backend + Vite dev server concurrently (Ctrl+C stops both)
	@echo "Starting dev servers..."
	@echo "  Go  → http://localhost:8080  (API + WS + reverse proxy to Vite)"
	@echo "  Vite → http://localhost:5173  (SPA hot-reload)"
	@echo ""
	@trap 'kill 0' EXIT; \
		cd $(WEB_DIR) && npm run dev & \
		sleep 1 && DEV=true go run ./cmd/$(BINARY) & \
		wait

dev-api: ## Run only the Go backend in dev mode (proxies / to Vite on :5173)
	DEV=true go run ./cmd/$(BINARY)

dev-web: ## Run only the Vite dev server
	cd $(WEB_DIR) && npm run dev

## —— Build ———————————————————————————————————————————————————————
build: build-web build-go ## Full production build (SPA + Go binary)
	@echo ""
	@echo "Build complete: ./$(BINARY) ($(shell du -h $(BINARY) | cut -f1) — single binary with embedded SPA)"

build-web: ## Build the SPA into web/dist/
	cd $(WEB_DIR) && npm ci && npm run build

build-go: ## Build the Go binary (assumes web/dist/ exists)
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/$(BINARY)

build-all: build-web ## Cross-compile for all OS/arch combinations
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		out=$(DIST)/$(BINARY)-$$os-$$arch$$ext; \
		echo "  Building $$out ..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -ldflags="-s -w" -o $$out ./cmd/$(BINARY) || exit 1; \
	done
	@echo ""
	@echo "All binaries written to $(DIST)/:"
	@ls -lh $(DIST)/

run: ## Run the compiled binary
	./$(BINARY)

## —— Docker ——————————————————————————————————————————————————————
docker-build: ## Build the Docker image
	docker compose build

docker-up: ## Start app + Postgres via Docker Compose (builds first)
	docker compose up --build

docker-down: ## Stop containers and remove volumes
	docker compose down -v

## —— Quality —————————————————————————————————————————————————————
lint: ## Run Go vet + Svelte check
	go vet ./...
	cd $(WEB_DIR) && npm run check

test: ## Run Go tests
	go test ./...

## —— Cleanup —————————————————————————————————————————————————————
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf $(DIST)
	rm -rf $(WEB_DIR)/dist
	rm -rf $(WEB_DIR)/node_modules
	rm -rf $(WEB_DIR)/.svelte-kit
