.PHONY: build build-linux-amd64 build-darwin-amd64 build-darwin-arm64 build-linux-arm64 run dev test test-v test-e2e test-e2e-live lint clean help release release-snapshot release-check release-manual release-upload

# Project configuration
PROJECT_NAME := warded
VERSION := $(shell git describe --tags --always | sed 's/-g/-/')
GIT_COMMIT := $(shell git rev-parse --short=8 HEAD)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_VERSION := $(shell go version | awk '{print $$3}')
GIT_BRANCH := $(shell git branch --show-current)

# Build ID: version (git describe with 'g' prefix removed)
BUILD_ID := $(VERSION)

# Build flags
LDFLAGS := -ldflags "\
	 -X main.Version=$(VERSION) \
	 -X main.BuildDate=$(BUILD_DATE) \
	 -X main.GitCommit=$(GIT_COMMIT) \
	 -X main.GoVersion=$(GO_VERSION) \
	 -s -w"

# ── Build ──────────────────────────────────────────────
build:
	go build $(LDFLAGS) -o bin/$(PROJECT_NAME) ./cmd/warded

build-linux-amd64:
	@mkdir -p bin/linux_amd64
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/linux_amd64/$(PROJECT_NAME) ./cmd/warded

build-darwin-amd64:
	@mkdir -p bin/darwin_amd64
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/darwin_amd64/$(PROJECT_NAME) ./cmd/warded

build-darwin-arm64:
	@mkdir -p bin/darwin_arm64
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/darwin_arm64/$(PROJECT_NAME) ./cmd/warded

build-linux-arm64:
	@mkdir -p bin/linux_arm64
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/linux_arm64/$(PROJECT_NAME) ./cmd/warded

# ── Run ────────────────────────────────────────────────
# Usage: make run ARGS="new --commit --site=global"
run:
	go run ./cmd/warded $(ARGS)


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

# ── Manual Release ─────────────────────────────────────
# Builds release packages and generates checksums for manual upload
# Upload to: https://downloads.warded.me/releases/{version}/
# Upload to: https://downloads.warded.cn/releases/{version}/
release-manual: clean build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64
	@mkdir -p dist
	@echo "Creating release archives (version + git hash)..."
	@tar -czf dist/$(PROJECT_NAME)_$(BUILD_ID)_linux_amd64.tar.gz -C bin/linux_amd64 $(PROJECT_NAME)
	@tar -czf dist/$(PROJECT_NAME)_$(BUILD_ID)_linux_arm64.tar.gz -C bin/linux_arm64 $(PROJECT_NAME)
	@tar -czf dist/$(PROJECT_NAME)_$(BUILD_ID)_darwin_amd64.tar.gz -C bin/darwin_amd64 $(PROJECT_NAME)
	@tar -czf dist/$(PROJECT_NAME)_$(BUILD_ID)_darwin_arm64.tar.gz -C bin/darwin_arm64 $(PROJECT_NAME)
	@echo "Generating checksums..."
	@cd dist && sha256sum *.tar.gz > checksums.txt
	@echo "Creating latest.txt..."
	@echo "$(BUILD_ID)" > dist/latest.txt
	@echo ""
	@echo "Release packages ready in dist/"
	@echo "Version:  $(VERSION)"
	@echo "Build ID: $(BUILD_ID)"
	@ls -la dist/

# ── Release Upload (R2) ────────────────────────────────
release-upload: release-manual
	@echo "Detected branch: $(GIT_BRANCH)"
	@if [ "$(GIT_BRANCH)" = "dev" ]; then \
		echo "Uploading dist/* to R2 bucket 'downloads/dev/' ..."; \
		for f in dist/*; do \
			echo "  Uploading $$(basename $$f)"; \
			npx wrangler r2 object put --remote downloads/dev/$$(basename $$f) --file $$f; \
		done; \
	elif [ "$(GIT_BRANCH)" = "main" ]; then \
		echo "Uploading dist/* to R2 bucket 'downloads/releases/latest/' and 'downloads/$(VERSION)/' ..."; \
		for f in dist/*; do \
			echo "  Uploading $$(basename $$f) -> latest/"; \
			npx wrangler r2 object put --remote downloads/releases/latest/$$(basename $$f) --file $$f; \
			echo "  Uploading $$(basename $$f) -> $(VERSION)/"; \
			npx wrangler r2 object put --remote downloads/releases/$(VERSION)/$$(basename $$f) --file $$f; \
		done; \
	else \
		echo "Error: release-upload only allowed on 'dev' or 'main' branch (current: $(GIT_BRANCH))"; \
		exit 1; \
	fi
	@echo "Upload complete."

# ── Testing ────────────────────────────────────────────
test:
	go test ./... -count=1

test-v:
	go test ./... -v -count=1




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
	@echo "  build              Build warded CLI binary (current platform)"
	@echo "  build-linux-amd64  Build for Linux AMD64"
	@echo "  build-linux-arm64  Build for Linux ARM64"
	@echo "  build-darwin-amd64 Build for macOS Intel"
	@echo "  build-darwin-arm64 Build for macOS Apple Silicon"
	@echo ""
	@echo "Development:"
	@echo "  run              Run warded CLI locally"
	@echo ""
	@echo "Release (GoReleaser - CI/CD):"
	@echo "  release-snapshot Build release locally without publishing (for testing)"
	@echo "  release          Build and publish release to GitHub (requires GITHUB_TOKEN)"
	@echo "  release-check    Verify GoReleaser is installed"
	@echo ""
	@echo "Release (Manual):"
	@echo "  release-manual   Build all platform archives with unique build ID"
	@echo "                   Output: dist/{name}_{build-id}_{os}_{arch}.tar.gz"
	@echo "                   Output: dist/checksums.txt, dist/latest.txt"
	@echo "  release-upload   Build and upload to R2 (dev -> dev/, main -> latest/ + version/)"
	@echo ""
	@echo "Testing:"
	@echo "  test             Run unit tests"
	@echo "  test-v           Run unit tests with verbose output"
	@echo ""
	@echo "Maintenance:"
	@echo "  lint             Run go vet"
	@echo "  clean            Remove build artifacts"
	@echo ""
	@echo "Build Metadata:"
	@echo "  VERSION:    $(VERSION)"
	@echo "  BUILD_DATE: $(BUILD_DATE)"
	@echo "  GIT_COMMIT: $(GIT_COMMIT)"
	@echo "  GO_VERSION: $(GO_VERSION)"
