.PHONY: build build-linux-amd64 run dev test test-v test-e2e test-e2e-live lint clean help release release-snapshot release-check

VERSION ?= v0.2.0
ENV_FILE ?= .env

# ── Build ──────────────────────────────────────────────
build:
	go build -ldflags "-X main.Version=$(VERSION)" -o bin/warded ./cmd/warded

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=$(VERSION)" -o bin/warded ./cmd/warded

# ── Run ────────────────────────────────────────────────
# Usage: make run ARGS="activate --platform-origin http://127.0.0.1:6688"
run:
	@set -a; \
	if [ -f "$(ENV_FILE)" ]; then \
		. "$(ENV_FILE)"; \
	fi; \
	set +a; \
	go run ./cmd/warded $(ARGS)

# ── Dev (with hot reload via air) ──────────────────────
dev:
	@if command -v air >/dev/null 2>&1; then \
		set -a; \
		if [ -f "$(ENV_FILE)" ]; then \
			. "$(ENV_FILE)"; \
		fi; \
		set +a; \
		air; \
	else \
		echo "Error: air is not installed. Install with: go install github.com/air-verse/air@latest"; \
		exit 1; \
	fi

# ── Release (GoReleaser) ───────────────────────────────
# Requires GoReleaser: https://goreleaser.com/install/

# Check if GoReleaser is installed
release-check:
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "Error: goreleaser is not installed."; \
		echo "Install with: go install github.com/goreleaser/goreleaser/v2@latest"; \
		echo "Or visit: https://goreleaser.com/install/"; \
		exit 1; \
	fi

# Build release locally without publishing (for testing)
release-snapshot: release-check
	goreleaser release --snapshot --clean

# Build release locally and publish to GitHub
# Requires GITHUB_TOKEN environment variable
release: release-check
	@if [ -z "$(GITHUB_TOKEN)" ]; then \
		echo "Error: GITHUB_TOKEN environment variable is not set"; \
		echo "Set it with: export GITHUB_TOKEN=your_token_here"; \
		exit 1; \
	fi
	goreleaser release --clean

# ── Test ───────────────────────────────────────────────
test:
	go test ./... -count=1

test-v:
	go test ./... -v -count=1

# Runs local preflight tests only (no platform required).
test-e2e:
	go test ./internal/e2e/ -v -count=1

# Runs all e2e tests against a live platform API.
# Usage: make test-e2e-live PLATFORM_URL=https://dev.warded.me
test-e2e-live:
	go test ./internal/e2e/ -v -count=1 -platform-url=$(PLATFORM_URL)

# ── Lint ───────────────────────────────────────────────
lint:
	go vet ./...

# ── Clean ──────────────────────────────────────────────
clean:
	rm -rf bin/ dist/

# ── Help ───────────────────────────────────────────────
help:
	@echo "Warded CLI Makefile"
	@echo ""
	@echo "Build Targets:"
	@echo "  build            Build warded CLI binary"
	@echo "  build-linux-amd64 Build for Linux AMD64"
	@echo ""
	@echo "Development:"
	@echo "  run              Run warded CLI locally (loads $(ENV_FILE) when present)"
	@echo "  dev              Run with hot reload via air (loads $(ENV_FILE) when present)"
	@echo ""
	@echo "Release (GoReleaser):"
	@echo "  release-snapshot Build release locally without publishing (for testing)"
	@echo "  release          Build and publish release to GitHub (requires GITHUB_TOKEN)"
	@echo "  release-check    Verify GoReleaser is installed"
	@echo ""
	@echo "Testing:"
	@echo "  test             Run unit tests"
	@echo "  test-v           Run unit tests with verbose output"
	@echo "  test-e2e         Run local preflight e2e tests (no platform required)"
	@echo "  test-e2e-live    Run all e2e tests against a live platform (requires PLATFORM_URL)"
	@echo ""
	@echo "Maintenance:"
	@echo "  lint             Run go vet"
	@echo "  clean            Remove build artifacts"
	@echo ""
	@echo "GitHub Actions Release:"
	@echo "  1. Create and push a tag: git tag v0.2.0 && git push origin v0.2.0"
	@echo "  2. GitHub Actions will automatically build and create a draft release"
	@echo "  3. Go to GitHub Releases, write release notes, and publish"
