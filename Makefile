BINARY_NAME=pg-monk
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: build run test clean fmt lint vet check snapshot install init-config help

## build: Build the binary
build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/pgmonk

## run: Build and run with optional ARGS
run: build
	./$(BINARY_NAME) $(ARGS)

## test: Run all tests
test:
	go test -race -v ./...

## fmt: Format code
fmt:
	go fmt ./...
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

## vet: Run go vet
vet:
	go vet ./...

## lint: Run linter (requires golangci-lint)
lint:
	golangci-lint run ./...

## check: Run all checks (fmt, vet, test, build)
check: fmt vet test build
	@echo "✓ All checks passed"

## snapshot: Build snapshot with goreleaser
snapshot:
	goreleaser release --snapshot --clean

## clean: Remove build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -rf dist/

## install: Install to GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/pgmonk

## init-config: Write a default config file
init-config: build
	./$(BINARY_NAME) init-config

## help: Show this help
help:
	@echo "Usage:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/  /'
