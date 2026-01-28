# Makefile for ai-terminal

BINARY_NAME=ai-terminal
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X github.com/runger/ai-terminal/internal/cmd.Version=$(VERSION) -X github.com/runger/ai-terminal/internal/cmd.GitCommit=$(GIT_COMMIT) -X github.com/runger/ai-terminal/internal/cmd.BuildDate=$(BUILD_DATE)"

.PHONY: all build install clean test fmt lint help

all: build

## build: Build the binary
build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/ai-terminal

## install: Install the binary to $GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/ai-terminal

## clean: Remove build artifacts
clean:
	rm -rf bin/
	go clean

## test: Run tests
test:
	go test -v ./...

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
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/ai-terminal
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/ai-terminal

build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/ai-terminal
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/ai-terminal

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/ai-terminal
