.DEFAULT_GOAL := help

.PHONY: help lint vet test build run

help: ## Print each target with a one-line description
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "%-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

lint: ## Fail on gofmt or golangci-lint findings
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then printf '%s\n' "$$out"; exit 1; fi
	"$$(go env GOPATH)/bin/golangci-lint" run

vet: ## Run go vet ./...
	go vet ./...

test: ## Run go test ./...
	go test ./...

build: ## Build ./cmd/veto to bin/veto
	mkdir -p bin
	go build -o bin/veto ./cmd/veto

run: ## Run the veto CLI, forwarding extra arguments
	go run ./cmd/veto $(ARGS)
