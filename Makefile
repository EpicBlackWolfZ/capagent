# ==============================================================================
# capagent Makefile — Container Capability Engine
# ==============================================================================

GO ?= go
COVERAGE_FILE ?= coverage.out
MODULE_PATH ?= github.com/EpicBlackWolfZ/capagent

# CLI Tool Detection
GOTESTSUM := $(shell command -v gotestsum 2> /dev/null)
GOLANGCI_LINT := $(shell command -v golangci-lint 2> /dev/null)

.PHONY: all build test coverage lint tidy clean help

all: tidy lint test

## help: Display available targets
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all        Run tidy, lint, and test"
	@echo "  test       Run unit tests with race detection"
	@echo "  coverage   Run unit tests with race detection and report code coverage"
	@echo "  lint       Run golangci-lint (with warning fallback to go vet)"
	@echo "  tidy       Run go mod tidy and go mod verify"
	@echo "  clean      Remove test artifacts and coverage files"

## tidy: Format dependencies and verify go.mod
tidy:
	@echo "==> Tidying Go module..."
	@$(GO) mod tidy
	@$(GO) mod verify

## test: Run unit tests with race detector
test:
	@echo "==> Running tests with race detection..."
ifdef GOTESTSUM
	@gotestsum -- -race ./...
else
	@$(GO) test -race ./...
endif

## coverage: Run tests with coverage profile and output metrics
coverage:
	@echo "==> Running tests with coverage..."
ifdef GOTESTSUM
	@gotestsum -- -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
else
	@$(GO) test -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
endif
	@echo "==> Coverage summary:"
	@$(GO) tool cover -func=$(COVERAGE_FILE)

## lint: Run strict golangci-lint check
lint:
	@echo "==> Running linter..."
ifdef GOLANGCI_LINT
	@golangci-lint run ./...
else
	@echo "⚠️  WARNING: golangci-lint not found in PATH."
	@echo "⚠️  Falling back to 'go vet' (REDUCED VALIDATION: staticcheck, errcheck, mnd, lll not checked)."
	@echo "⚠️  Install golangci-lint to enforce the full repository quality baseline."
	@$(GO) vet ./...
endif

## clean: Remove generated coverage and temporary files
clean:
	@echo "==> Cleaning artifacts..."
	@rm -f $(COVERAGE_FILE)
