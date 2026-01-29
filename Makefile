# Makefile for clai

BINARY_NAME=clai
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X github.com/runger/clai/internal/cmd.Version=$(VERSION) -X github.com/runger/clai/internal/cmd.GitCommit=$(GIT_COMMIT) -X github.com/runger/clai/internal/cmd.BuildDate=$(BUILD_DATE)"

.PHONY: all build install install-dev clean test test-race cover fmt lint vuln dev help proto

all: build

## build: Build the binary
build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/clai

## install: Install the binary to $GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/clai

## proto: Generate Go code from protobuf definitions
proto:
	@echo "Generating protobuf code..."
	@if ! command -v protoc >/dev/null 2>&1; then \
		echo "Error: protoc not found. Install with: brew install protobuf"; \
		exit 1; \
	fi
	@mkdir -p gen/proto/clai/v1
	protoc --go_out=gen/proto/clai/v1 --go_opt=paths=source_relative \
		--go-grpc_out=gen/proto/clai/v1 --go-grpc_opt=paths=source_relative \
		-I proto/clai/v1 \
		proto/clai/v1/clai.proto
	@echo "Generated code in gen/proto/clai/v1/"

## install-dev: Install development dependencies
install-dev:
	@echo "Installing Go tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install golang.org/x/tools/cmd/goimports@latest
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

## test: Run unit tests (fast, skips integration tests)
test:
	go test -short -v ./...

## test-all: Run all tests including integration tests (slow, requires Claude CLI)
test-all:
	go test -v ./...

## test-race: Run unit tests with race detector
test-race:
	go test -short -race -v ./...

## cover: Run unit tests with coverage
cover:
	go test -short -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## fmt: Format code
fmt:
	go fmt ./...

## lint: Run linter
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

## vuln: Scan for vulnerabilities
vuln:
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "govulncheck not installed. Install with: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
	fi

## dev: Run all checks (fmt, lint, test, vuln)
dev: fmt lint test-race vuln
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
