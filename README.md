# MCPXcel

Go-based MCP server for targeted Excel analysis using Excelize and mcp-go.

## Requirements
- Go 1.25.0+
- Optional: GitHub CLI (`gh`) for repo operations

## Quick Start
Run the server over stdio for MCP clients:

```
MCPXCEL_ALLOWED_DIRS="$HOME/Documents:/data" go run ./cmd/server --stdio
```

## Make Targets
- `make run` — start server with `--stdio`
- `make build` — build `./cmd/server`
- `make lint` — gofmt/goimports (if available) + go vet
- `make test` — run all tests with coverage
- `make test-race` — race-enabled tests for `internal/...`

## Module
Import path: `github.com/vinodismyname/mcpxcel`

