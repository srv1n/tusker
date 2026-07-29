BIN_NAME := tusker
CMD_DIR := ./cmd/tusker
DIST_DIR := dist
DIST_BIN := $(DIST_DIR)/$(BIN_NAME)
UI_DIR := internal/serve/ui
BIN_DIR ?= $(HOME)/.local/bin
MAC_APP_DIR ?= $(HOME)/Applications
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
RELEASE_VERSION ?= $(VERSION)
RELEASES_DIR := $(DIST_DIR)/releases/$(RELEASE_VERSION)
RELEASE_MATRIX ?= darwin/arm64 darwin/amd64 linux/arm64 linux/amd64
SHA256_CMD := $(shell if command -v shasum >/dev/null 2>&1; then echo "shasum -a 256"; elif command -v sha256sum >/dev/null 2>&1; then echo "sha256sum"; fi)
GREEN := \033[32m
RESET := \033[0m
GO_MAX_PROCS ?= 2
GO_PACKAGE_PARALLELISM ?= 1
GO_TEST_PARALLELISM ?= 1
VALIDATION_GATE := sh scripts/with-validation-lock.sh --

.DEFAULT_GOAL := help

.NOTPARALLEL: check-unlocked ui-check-unlocked
.PHONY: help build build-go build-go-unlocked mac-app mac-install mac-uninstall mac-open mac-preview ui-install ui-test ui-build ui-check ui-check-unlocked fmt fmt-check test test-unlocked vet vet-unlocked validate validate-unlocked check check-unlocked skill-doctor install install-bin install-user install-repo sync-repo-contract release-artifacts tag-release docs-export docs-dev docs-build docs-check codebasezip codebase zip

help: ## Show available make targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ui-build ## Build fresh embedded UI assets and the local CLI
	$(MAKE) build-go

build-go: ## Build the Go binary from the prepared embedded assets
	$(VALIDATION_GATE) $(MAKE) build-go-unlocked

build-go-unlocked:
	@mkdir -p "$(DIST_DIR)"
	GOMAXPROCS=$(GO_MAX_PROCS) go build -p=$(GO_PACKAGE_PARALLELISM) -ldflags "-X main.buildVersion=$(VERSION)" -o "$(DIST_BIN)" "$(CMD_DIR)"
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		for attempt in 1 2 3; do \
			xattr -d com.apple.provenance "$(DIST_BIN)" 2>/dev/null || true; \
			sleep 0.1; \
		done; \
	fi

mac-app: build ## Build and sign TuskerBar with the current embedded CLI/Serve runtime
	apps/mac/TuskerBar/scripts/build-app.sh

mac-install: mac-app ## Install TuskerBar in ~/Applications (override with MAC_APP_DIR=...)
	MAC_APP_DIR="$(MAC_APP_DIR)" apps/mac/TuskerBar/scripts/install-app.sh

mac-uninstall: ## Remove TuskerBar from ~/Applications
	MAC_APP_DIR="$(MAC_APP_DIR)" apps/mac/TuskerBar/scripts/uninstall-app.sh

mac-open: ## Open the installed TuskerBar app
	open "$(MAC_APP_DIR)/TuskerBar.app"

mac-preview: install ## Build, install, and open the self-starting Tusker Mac app
	@echo "Tusker is open; the app starts or reuses its bundled daemon automatically."

ui-install: ## Install the pinned Serve UI dependency graph
	cd "$(UI_DIR)" && bun install --frozen-lockfile

ui-test: ui-install ## Typecheck and test the Serve UI
	cd "$(UI_DIR)" && bun run typecheck
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
	$(MAKE) fmt-check test-unlocked vet-unlocked validate-unlocked build-go-unlocked

install-bin: build ## Build/install binary and refresh existing root user skills
	./"$(DIST_BIN)" update --bin-dir "$(BIN_DIR)"
	@printf "$(GREEN)OK install-bin complete$(RESET)\n"

install-user: build ## Build and install binary + Codex/Claude user skills
	./"$(DIST_BIN)" install --codex-user --claude-user --bin-dir "$(BIN_DIR)" --force
	@printf "$(GREEN)OK install-user complete$(RESET)\n"

install: install-user mac-install ## Install the CLI/skills and the TuskerBar app
	@printf "$(GREEN)OK install complete$(RESET)\n"

install-repo: build ## Install repo-local skills into REPO=/abs/path
	@test -n "$(REPO)" || (echo "REPO is required: make install-repo REPO=/abs/path/to/repo" >&2; exit 1)
	./"$(DIST_BIN)" install --repo "$(REPO)" --bin-dir "$(BIN_DIR)" --force
	@printf "$(GREEN)OK install-repo complete$(RESET)\n"

sync-repo-contract: build ## Sync repo helper docs into REPO=/abs/path
	@test -n "$(REPO)" || (echo "REPO is required: make sync-repo-contract REPO=/abs/path/to/repo" >&2; exit 1)
	./"$(DIST_BIN)" sync-repo-contract --repo "$(REPO)"

release-artifacts: build ## Build tar.gz release artifacts under dist/releases/$(RELEASE_VERSION)
	@test -n "$(SHA256_CMD)" || (echo "Need shasum or sha256sum on PATH" >&2; exit 1)
	@rm -rf "$(RELEASES_DIR)"
	@mkdir -p "$(RELEASES_DIR)"
	@set -e; \
	for target in $(RELEASE_MATRIX); do \
		GOOS=$${target%/*}; \
		GOARCH=$${target#*/}; \
		STEM="$(BIN_NAME)_$(RELEASE_VERSION)_$${GOOS}_$${GOARCH}"; \
		STAGE="$(RELEASES_DIR)/$$STEM"; \
		EXT=""; \
		if [ "$$GOOS" = "windows" ]; then EXT=".exe"; fi; \
		mkdir -p "$$STAGE"; \
		CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH go build -ldflags "-X main.buildVersion=$(RELEASE_VERSION)" -o "$$STAGE/$(BIN_NAME)$$EXT" "$(CMD_DIR)"; \
		cp README.md LICENSE "$$STAGE"/; \
		tar -C "$(RELEASES_DIR)" -czf "$(RELEASES_DIR)/$$STEM.tar.gz" "$$STEM"; \
		rm -rf "$$STAGE"; \
		printf "built %s\n" "$$STEM.tar.gz"; \
	done
	@cd "$(RELEASES_DIR)" && $(SHA256_CMD) *.tar.gz > checksums.txt
	@printf "release artifacts: %s\n" "$(RELEASES_DIR)"

tag-release: ## Create annotated git tag RELEASE_VERSION=vX.Y.Z (does not push)
	@test -n "$(RELEASE_VERSION)" || (echo "RELEASE_VERSION is required, e.g. make tag-release RELEASE_VERSION=v0.1.0" >&2; exit 1)
	@git rev-parse --is-inside-work-tree >/dev/null 2>&1 || (echo "Not inside a git worktree" >&2; exit 1)
	@git rev-parse "$(RELEASE_VERSION)" >/dev/null 2>&1 && (echo "Tag $(RELEASE_VERSION) already exists" >&2; exit 1) || true
	git tag -a "$(RELEASE_VERSION)" -m "Release $(RELEASE_VERSION)"
	@printf "created local tag %s\n" "$(RELEASE_VERSION)"
	@printf "next: git push origin %s\n" "$(RELEASE_VERSION)"

docs-export: build ## Reindex/export published docs into the local site
	./"$(DIST_BIN)" docs export --site ./site

docs-dev: build ## Run the docs export and start the local docs dev server
	./"$(DIST_BIN)" docs dev --site ./site --watch

docs-build: build ## Export docs and build the static docs site
	./"$(DIST_BIN)" docs build --site ./site

docs-check: build ## Validate the vault and build the docs pipeline end-to-end
	./"$(DIST_BIN)" validate
	./"$(DIST_BIN)" docs build --site ./site

ARTIFACTS_DIR ?= artifacts
CODEBASEZIP_NAME ?= tusker-codebase-$(shell date -u +%Y%m%dT%H%M%SZ).zip
CODEBASEZIP_PATH ?= $(ARTIFACTS_DIR)/$(CODEBASEZIP_NAME)

codebase: codebasezip ## Alias for codebasezip
	@:

zip: codebasezip ## Alias for codebasezip
	@:

codebasezip: ## Zip reviewable repository files into ARTIFACTS_DIR
	@mkdir -p "$(ARTIFACTS_DIR)"
	@tmp_list=$$(mktemp); \
	find . \
		\( \
			-path './.git' -o \
			-path './.tools' -o \
			-path './.tusker/events' -o \
			-path './.tusker/attempts' -o \
			-path './.tusker/_generated' -o \
			-path './.tusker/_runtime' -o \
			-path './.tusker/scratch' -o \
			-path './.tusker/workspaces' -o \
			-path './.tusker-runtime' -o \
			-path './.tusker-state' -o \
			-path './.tusker-worktrees' -o \
			-path './artifacts' -o \
			-path './build' -o \
			-path './coverage' -o \
			-path './dist' -o \
			-path './node_modules' -o \
			-path './out' -o \
			-path './site/.astro' -o \
			-path './site/dist' -o \
			-path './site/node_modules' -o \
			-path './tmp' -o \
			-path './tusker/.tusker' -o \
			-path './tusker/events' -o \
			-path './tusker/attempts' -o \
			-path './tusker/_generated' -o \
			-path './tusker/evidence/*/artifacts' -o \
			-path './.tusker/evidence/*/artifacts' -o \
			-path './vendor' -o \
			-name 'node_modules' -o \
			-name 'dist' -o \
			-name '.vite' -o \
			-name '.astro' -o \
			-name '.cache' -o \
			-name '.turbo' -o \
			-name '.next' -o \
			-name '.svelte-kit' \
		\) -prune -o \
		-type f \
		! -name '.DS_Store' \
		! -name '*.bin' \
		! -name '*.dmg' \
		! -name '*.exe' \
		! -name '*.log' \
		! -name '*.mov' \
		! -name '*.mp4' \
		! -name '*.out' \
		! -name '*.pkg' \
		! -name '*.prof' \
		! -name '*.tar' \
		! -name '*.tar.gz' \
		! -name '*.tgz' \
		! -name '*.webm' \
		! -name '*.zip' \
		-print | sed 's#^\./##' | sort > "$$tmp_list"; \
	if [ ! -s "$$tmp_list" ]; then \
		rm -f "$$tmp_list"; \
		echo "No files matched the codebase archive filter." >&2; \
		exit 1; \
	fi; \
	rm -f "$(CODEBASEZIP_PATH)"; \
	zip -q -r -D "$(CODEBASEZIP_PATH)" -@ < "$$tmp_list"; \
	file_count=$$(wc -l < "$$tmp_list" | tr -d ' '); \
	rm -f "$$tmp_list"; \
	echo "Created $(CODEBASEZIP_PATH) ($$file_count files)"; \
	if [ "$${CI:-}" != "true" ] && command -v open >/dev/null 2>&1; then \
		open "$(ARTIFACTS_DIR)" >/dev/null 2>&1 || true; \
	fi
