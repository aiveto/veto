.DEFAULT_GOAL := help

.PHONY: help lint vet build run

help: ## Print each target with a one-line description
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "%-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

lint: ## Fail if any Go file is not gofmt-clean
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then printf '%s\n' "$$out"; exit 1; fi

vet: ## Run go vet ./...
	go vet ./...

build: ## Build ./cmd/veto to bin/veto
	mkdir -p bin
	go build -o bin/veto ./cmd/veto

run: ## Run the veto CLI, forwarding extra arguments
	go run ./cmd/veto $(ARGS)
