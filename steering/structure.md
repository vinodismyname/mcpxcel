# Project Structure & Organization

## Directory Layout

```
├── cmd/
│   └── server/          # Main server entrypoint
├── internal/
│   ├── runtime/         # Concurrency controls and limits
│   ├── registry/        # Tool registration and wiring
│   ├── workbooks/       # Excelize adapters and workbook management
│   └── integration/     # Integration tests (behind build tag)
├── pkg/                 # Reusable helpers for external sharing
├── testdata/           # Sanitized .xlsx fixtures for testing
├── config/             # Configuration files and defaults
├── .kiro/
│   ├── specs/          # Feature specifications
│   └── steering/       # AI assistant guidance rules
└── design.md           # Design documentation
```

## Module Organization

### Core Packages

- **`cmd/server`**: Go entrypoint code, main function, CLI argument handling
- **`internal/runtime`**: Concurrency controls, semaphores, workbook locks, resource limits
- **`internal/registry`**: Tool registration, MCP server setup, handler wiring
- **`internal/workbooks`**: Excelize adapters, workbook handle management, Excel operations

### Supporting Packages

- **`pkg/`**: Reusable utilities that may be shared externally
- **`testdata/`**: Sanitized Excel fixtures for testing (never commit real customer data)
- **`config/`**: Configuration management, default limits, allow-lists

## Naming Conventions

### Go Standards
- **Indentation**: Tabs (Go standard)
- **Exported identifiers**: CamelCase
- **File names**: `snake_case.go` tied to primary type
  - Examples: `workbook_manager.go`, `runtime_limits.go`

### Tool Handlers
- **Pattern**: `VerbNoun` naming
  - Examples: `OpenWorkbookHandler`, `FilterDataHandler`, `AnalyzeDataHandler`

### Test Files
- **Location**: `*_test.go` files beside the code they exercise
- **Style**: Table-driven tests for validation matrices and error catalogs
- **Integration**: Tests in `internal/integration` behind `integration` build tag

## File Boundaries

Model new folders on tool boundaries to keep handlers, caches, and policy modules loosely coupled. Each package should have a clear, single responsibility:

- **Runtime**: Resource management and concurrency
- **Registry**: Tool definitions and MCP protocol handling  
- **Workbooks**: Excel file operations and data access
- **Integration**: End-to-end testing across tool boundaries

## Configuration Management

- **Runtime limits**: Versioned in `config/` or `internal/runtime/config.go`
- **Workbook allow-lists**: Configurable directory restrictions
- **Default limits**: Document effective defaults for MCP client understanding