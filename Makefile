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

.PHONY: all help build test coverage lint vuln deps-microfat snapshot tidy clean

all: tidy lint vuln test coverage build ## Run complete verification pipeline (tidy, lint, vuln, test, coverage gate, build)

## help: Display available targets
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all            Run tidy, lint, vuln, test, coverage, and build"
	@echo "  build          Compile universal fat binary for host arch via GoReleaser snapshot"
	@echo "  test           Run unit tests with race detection"
	@echo "  coverage       Run unit tests with race detection and verify coverage (> 95%)"
	@echo "  lint           Run golangci-lint (with warning fallback to go vet)"
	@echo "  vuln           Run govulncheck vulnerability scanner"
	@echo "  deps-microfat  Ensure microfat CLI and architecture stubs are present"
	@echo "  snapshot       Run GoReleaser snapshot build and fat binary packaging"
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
	@goreleaser release --snapshot --clean --skip=publish,sign,announce,sbom
	@mkdir -p $(BIN_DIR)
	@cp $(DIST_DIR)/fat/$(HOST_ARCH)/capagent $(BIN_DIR)/capagent
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

## coverage: Run tests with coverage profile, output metrics, and verify threshold (> 95%)
coverage:
	@echo "==> Running tests with coverage..."
ifdef GOTESTSUM
	@gotestsum -- -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
else
	@$(GO) test -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
endif
	@echo "==> Coverage summary:"
	@$(GO) tool cover -func=$(COVERAGE_FILE)
	@echo "==> Verifying code coverage threshold (> $(COVERAGE_THRESHOLD)%)..."
	@TOTAL_COVERAGE=$$($(GO) tool cover -func=$(COVERAGE_FILE) | grep "total:" | awk '{print substr($$3, 1, length($$3)-1)}'); \
	echo "$${TOTAL_COVERAGE} $(COVERAGE_THRESHOLD)" | awk '{if ($$1 <= $$2) { printf "❌ Coverage %s%% is not above target $(COVERAGE_THRESHOLD)%%\n", $$1; exit 1 } else { printf "✅ Total coverage %s%% satisfies target > $(COVERAGE_THRESHOLD)%%\n", $$1 }}'

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

## vuln: Run govulncheck vulnerability scanner
vuln:
	@echo "==> Running vulnerability check..."
ifdef GOVULNCHECK
	@govulncheck ./...
else
	@echo "⚠️  WARNING: govulncheck not found in PATH."
	@echo "⚠️  Install govulncheck via: go install golang.org/x/vuln/cmd/govulncheck@latest"
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
