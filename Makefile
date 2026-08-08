# clauditor — portable Makefile (GNU make on Linux and macOS).
# No GNU-only shell flags; tools resolved from PATH with local fallbacks.

BINARY      := clauditor
MODULE      := github.com/mjraval/clauditor
BIN_DIR     := bin
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X $(MODULE)/internal/version.Version=$(VERSION)

# Prefer PATH, fall back to the local dev-setup locations used by `make setup`.
GO          := $(shell command -v go 2>/dev/null || echo $(HOME)/.local/go/bin/go)
GOLANGCI    := $(shell command -v golangci-lint 2>/dev/null || echo $(HOME)/.local/bin/golangci-lint)
SHELLCHECK  := $(shell command -v shellcheck 2>/dev/null || echo $(HOME)/.local/bin/shellcheck)

UNAME_S     := $(shell uname -s)

.PHONY: all build test lint lint-sh vet fmt tidy clean run-serve run-status doctor setup install help

all: build test lint

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

build: ## Build ./bin/clauditor (static, version-stamped)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) ./cmd/$(BINARY)

test: ## Run all tests (offline; uses test/stubbin fakes)
	$(GO) test ./...

vet: ## go vet
	$(GO) vet ./...

lint: vet ## golangci-lint + shellcheck
	$(GOLANGCI) run ./...
	@$(MAKE) lint-sh

lint-sh: ## shellcheck all shell scripts
	@if [ -x "$(SHELLCHECK)" ] || command -v "$(SHELLCHECK)" >/dev/null 2>&1; then \
		$(SHELLCHECK) scripts/*.sh test/stubbin/claude test/stubbin/tmux; \
	else \
		echo "shellcheck not found — run 'make setup'"; exit 1; \
	fi

fmt: ## gofmt all Go files
	$(GO) fmt ./...

tidy: ## go mod tidy
	$(GO) mod tidy

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out

run-serve: build ## Build and run the daemon in dev-insecure mode
	$(BIN_DIR)/$(BINARY) serve --dev-insecure-local

run-status: build ## Build and run status
	$(BIN_DIR)/$(BINARY) status

doctor: build ## Build and run environment checks
	$(BIN_DIR)/$(BINARY) doctor

install: build ## Install to ~/.local/bin
	mkdir -p $(HOME)/.local/bin
	cp $(BIN_DIR)/$(BINARY) $(HOME)/.local/bin/

setup: ## One-time dev setup: Go toolchain + golangci-lint + shellcheck into ~/.local
	@command -v go >/dev/null 2>&1 || [ -x $(HOME)/.local/go/bin/go ] || { \
		echo "installing Go..."; \
		V=$$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -1); \
		case "$(UNAME_S)" in \
			Darwin) OS=darwin ;; \
			*)      OS=linux ;; \
		esac; \
		case "$$(uname -m)" in \
			arm64|aarch64) ARCH=arm64 ;; \
			*)             ARCH=amd64 ;; \
		esac; \
		mkdir -p $(HOME)/.local; \
		curl -fsSL "https://go.dev/dl/$$V.$$OS-$$ARCH.tar.gz" | tar -xz -C $(HOME)/.local; \
		echo "Go installed to ~/.local/go — add ~/.local/go/bin to PATH"; }
	@command -v golangci-lint >/dev/null 2>&1 || [ -x $(HOME)/.local/bin/golangci-lint ] || { \
		echo "installing golangci-lint..."; \
		GOBIN=$(HOME)/.local/bin $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest; }
	@command -v shellcheck >/dev/null 2>&1 || [ -x $(HOME)/.local/bin/shellcheck ] || \
		echo "shellcheck not found — install it: 'brew install shellcheck' (mac) or 'apt install shellcheck' (linux), or drop a binary in ~/.local/bin"
	@echo "setup complete"
