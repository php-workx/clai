# Makefile for clai

BINARY_NAME=clai
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X github.com/runger/clai/internal/cmd.Version=$(VERSION) -X github.com/runger/clai/internal/cmd.GitCommit=$(GIT_COMMIT) -X github.com/runger/clai/internal/cmd.BuildDate=$(BUILD_DATE)"
PICKER_LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE)"

.PHONY: all build install install-dev clean test test-all test-interactive test-docker test-server test-server-stop test-server-status cover fmt lint vuln roam dev help proto bin/linux

TEST_SHELL?=bash
PORT?=8080
ADDRESS?=127.0.0.1

all: build

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

## install-dev: Install development dependencies
install-dev:
	@echo "Installing Go tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install golang.org/x/tools/cmd/deadcode@latest
	go install gotest.tools/gotestsum@v1.12.1
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "Installing pre-commit..."
	@if command -v pipx >/dev/null 2>&1; then \
		pipx install pre-commit || pipx upgrade pre-commit; \
	elif command -v pip3 >/dev/null 2>&1; then \
		pip3 install --user pre-commit; \
	elif command -v pip >/dev/null 2>&1; then \
		pip install --user pre-commit; \
	else \
		echo "Error: pip/pipx not found. Install Python first."; \
		exit 1; \
	fi
	@echo "Installing pre-commit hooks..."
	pre-commit install
	@echo "Done! Development environment ready."

## clean: Remove build artifacts
clean:
	rm -rf bin/
	go clean

## test: Run all tests with race detector
test:
	@if command -v gotestsum >/dev/null 2>&1; then \
		gotestsum --format testdox -- -race ./...; \
	else \
		go test -race -v ./...; \
	fi

## test-all: Run all tests including Docker containers
test-all: test test-docker

## cover: Run all tests with coverage
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

## test-docker: Run interactive tests in Docker containers (Alpine, Ubuntu, Debian, Fedora) sequentially
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

## fmt: Format code
fmt:
	go fmt ./...

## lint: Run linter
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "Error: golangci-lint not installed."; \
		echo "Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		echo "Or run: make install-dev"; \
		exit 1; \
	fi

## vuln: Scan for vulnerabilities
vuln:
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "Error: govulncheck not installed."; \
		echo "Install with: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
		echo "Or run: make install-dev"; \
		exit 1; \
	fi

## roam: Run roam architectural checks (fitness + pr-risk)
roam:
	@if command -v roam >/dev/null 2>&1; then \
		roam index && roam fitness && roam pr-risk main..HEAD; \
	else \
		echo "roam not installed, skipping roam checks..."; \
	fi

## dev: Run all checks (fmt, lint, test, vuln, roam)
dev: fmt lint test vuln roam
	@echo "All checks passed!"

## deps: Download dependencies
deps:
	go mod download
	go mod tidy

## run: Build and run with arguments
run: build
	./bin/$(BINARY_NAME) $(ARGS)

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

# Cross-compilation targets
.PHONY: build-all build-linux build-darwin build-windows

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
