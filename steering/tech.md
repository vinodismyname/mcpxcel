# Technology Stack & Build System

## Core Technologies

- **Language**: Go 1.25+
- **MCP Framework**: mark3labs/mcp-go for Model Context Protocol server implementation
- **Excel Processing**: Excelize (github.com/xuri/excelize/v2) for spreadsheet operations
- **LLM Integration**: LangChain-Go for multi-provider LLM support (OpenAI, Anthropic, AWS Bedrock)

## Build System

Uses standard Go toolchain with Make for automation:

### Common Commands

```bash
# Development
make run          # Start server with --stdio flag
go run ./cmd/server --stdio

# Building
make build        # Build binary to ./cmd/server
go build ./cmd/server

# Code Quality
make lint         # Run full linting (fmt + imports + vet)
make fmt          # Format code with go fmt
make imports      # Organize imports with goimports
make vet          # Run go vet

# Testing
make test         # Run all tests with coverage
make test-race    # Run race-enabled tests on internal packages
go test ./...     # Quick test run
go test -race ./internal/...  # Race detection for concurrency
```

## Dependencies

- **MCP Server**: `github.com/mark3labs/mcp-go`
- **Excel Processing**: `github.com/xuri/excelize/v2`
- **LLM Integration**: `github.com/tmc/langchaingo`

## Architecture Patterns

- **MCP Protocol**: Server exposes tools via Model Context Protocol for AI assistant integration
- **Concurrent Design**: Go goroutines for parallel request handling with per-workbook locking
- **Bounded Operations**: All operations have configurable limits (10k cells, 128KB payloads, 200 rows)
- **Path-First API**: Tools accept `path` or `cursor`; no client-visible workbook IDs
- **Stateless Design**: No persistent server-side sessions; internal handle cache keyed by canonical path for efficiency

## Configuration

- Environment variables for LLM provider selection (LLM_PROVIDER=openai|anthropic)
- Configurable limits for file size (20MB default), payload size, and operation bounds
- Local file system access with optional directory allow-lists for security

## CI & Repository Integration

- GitHub Actions workflow: `.github/workflows/ci.yml`.
  - Triggers on pushes to `main` and pull requests targeting `main`.
  - Steps: `actions/setup-go@v5` (Go 1.25.x), `make lint`, `make test`, `make test-race`.
- GitHub CLI (`gh`) supports the standard flow:
  - Open PR: `gh pr create -B main -H <branch> -t "..." -b "..."`
  - Merge PR: `gh pr merge --squash --delete-branch`
  - Tag release and create notes: `git tag vX.Y.Z -m "..." && gh release create vX.Y.Z --generate-notes`
