.PHONY: help build install wasm test test-race fuzz bench stream-check stats-stream-check cover lint fmt fmt-check vet tidy rulesdoc clean ci docs docs-check


# Keep this pinned to the version .github/workflows/ci.yml uses, so `make lint`
# and CI cannot disagree.
GOLANGCI_LINT_VERSION := v2.13.2
GOVULNCHECK_VERSION := v1.8.0
ACTIONLINT_VERSION := v1.7.12

BIN := bin/edilint

help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the CLI into bin/
	go build -o $(BIN) ./cmd/edilint

install: ## Install the CLI into GOPATH/bin
	go install ./cmd/edilint

# The browser build: the linter as a WebAssembly module plus the Go runtime's
# JavaScript shim, which moved from misc/wasm to lib/wasm in Go 1.24.
WASM_DIR := dist/wasm
WASM_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')

wasm: ## Build the browser module into dist/wasm/ (edilint.wasm + wasm_exec.js)
	mkdir -p $(WASM_DIR)
	GOOS=js GOARCH=wasm go build -trimpath -ldflags "-s -w -X main.version=$(WASM_VERSION)" -o $(WASM_DIR)/edilint.wasm ./cmd/edilint-wasm
	@shim="$$(go env GOROOT)/lib/wasm/wasm_exec.js"; \
	[ -f "$$shim" ] || shim="$$(go env GOROOT)/misc/wasm/wasm_exec.js"; \
	cp "$$shim" $(WASM_DIR)/wasm_exec.js
	@ls -la $(WASM_DIR)

test: ## Run the tests
	go test ./...

test-race: ## Run the tests with the race detector, as CI does
	go test -race -cover ./...

FUZZ_TIME ?= 10s
FUZZ_WORKERS ?= 2
FUZZ_TARGETS ?= FuzzParseYAML FuzzX12 FuzzHL7Batch FuzzEdifact FuzzDelimited FuzzFixedWidth FuzzDetectAndLint FuzzStreamParity FuzzStatsStreamParity

fuzz: ## Fuzz every parser (10s each, two workers; override FUZZ_TIME/FUZZ_TARGETS)
	@set -e; for target in $(FUZZ_TARGETS); do \
		go test -run '^$$' -fuzz "^$$target$$" -fuzztime $(FUZZ_TIME) -parallel $(FUZZ_WORKERS) -timeout 2m .; \
	done

bench: ## Check allocation budgets and measure lint throughput and memory
	go test -run '^TestLintAllocationBudget$$' -bench '^BenchmarkLint(Reader)?$$' -benchmem -benchtime 100ms -count 3 -timeout 2m .

stream-check: ## Lint a synthetic 2 GiB file under a 128 MiB heap budget
	EDILINT_STREAM_ACCEPTANCE=1 go test -run '^TestStreamMemoryBound$$' -count 1 -v -timeout 15m .

stats-stream-check: ## Census a synthetic 2 GiB file under a 128 MiB heap budget
	EDILINT_STREAM_ACCEPTANCE=1 go test -run '^TestStatsStreamMemoryBound$$' -count 1 -v -timeout 15m .

cover: ## Write and open an HTML coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint: ## Run golangci-lint
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

# Go sources to format: everything except hidden directories. CI keeps its
# module and build caches inside the checkout (.go/, .go-build/) and linked
# worktrees live under .worktrees/; a bare `gofmt -l .` walks all of them and
# reports third-party testdata that is deliberately unparseable.
GO_SOURCES = find . -path '*/.*' -prune -o -name '*.go' -print

fmt: ## Format the source
	$(GO_SOURCES) | xargs gofmt -w

fmt-check: ## Fail if any file needs formatting
	@unformatted=$$($(GO_SOURCES) | xargs gofmt -l); \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy go.mod
	go mod tidy

rulesdoc: ## Regenerate the per-rule reference
	go run ./cmd/edilint-rulesdoc

clean: ## Remove build and coverage output
	rm -rf bin coverage.out coverage.html

docs: ## Generate the static rule reference
	go run ./cmd/edilint-docs

docs-check: ## Reject missing or stale rule reference pages
	go run ./cmd/edilint-docs --check

ci: fmt-check vet lint test-race fuzz bench docs-check ## Run everything CI runs
	@echo "All checks passed."

.PHONY: vuln workflow-check release-checks
vuln: ## Reject reachable known vulnerabilities, including the Go standard library
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

workflow-check: ## Validate GitHub Actions syntax and release job dependencies
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) -color

release-checks: workflow-check vuln ## Run the additional release-readiness checks
