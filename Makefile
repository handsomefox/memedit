# memedit — fast Windows memory scanner/editor.
# The binary targets windows/amd64; tests and benchmarks run on any platform.

BINARY  := memedit.exe
PKG     := ./...
GOFLAGS :=

.DEFAULT_GOAL := build

.PHONY: build
build: ## Cross-compile the Windows binary (windows/amd64)
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -o $(BINARY) .

.PHONY: test
test: ## Run the OS-independent tests (race detector on)
	go test -race $(PKG)

.PHONY: bench
bench: ## Run the matcher benchmarks
	go test -bench=Match -benchmem ./internal/scan

.PHONY: vet
vet: ## go vet for both the host and windows/amd64
	go vet $(PKG)
	GOOS=windows GOARCH=amd64 go vet $(PKG)

.PHONY: lint
lint: ## golangci-lint for both the host and windows/amd64 (requires golangci-lint)
	golangci-lint run $(PKG)
	GOOS=windows GOARCH=amd64 golangci-lint run $(PKG)

.PHONY: fmt
fmt: ## Format all Go files
	gofmt -w .

.PHONY: check
check: fmt vet test ## Format, vet, and test

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)

.PHONY: help
help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
