BIN_NAME := tusker
CMD_DIR := ./cmd/tusker
DIST_DIR := dist
DIST_BIN := $(DIST_DIR)/$(BIN_NAME)
BIN_DIR ?= $(HOME)/.local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
RELEASE_VERSION ?= $(VERSION)
RELEASES_DIR := $(DIST_DIR)/releases/$(RELEASE_VERSION)
RELEASE_MATRIX ?= darwin/arm64 darwin/amd64 linux/arm64 linux/amd64
SHA256_CMD := $(shell if command -v shasum >/dev/null 2>&1; then echo "shasum -a 256"; elif command -v sha256sum >/dev/null 2>&1; then echo "sha256sum"; fi)
GREEN := \033[32m
RESET := \033[0m
CHECK := ✓

.DEFAULT_GOAL := help

.PHONY: help build fmt-check test vet validate check install install-bin install-user install-repo sync-repo-contract release-artifacts tag-release docs-export docs-dev docs-build docs-check codebasezip

help: ## Show available make targets
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the local CLI into dist/tusker
	@mkdir -p "$(DIST_DIR)"
	go build -o "$(DIST_BIN)" "$(CMD_DIR)"

test: ## Run Go tests
	go test ./...

vet: ## Run go vet
	go vet ./...

fmt-check: ## Verify Go source is gofmt-formatted
	@files=$$(git ls-files -c -o --exclude-standard '*.go' | while IFS= read -r file; do [ -f "$$file" ] && printf '%s\n' "$$file"; done); \
	if [ -z "$$files" ]; then exit 0; fi; \
	unformatted=$$(gofmt -l $$files); \
	if [ -n "$$unformatted" ]; then \
		printf 'gofmt required:\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

validate: ## Run Tusker validation with branch-policy checks
	go run ./cmd/tusker validate --branch-policy --json

check: fmt-check test vet validate build ## Run format, tests, vet, validation, and build

install-bin: build ## Build/install binary and refresh existing root user skills
	./"$(DIST_BIN)" update --bin-dir "$(BIN_DIR)"
	@printf "$(GREEN)$(CHECK) install-bin complete$(RESET)\n"

install-user: build ## Build and install binary + Codex/Claude user skills
	./"$(DIST_BIN)" install --codex-user --claude-user --bin-dir "$(BIN_DIR)" --force
	@printf "$(GREEN)$(CHECK) install-user complete$(RESET)\n"

install: install-user ## Default local developer install
	@printf "$(GREEN)$(CHECK) install complete$(RESET)\n"

install-repo: build ## Install repo-local skills into REPO=/abs/path
	@test -n "$(REPO)" || (echo "REPO is required: make install-repo REPO=/abs/path/to/repo" >&2; exit 1)
	./"$(DIST_BIN)" install --repo "$(REPO)" --bin-dir "$(BIN_DIR)" --force
	@printf "$(GREEN)$(CHECK) install-repo complete$(RESET)\n"

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
		CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH go build -o "$$STAGE/$(BIN_NAME)$$EXT" "$(CMD_DIR)"; \
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

CODEBASEZIP_NAME ?= tusker-codebase.zip
CODEBASEZIP_PATH ?= $(CURDIR)/$(CODEBASEZIP_NAME)
CODEBASEZIP_EXT_PATTERN := \.(go|rs|js|jsx|mjs|cjs|ts|tsx|css|scss|less|html|htm|json|yml|yaml|toml)$$|(^|/)Makefile$|(^|/)go\.mod$|(^|/)go\.sum$

codebasezip: ## Zip code-only source files; use scripts/package-code-review.sh for docs/skills review
	@tmp_list=$$(mktemp); \
	git ls-files -c -o --exclude-standard -z | tr '\0' '\n' > "$$tmp_list".all; \
	grep -E "$(CODEBASEZIP_EXT_PATTERN)" "$$tmp_list".all | sort -u > "$$tmp_list".filtered; \
	if [ ! -s "$$tmp_list".filtered ]; then \
		rm -f "$$tmp_list".all "$$tmp_list".filtered; \
		echo "No source files matched the filter. Refusing to write empty zip." >&2; \
		exit 1; \
	fi; \
	rm -f "$(CODEBASEZIP_PATH)"; \
	rm -f "$$tmp_list".all; \
	zip -q -r -D "$(CODEBASEZIP_PATH)" -@ < "$$tmp_list".filtered; \
	rm -f "$$tmp_list".filtered; \
	echo "Created $(CODEBASEZIP_PATH)"; \
	echo "Code-only archive. For docs/skills review use: ./scripts/package-code-review.sh"
