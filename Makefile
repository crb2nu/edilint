.PHONY: help build install wasm test test-race fuzz cover lint fmt fmt-check vet tidy clean ci

# Keep this pinned to the version .github/workflows/ci.yml uses, so `make lint`
# and CI cannot disagree.
GOLANGCI_LINT_VERSION := v2.13.2

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

fuzz: ## Run the bounded YAML fuzz pass, as CI does
	go test -run '^$$' -fuzz '^FuzzParseYAML$$' -fuzztime 10s .

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

clean: ## Remove build and coverage output
	rm -rf bin coverage.out coverage.html

ci: fmt-check vet lint test-race fuzz ## Run everything CI runs
	@echo "All checks passed."
