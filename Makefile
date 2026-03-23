# Makefile for clai
# Single source of truth for "my code is clean" — hooks and CI delegate here.

# Ensure Rust toolchain is discoverable (cargo, rustc, etc.)
export PATH := $(HOME)/.cargo/bin:$(PATH)

BINARY_NAME=clai
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X github.com/runger/clai/internal/cmd.Version=$(VERSION) -X github.com/runger/clai/internal/cmd.GitCommit=$(GIT_COMMIT) -X github.com/runger/clai/internal/cmd.BuildDate=$(BUILD_DATE)"
PICKER_LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE)"

# Pinned tool versions — keep in sync with CI (.github/workflows/ci.yml)
GOLANGCI_LINT_VER?=v2.10.1
GOVULNCHECK_VER?=v1.1.4

# Suppression budgets — ratchet down over time
NOLINT_BUDGET?=137
NOSEC_BUDGET?=3

.PHONY: all build install install-dev setup clean help
.PHONY: fmt format go-fmt rust-fmt go-fmt-check rust-fmt-check
.PHONY: lint go-lint rust-lint budgets
.PHONY: test go-test test-rust cover
.PHONY: vuln go-vuln rust-vuln
.PHONY: gitleaks semgrep sonar
.PHONY: pre-commit check dev roam
.PHONY: test-interactive test-docker test-server test-server-stop test-server-status test-e2e test-e2e-shell
.PHONY: proto deps run
.PHONY: build-all build-linux build-darwin build-windows bin/linux

TEST_SHELL?=bash
PORT?=8080
ADDRESS?=127.0.0.1
E2E_SHELL?=bash
E2E_SHELLS?=bash zsh fish
E2E_PLANS?=tests/e2e/example-test-plan.yaml,tests/e2e/suggestions-tests.yaml
E2E_GREP?=
E2E_OUT?=.tmp/e2e-runs
E2E_URL?=http://127.0.0.1:8080
E2E_REPORTER?=line

all: build

# =============================================================================
# Quality gates
# =============================================================================

## pre-commit: Fast local checks — formatting, lint, tests, secrets (~2-3 min)
pre-commit: fmt lint test gitleaks

## check: Full quality gate — pre-commit + vuln + SAST + sonar (pre-push)
check: pre-commit vuln semgrep sonar

## dev: Full dev suite — quality gate + roam
dev: check roam
	@echo "All checks passed!"

# =============================================================================
# Formatting (detect-only — no auto-fix)
# =============================================================================

## fmt: Check formatting for Go and Rust (detect-only)
fmt: go-fmt-check rust-fmt-check

go-fmt-check:
	@command -v goimports >/dev/null 2>&1 || (echo "goimports not installed (run: make install-dev)" && exit 1)
	@UNFORMATTED=$$(goimports -l .); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "goimports: unformatted files:"; \
		echo "$$UNFORMATTED"; \
		exit 1; \
	fi

rust-fmt-check:
	@if [ -d "clai-wrap" ] && command -v cargo >/dev/null 2>&1; then \
		cargo fmt --manifest-path clai-wrap/Cargo.toml --check; \
	fi

## format: Auto-fix formatting for Go and Rust
format: go-fmt rust-fmt

go-fmt:
	goimports -w .

rust-fmt:
	@if [ -d "clai-wrap" ] && command -v cargo >/dev/null 2>&1; then \
		cargo fmt --manifest-path clai-wrap/Cargo.toml; \
	fi

# =============================================================================
# Linting
# =============================================================================

## lint: Run all linters (Go + Rust + suppression budgets)
lint: go-lint rust-lint budgets

go-lint:
	@command -v golangci-lint >/dev/null 2>&1 || (echo "golangci-lint not installed (run: make install-dev)" && exit 1)
	golangci-lint run

rust-lint:
	@if [ -d "clai-wrap" ] && command -v cargo >/dev/null 2>&1; then \
		cargo clippy --manifest-path clai-wrap/Cargo.toml --all-targets -- -D warnings; \
	fi

## budgets: Enforce suppression directive budgets (nolint/nosec ratchet)
budgets:
	@nolint_count="$$( (git grep -n '//nolint' -- '*.go' || true) | wc -l | tr -d '[:space:]' )"; \
	nosec_count="$$( (git grep -n '#nosec' -- '*.go' || true) | wc -l | tr -d '[:space:]' )"; \
	echo "//nolint: $${nolint_count} (budget: $(NOLINT_BUDGET))"; \
	echo "#nosec:  $${nosec_count} (budget: $(NOSEC_BUDGET))"; \
	if [ "$${nolint_count}" -gt "$(NOLINT_BUDGET)" ]; then \
		echo "Error: //nolint count exceeded budget."; \
		exit 1; \
	fi; \
	if [ "$${nosec_count}" -gt "$(NOSEC_BUDGET)" ]; then \
		echo "Error: #nosec count exceeded budget."; \
		exit 1; \
	fi

# =============================================================================
# Testing
# =============================================================================

## test: Run all tests (Go + Rust)
test: go-test test-rust

## go-test: Run Go tests with race detector
go-test:
	@if command -v gotestsum >/dev/null 2>&1; then \
		gotestsum --format testdox -- -race -short ./...; \
	else \
		go test -race -short -v ./...; \
	fi

## test-rust: Run clai-wrap Rust tests
test-rust:
	@if [ -d "clai-wrap" ] && command -v cargo >/dev/null 2>&1; then \
		cargo test --manifest-path clai-wrap/Cargo.toml; \
	else \
		echo "Skipping Rust tests (cargo or clai-wrap not found)"; \
	fi

## cover: Run Go tests with coverage report
cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## test-interactive: Run interactive shell tests (requires zsh, bash, fish)
test-interactive:
	@if command -v gotestsum >/dev/null 2>&1; then \
		gotestsum --format testdox -- -v ./tests/expect/...; \
	else \
		go test -v ./tests/expect/...; \
	fi

# =============================================================================
# Security
# =============================================================================

## vuln: Scan for known vulnerabilities (Go + Rust)
vuln: go-vuln rust-vuln

go-vuln:
	@command -v govulncheck >/dev/null 2>&1 || (echo "govulncheck not installed (run: make install-dev)" && exit 1)
	govulncheck ./...

rust-vuln:
	@if [ -d "clai-wrap" ] && command -v cargo >/dev/null 2>&1; then \
		if command -v cargo-audit >/dev/null 2>&1; then \
			cargo audit --file clai-wrap/Cargo.lock; \
		else \
			echo "cargo-audit not installed, skipping Rust vuln scan"; \
		fi \
	fi

## gitleaks: Scan staged changes for leaked secrets
gitleaks:
	@if command -v gitleaks >/dev/null 2>&1; then \
		gitleaks protect --staged --no-banner; \
	else \
		echo "warning: gitleaks not installed, skipping secret scan"; \
	fi

## semgrep: SAST scan with semgrep
semgrep:
	@if command -v semgrep >/dev/null 2>&1; then \
		semgrep scan --config auto --error --quiet --exclude='testdata' --exclude='.beads' .; \
	else \
		echo "warning: semgrep not installed, skipping SAST scan"; \
	fi

## sonar: Run SonarQube analysis
sonar:
	@if ! command -v sonar-scanner >/dev/null 2>&1; then \
		echo "sonar-scanner not installed, skipping"; \
	elif [ ! -f .env ]; then \
		echo ".env missing, skipping sonar scan"; \
	else \
		TOKEN=$$(grep -E '^SONAR_TOKEN=[A-Za-z0-9_]+$$' .env | cut -d= -f2); \
		if [ -z "$$TOKEN" ]; then \
			echo "error: SONAR_TOKEN not found or invalid in .env"; exit 1; \
		fi; \
		SONAR_TOKEN="$$TOKEN" sonar-scanner -Dsonar.qualitygate.wait=true; \
	fi

# =============================================================================
# External analysis
# =============================================================================

## roam: Run roam architectural checks (fitness + pr-risk)
roam:
	@if command -v roam >/dev/null 2>&1; then \
		roam index && roam fitness && roam pr-risk main..HEAD; \
	else \
		echo "roam not installed, skipping roam checks..."; \
	fi

# =============================================================================
# Build targets
# =============================================================================

## build: Build all binaries (clai, claid, clai-shim, clai-picker)
build:
	go build $(LDFLAGS) -o bin/clai ./cmd/clai
	go build $(LDFLAGS) -o bin/claid ./cmd/claid
	go build $(LDFLAGS) -o bin/clai-shim ./cmd/clai-shim
	go build $(PICKER_LDFLAGS) -o bin/clai-picker ./cmd/clai-picker

## install: Install all binaries to $GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/clai
	go install $(LDFLAGS) ./cmd/claid
	go install $(LDFLAGS) ./cmd/clai-shim
	go install $(PICKER_LDFLAGS) ./cmd/clai-picker

## build-all: Build for all platforms
build-all: build-linux build-darwin build-windows

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/clai
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/clai

build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/clai
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/clai

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/clai

# =============================================================================
# Setup & utilities
# =============================================================================

## setup: Set up development environment (install tools + git hooks)
setup: install-dev
	@bash scripts/install-hooks.sh
	@echo "Development environment ready."

## install-dev: Install development tool dependencies
install-dev:
	@echo "Installing Go tools..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VER)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VER)
	go install golang.org/x/tools/cmd/goimports@latest
	go install golang.org/x/tools/cmd/deadcode@latest
	go install gotest.tools/gotestsum@v1.12.1
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "Installing Rust tools..."
	@if command -v cargo >/dev/null 2>&1; then \
		cargo install cargo-audit --quiet; \
	else \
		echo "cargo not found, skipping Rust tools"; \
	fi
	@echo "Done! Run 'make setup' to install git hooks."

## proto: Generate Go code from protobuf definitions
proto:
	@echo "Generating protobuf code..."
	@if ! command -v protoc >/dev/null 2>&1; then \
		echo "Error: protoc not found. Please install the protobuf compiler. See: https://grpc.io/docs/protoc-installation/"; \
		exit 1; \
	fi
	@mkdir -p gen
	protoc --go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		-I proto \
		proto/clai/v1/clai.proto
	@echo "Generated code in gen/"

## deps: Download dependencies
deps:
	go mod download
	go mod tidy

## run: Build and run with arguments
run: build
	./bin/$(BINARY_NAME) $(ARGS)

## clean: Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html
	go clean

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

# =============================================================================
# Docker & E2E testing
# =============================================================================

## bin/linux: Cross-compile binaries and test runner for Linux (used by Docker tests)
bin/linux:
	@mkdir -p bin/linux
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/linux/clai ./cmd/clai
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/linux/claid ./cmd/claid
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/linux/clai-shim ./cmd/clai-shim
	GOOS=linux GOARCH=amd64 go build $(PICKER_LDFLAGS) -o bin/linux/clai-picker ./cmd/clai-picker
	GOOS=linux GOARCH=amd64 go test -c -o bin/linux/expect.test ./tests/expect
	@tmpdir=$$(mktemp -d) && \
		cd "$$tmpdir" && \
		go mod init temp && \
		go get gotest.tools/gotestsum@latest && \
		GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(CURDIR)/bin/linux/gotestsum gotest.tools/gotestsum && \
		rm -rf "$$tmpdir"

## test-docker: Run interactive tests in Docker containers
test-docker: bin/linux
	@set -e; \
	if command -v docker-compose >/dev/null 2>&1; then \
		compose_cmd="docker-compose -f tests/docker/docker-compose.yml"; \
	elif command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then \
		compose_cmd="docker compose -f tests/docker/docker-compose.yml"; \
	else \
		echo "Error: docker-compose or docker compose not found"; \
		exit 1; \
	fi; \
	$$compose_cmd build; \
	for svc in alpine ubuntu debian fedora; do \
		echo "==> Running docker expect tests in $$svc"; \
		$$compose_cmd run --rm $$svc expect.test -test.v -test.parallel=1; \
	done

## test-server: Start gotty-backed terminal server for browser e2e tests
test-server:
	@test_shell="$(if $(filter bash zsh fish,$(SHELL)),$(SHELL),$(TEST_SHELL))"; \
	TEST_SHELL="$$test_shell" PORT="$(PORT)" ADDRESS="$(ADDRESS)" ./scripts/start-test-server.sh

## test-server-stop: Stop gotty-backed terminal server
test-server-stop:
	@./scripts/stop-test-server.sh

## test-server-status: Show status of gotty-backed terminal server
test-server-status:
	@./scripts/stop-test-server.sh --status

## test-e2e: Run gotty+Playwright e2e suite for bash/zsh/fish and aggregate results
test-e2e:
	@E2E_SHELLS="$(E2E_SHELLS)" \
	E2E_PLANS="$(E2E_PLANS)" \
	E2E_GREP="$(E2E_GREP)" \
	E2E_OUT="$(E2E_OUT)" \
	E2E_URL="$(E2E_URL)" \
	E2E_REPORTER="$(E2E_REPORTER)" \
	./scripts/run-e2e-suite.sh

## test-e2e-shell: Run gotty+Playwright e2e suite for one shell (set E2E_SHELL=bash|zsh|fish)
test-e2e-shell:
	@E2E_SHELLS="$(E2E_SHELL)" \
	E2E_PLANS="$(E2E_PLANS)" \
	E2E_GREP="$(E2E_GREP)" \
	E2E_OUT="$(E2E_OUT)" \
	E2E_URL="$(E2E_URL)" \
	E2E_REPORTER="$(E2E_REPORTER)" \
	./scripts/run-e2e-suite.sh
