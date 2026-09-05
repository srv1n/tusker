BIN_NAME := tusker
CMD_DIR := ./cmd/tusker
DIST_DIR := dist
DIST_BIN := $(DIST_DIR)/$(BIN_NAME)
UI_DIR := internal/serve/ui
BIN_DIR ?= $(HOME)/.local/bin
MAC_APP_DIR ?= $(HOME)/Applications
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
RELEASE_VERSION ?= $(VERSION)
export RELEASE_VERSION
RELEASE_MATRIX ?= darwin/arm64 darwin/amd64 linux/arm64 linux/amd64
export RELEASE_MATRIX
SHA256_CMD := $(shell if command -v shasum >/dev/null 2>&1; then echo "shasum -a 256"; elif command -v sha256sum >/dev/null 2>&1; then echo "sha256sum"; fi)
GREEN := \033[32m
RESET := \033[0m
GO_MAX_PROCS ?= 2
GO_PACKAGE_PARALLELISM ?= 1
GO_TEST_PARALLELISM ?= 1
HOST_OS ?= $(shell uname -s)
VALIDATION_GATE := sh scripts/with-validation-lock.sh --

.DEFAULT_GOAL := help

.NOTPARALLEL: check-unlocked ui-check-unlocked
.PHONY: help build build-go build-go-unlocked require-macos mac-app mac-install mac-preview-install mac-uninstall mac-open mac-preview ui-install ui-test ui-build ui-check ui-check-unlocked fmt fmt-check test test-unlocked test-fast test-fast-unlocked vet vet-unlocked validate validate-unlocked check check-unlocked check-fast check-fast-unlocked release-test skill-doctor install install-cli install-bin install-user install-repo sync-repo-contract release-artifacts tag-release codebasezip codebase zip

help: ## Show available make targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ui-build ## Build fresh embedded UI assets and the local CLI
	$(MAKE) build-go

build-go: ## Build the Go binary from the prepared embedded assets
	$(VALIDATION_GATE) $(MAKE) build-go-unlocked

build-go-unlocked:
	@mkdir -p "$(DIST_DIR)"
	GOMAXPROCS=$(GO_MAX_PROCS) go build -trimpath -p=$(GO_PACKAGE_PARALLELISM) -ldflags "-s -w -X main.buildVersion=$(VERSION)" -o "$(DIST_BIN)" "$(CMD_DIR)"
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		for attempt in 1 2 3; do \
			xattr -d com.apple.provenance "$(DIST_BIN)" 2>/dev/null || true; \
			sleep 0.1; \
		done; \
	fi

require-macos:
	@test "$$(uname -s)" = "Darwin" || (echo "This target requires macOS." >&2; exit 1)

mac-app: require-macos build ## Build and sign TuskerBar with the current embedded CLI/Serve runtime
	apps/mac/TuskerBar/scripts/build-app.sh

mac-install: mac-app ## Install TuskerBar in ~/Applications (override with MAC_APP_DIR=...)
	MAC_APP_DIR="$(MAC_APP_DIR)" apps/mac/TuskerBar/scripts/install-app.sh

mac-uninstall: require-macos ## Remove TuskerBar from ~/Applications
	MAC_APP_DIR="$(MAC_APP_DIR)" apps/mac/TuskerBar/scripts/uninstall-app.sh

mac-open: require-macos ## Open the installed TuskerBar app
	open "$(MAC_APP_DIR)/TuskerBar.app"

mac-preview: require-macos install ## Install every local Tusker surface and open TuskerBar
	@echo "Tusker is open; the app starts or reuses its bundled daemon automatically."

mac-preview-install: mac-app
	MAC_PREVIEW=1 MAC_APP_DIR="$(MAC_APP_DIR)" apps/mac/TuskerBar/scripts/install-app.sh

ui-install: ## Install the pinned Serve UI dependency graph
	cd "$(UI_DIR)" && bun install --frozen-lockfile

ui-test: ui-install ## Test the Serve UI
	cd "$(UI_DIR)" && bun test

ui-build: ui-install ## Build the Serve UI assets embedded by the Go binary
	cd "$(UI_DIR)" && bun run build

ui-check: ## Run the complete Serve UI release gate
	$(VALIDATION_GATE) $(MAKE) ui-check-unlocked

ui-check-unlocked: ui-test ui-build

fmt: ## Format Go source files
	@files=$$(git ls-files -c -o --exclude-standard '*.go' | while IFS= read -r file; do [ -f "$$file" ] && printf '%s\n' "$$file"; done); \
	if [ -z "$$files" ]; then exit 0; fi; \
	gofmt -w $$files

fmt-check: ## Verify Go source is gofmt-formatted
	@files=$$(git ls-files -c -o --exclude-standard '*.go' | while IFS= read -r file; do [ -f "$$file" ] && printf '%s\n' "$$file"; done); \
	if [ -z "$$files" ]; then exit 0; fi; \
	unformatted=$$(gofmt -l $$files); \
	if [ -n "$$unformatted" ]; then \
		printf 'gofmt required:\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

test: ## Run Go tests
	$(VALIDATION_GATE) $(MAKE) test-unlocked

test-unlocked:
	GOMAXPROCS=$(GO_MAX_PROCS) go test -timeout=20m -p=$(GO_PACKAGE_PARALLELISM) -parallel=$(GO_TEST_PARALLELISM) ./...

test-fast: ## Run Go tests with t.Parallel-marked unit tests overlapped (Go-only fast loop)
	$(VALIDATION_GATE) $(MAKE) test-fast-unlocked

test-fast-unlocked:
	GOMAXPROCS=$(GO_MAX_PROCS) go test -timeout=20m -p=$(GO_PACKAGE_PARALLELISM) -parallel=4 ./...

vet: ## Run go vet
	$(VALIDATION_GATE) $(MAKE) vet-unlocked

vet-unlocked:
	GOMAXPROCS=$(GO_MAX_PROCS) go vet -p=$(GO_PACKAGE_PARALLELISM) ./...

validate: ## Run Tusker validation with branch-policy checks
	$(VALIDATION_GATE) $(MAKE) validate-unlocked

validate-unlocked:
	GOMAXPROCS=$(GO_MAX_PROCS) go run -p=$(GO_PACKAGE_PARALLELISM) ./cmd/tusker validate --branch-policy --json

skill-doctor: ## Run the strict project skill doctor
	go run ./cmd/tusker skill doctor --strict --json

check: ## Run the serialized UI + Go release-candidate gate
	$(VALIDATION_GATE) $(MAKE) check-unlocked

check-unlocked: ui-check-unlocked
	$(MAKE) fmt-check test-unlocked vet-unlocked validate-unlocked release-test build-go-unlocked

check-fast: ## Fast Go-only gate: fmt + vet + parallel tests + build (skips UI, validate, release-test; use check for release)
	$(VALIDATION_GATE) $(MAKE) check-fast-unlocked

check-fast-unlocked:
	$(MAKE) fmt-check vet-unlocked test-fast-unlocked build-go-unlocked

release-test: ## Run offline release/install integrity fixtures
	@set -eu; for test_script in scripts/tests/test-release-*.sh scripts/tests/test-mac-atomic-swap.sh; do sh "$$test_script"; done

install-bin: build ## Build/install binary and refresh existing root user skills
	./"$(DIST_BIN)" update --bin --bin-dir "$(BIN_DIR)"
	@printf "$(GREEN)OK install-bin complete$(RESET)\n"

install-cli: install-bin ## Alias for the cross-platform CLI install
	@:

install-user: build ## Build and install binary + Codex/Claude user skills
	./"$(DIST_BIN)" install --bin --codex-user --claude-user --bin-dir "$(BIN_DIR)" --force
	@printf "$(GREEN)OK install-user complete$(RESET)\n"

ifeq ($(HOST_OS),Darwin)
install: install-user mac-preview-install ## Install CLI/skills plus the macOS app and bundled daemon
else
install: install-user ## Install the cross-platform CLI and Codex/Claude user skills
endif
	@printf "$(GREEN)OK install complete$(RESET)\n"

install-repo: build ## Install repo-local skills into REPO=/abs/path
	@test -n "$(REPO)" || (echo "REPO is required: make install-repo REPO=/abs/path/to/repo" >&2; exit 1)
	./"$(DIST_BIN)" install --repo "$(REPO)" --bin-dir "$(BIN_DIR)" --force
	@printf "$(GREEN)OK install-repo complete$(RESET)\n"

sync-repo-contract: build ## Sync repo helper docs into REPO=/abs/path
	@test -n "$(REPO)" || (echo "REPO is required: make sync-repo-contract REPO=/abs/path/to/repo" >&2; exit 1)
	./"$(DIST_BIN)" sync-repo-contract --repo "$(REPO)"

release-artifacts: ## Build signed reproducible release artifacts from a clean trusted tag
	@scripts/release-build.sh

tag-release: ## Create annotated git tag RELEASE_VERSION=vX.Y.Z (does not push)
	@ scripts/release-validate.sh --check-only
	@git rev-parse --is-inside-work-tree >/dev/null 2>&1 || (echo "Not inside a git worktree" >&2; exit 1)
	@git rev-parse "$${RELEASE_VERSION}" >/dev/null 2>&1 && (echo "Tag $${RELEASE_VERSION} already exists" >&2; exit 1) || true
	@test -f scripts/release-tag-signer.asc || (echo "scripts/release-tag-signer.asc is required" >&2; exit 1)
	@test -n "$${RELEASE_TRUSTED_TAG_SIGNER:-}" || (echo "RELEASE_TRUSTED_TAG_SIGNER is required" >&2; exit 1)
	git tag -s "$${RELEASE_VERSION}" -m "Release $${RELEASE_VERSION}"
	@RELEASE_TRUSTED_TAG_SIGNER="$${RELEASE_TRUSTED_TAG_SIGNER}" scripts/release-verify-tag.sh "$${RELEASE_VERSION}"
	@printf "created local tag %s\n" "$${RELEASE_VERSION}"
	@printf "next: git push origin %s\n" "$${RELEASE_VERSION}"

ARTIFACTS_DIR ?= artifacts
CODEBASEZIP_NAME ?= tusker-codebase-$(shell date -u +%Y%m%dT%H%M%SZ).zip
CODEBASEZIP_PATH ?= $(ARTIFACTS_DIR)/$(CODEBASEZIP_NAME)

codebase: codebasezip ## Alias for codebasezip
	@:

zip: codebasezip ## Alias for codebasezip
	@:

codebasezip: ## Zip source and build/config files only into ARTIFACTS_DIR
	@python3 scripts/codebasezip.py --output "$(CODEBASEZIP_PATH)"
