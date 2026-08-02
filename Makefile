.PHONY: help build install test test-race cover lint fmt fmt-check vet tidy clean ci

# Keep this pinned to the version .github/workflows/ci.yml uses, so `make lint`
# and CI cannot disagree.
GOLANGCI_LINT_VERSION := v2.12.2

BIN := bin/edilint

help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the CLI into bin/
	go build -o $(BIN) ./cmd/edilint

install: ## Install the CLI into GOPATH/bin
	go install ./cmd/edilint

test: ## Run the tests
	go test ./...

test-race: ## Run the tests with the race detector, as CI does
	go test -race -cover ./...

cover: ## Write and open an HTML coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint: ## Run golangci-lint
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

fmt: ## Format the source
	gofmt -w .

fmt-check: ## Fail if any file needs formatting
	@unformatted=$$(gofmt -l .); \
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

ci: fmt-check vet lint test-race ## Run everything CI runs
	@echo "All checks passed."
