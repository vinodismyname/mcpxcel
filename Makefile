GO ?= go
GOFMT ?= gofmt
GOIMPORTS ?= goimports

PKGS := ./...
INTERNAL_PKGS := ./internal/...

.PHONY: lint fmt imports vet test test-race build run ensure-go ensure-go-mod

ensure-go:
	@command -v $(GO) >/dev/null 2>&1 || { \
		echo "Go toolchain not found. Install Go 1.22+ before running this target."; \
		exit 1; \
	}

ensure-go-mod: ensure-go
	@if [ ! -f go.mod ]; then \
		echo "go.mod not found. Initialize a module with 'go mod init <module>' before using build/test targets."; \
		exit 1; \
	fi

fmt: ensure-go-mod
	@pkgs="$$( $(GO) list $(PKGS) 2>/dev/null )"; \
	if [ -z "$$pkgs" ]; then \
		echo "No Go packages detected; skipping go fmt."; \
	else \
		$(GO) fmt $$pkgs; \
	fi

imports: ensure-go-mod
	@if ! command -v $(GOIMPORTS) >/dev/null 2>&1; then \
		echo "goimports not found; skipping import reordering."; \
	else \
		pkgs="$$( $(GO) list -f '{{.Dir}}' $(PKGS) 2>/dev/null )"; \
		if [ -z "$$pkgs" ]; then \
			echo "No Go packages detected; skipping goimports."; \
		else \
			$(GOIMPORTS) -w $$pkgs; \
		fi; \
	fi

vet: ensure-go-mod
	@pkgs="$$( $(GO) list $(PKGS) 2>/dev/null )"; \
	if [ -z "$$pkgs" ]; then \
		echo "No Go packages detected; skipping go vet."; \
	else \
		$(GO) vet $$pkgs; \
	fi

lint: fmt imports vet
	@echo "Lint completed."

test: ensure-go-mod
	@pkgs="$$( $(GO) list $(PKGS) 2>/dev/null )"; \
	if [ -z "$$pkgs" ]; then \
		echo "No Go packages detected; skipping go test."; \
	else \
		$(GO) test -cover $$pkgs; \
	fi

test-race: ensure-go-mod
	@pkgs="$$( $(GO) list $(INTERNAL_PKGS) 2>/dev/null )"; \
	if [ -z "$$pkgs" ]; then \
		echo "No internal packages detected; skipping race-enabled tests."; \
	else \
		$(GO) test -race $$pkgs; \
	fi

build: ensure-go-mod
	@if [ ! -d cmd/server ]; then \
		echo "cmd/server not found; skipping go build."; \
		exit 0; \
	fi; \
	$(GO) build ./cmd/server

run: ensure-go-mod
	@if [ ! -d cmd/server ]; then \
		echo "cmd/server not found; nothing to run."; \
		exit 0; \
	fi; \
	$(GO) run ./cmd/server --stdio
