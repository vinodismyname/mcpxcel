# Technology Stack & Build System

## Core Technologies

- **Language**: Go 1.22+
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
- **Stateless Design**: No persistent server-side sessions; workbook handles for efficiency

## Configuration

- Environment variables for LLM provider selection (LLM_PROVIDER=openai|anthropic)
- Configurable limits for file size (20MB default), payload size, and operation bounds
- Local file system access with optional directory allow-lists for security