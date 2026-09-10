# ==============================================================================
# capagent Makefile — Container Capability Engine
# ==============================================================================

GO ?= go
BIN_DIR ?= bin
DIST_DIR ?= dist
COVERAGE_FILE ?= coverage.out
COVERAGE_THRESHOLD ?= 95.0
MODULE_PATH ?= github.com/EpicBlackWolfZ/capagent
HOST_ARCH ?= $(shell $(GO) env GOARCH 2>/dev/null || echo "amd64")

# Version metadata
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS ?= -s -w \
  -X $(MODULE_PATH)/internal/version.Version=$(VERSION) \
  -X $(MODULE_PATH)/internal/version.Commit=$(COMMIT) \
  -X $(MODULE_PATH)/internal/version.Date=$(DATE) \
  -X $(MODULE_PATH)/internal/version.BuiltBy=makefile \
  -X $(MODULE_PATH)/internal/version.Vendor=EpicBlackWolfZ

# CLI Tool Detection
GOTESTSUM := $(shell command -v gotestsum 2> /dev/null)
GOLANGCI_LINT := $(shell command -v golangci-lint 2> /dev/null)
GOVULNCHECK := $(shell command -v govulncheck 2> /dev/null)
GORELEASER := $(shell command -v goreleaser 2> /dev/null)

.PHONY: all help build test coverage lint vuln vuln-optional deps-microfat snapshot tidy clean

all: check-mod lint vuln coverage build ## Run strict verification pipeline (module check, lint, vuln, race coverage, build)

## help: Display available targets
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all            Run module verification, strict lint/vuln, race coverage, and build"
	@echo "  build          Compile universal fat binary for host arch via GoReleaser snapshot"
	@echo "  test           Run unit tests with race detection"
	@echo "  coverage       Run unit tests with race detection and verify coverage (>= 95.0%)"
	@echo "  lint           Run required golangci-lint (fails if missing)"
	@echo "  vuln           Run govulncheck vulnerability scanner (strict; fails if missing)"
	@echo "  vuln-optional  Run govulncheck vulnerability scanner (warning fallback if missing)"
	@echo "  deps-microfat  Ensure microfat CLI and architecture stubs are present"
	@echo "  snapshot       Run GoReleaser snapshot build and fat binary packaging"
	@echo "  release-check  Verify full/minimal binaries, release archives, SBOMs, and provenance"
	@echo "  build-contracts Run shell/workflow validators and build-security contracts"
	@echo "  hardening-regressions Run bounded fault/resource regressions and verify their report"
	@echo "  hardening-stress Run the longer replayable fault/resource profile"
	@echo "  tidy           Run go mod tidy and go mod verify"
	@echo "  clean          Remove build artifacts, test outputs, and coverage files"

## tidy: Format dependencies and verify go.mod
tidy:
	@echo "==> Tidying Go module..."
	@$(GO) mod tidy
	@$(GO) mod verify

## build: Compile capagent universal fat binary for host architecture via GoReleaser snapshot mode
build: deps-microfat
	@echo "==> Building capagent via GoReleaser snapshot mode..."
ifdef GORELEASER
	@goreleaser build --snapshot --clean
	@echo "==> Assembling universal fat binaries..."
	@./scripts/bundle-fat.sh full
	@mkdir -p $(BIN_DIR)
	@cp $(DIST_DIR)/fat/full/$(HOST_ARCH)/capagent $(BIN_DIR)/capagent
	@echo "✔ Successfully built capagent [$(VERSION)] (microfat universal binary) -> $(BIN_DIR)/capagent"
else
	@echo "❌ Error: 'goreleaser' is required for building capagent."
	@echo "   Install GoReleaser from https://goreleaser.com/install/"
	@exit 1
endif

## test: Run unit tests with race detection
test:
	@echo "==> Running unit tests with race detector..."
ifdef GOTESTSUM
	@gotestsum -- -race ./...
else
	@$(GO) test -race ./...
endif

## coverage: Run tests with coverage profile, output metrics, and verify total coverage (>= 95.0%)
coverage:
	@echo "==> Running tests with coverage..."
ifdef GOTESTSUM
	@gotestsum -- -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
else
	@$(GO) test -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
endif
	@echo "==> Coverage summary:"
	@$(GO) tool cover -func=$(COVERAGE_FILE)
	@python3 -B scripts/check-coverage.py $(COVERAGE_FILE) $(COVERAGE_THRESHOLD)

## lint: Run strict golangci-lint check
lint:
	@echo "==> Running linter..."
ifdef GOLANGCI_LINT
	@golangci-lint run ./...
else
	@echo "Error: golangci-lint is required; install the pinned version from CI."
	@exit 1
endif

## vuln: Run govulncheck vulnerability scanner (fails if tool missing)
vuln:
	@echo "==> Running vulnerability check..."
ifdef GOVULNCHECK
	@govulncheck ./...
else
	@echo "❌ Error: 'govulncheck' not found in PATH."
	@echo "   Install govulncheck via: go install golang.org/x/vuln/cmd/govulncheck@latest"
	@exit 1
endif

## vuln-optional: Run govulncheck if installed; warn and continue if missing
vuln-optional:
	@echo "==> Running vulnerability check (optional)..."
ifdef GOVULNCHECK
	@govulncheck ./...
else
	@echo "⚠️  WARNING: govulncheck not found in PATH; skipping check."
endif

## deps-microfat: Ensure microfat CLI and architecture stubs are present
deps-microfat:
	@echo "==> Ensuring microfat dependencies..."
	@./scripts/ensure-microfat.sh

## snapshot: Run GoReleaser snapshot build and fat binary packaging
snapshot: build

## clean: Remove generated artifacts and coverage files
clean:
	@echo "==> Cleaning artifacts..."
	@rm -rf $(BIN_DIR) $(DIST_DIR) $(COVERAGE_FILE)

.PHONY: check-mod release-check build-contracts

## check-mod: Verify dependency integrity without silently changing tracked files
check-mod:
	@$(GO) mod tidy -diff
	@$(GO) mod verify

## release-check: Build and validate full/minimal artifacts without signing or publishing
release-check:
	@./scripts/release-check.sh

## build-contracts: Validate shell/workflow semantics and build security regressions
build-contracts:
	@for script in scripts/*.sh; do bash -n "$$script"; done
	@shellcheck scripts/*.sh
	@actionlint
	@$(GO) test -race ./tests/contract

.PHONY: hardening-regressions hardening-stress

hardening-regressions:
	@python3 -B scripts/hardening.py run --profile regressions

hardening-stress:
	@python3 -B scripts/hardening.py run --profile stress
