# Project Structure & Organization

## Directory Layout

```
├── cmd/
│   └── server/          # Main server entrypoint
├── internal/
│   ├── runtime/         # Concurrency controls and limits
│   ├── registry/        # Tool registration and wiring
│   ├── workbooks/       # Excelize adapters and workbook management
│   ├── insights/        # Sequential insights thought tracking + primitives (domain-neutral)
│   └── integration/     # Integration tests (behind build tag)
├── pkg/                 # Reusable helpers for external sharing
├── testdata/           # Sanitized .xlsx fixtures for testing
├── config/             # Configuration files and defaults
├── .kiro/
│   ├── specs/          # Feature specifications
│   └── steering/       # AI assistant guidance rules
└── design.md           # Design documentation

### GitHub & CI

- CI workflows live under `.github/workflows/` (see `ci.yml`).
- The `main` branch is protected; contributions land via PRs with passing CI.
- Release tags (e.g., `v0.2.0`) are created on `main` and published as GitHub Releases.
```

## Module Organization

### Core Packages

- **`cmd/server`**: Go entrypoint code, main function, CLI argument handling
- **`internal/runtime`**: Concurrency controls, semaphores, resource limits
- **`internal/registry`**: Tool registration, MCP server setup, handler wiring
- **`internal/workbooks`**: Excelize adapters and workbook access by canonical path with TTL caching
- **`internal/insights`**: Thought tracker and deterministic, bounded primitives (composition/mix shift, concentration/HHI, funnel, quality)

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
  - Examples: `OpenWorkbookHandler`, `FilterDataHandler`, `GenerateInsightsHandler`, `DetectTablesHandler`

### Test Files
- **Location**: `*_test.go` files beside the code they exercise
- **Style**: Table-driven tests for validation matrices and error catalogs
- **Integration**: Tests in `internal/integration` behind `integration` build tag
 - **Insights**: See `internal/insights/*_test.go` for planner, detection, profiling, and primitive correctness tests using tiny generated `.xlsx` fixtures.

## File Boundaries

Model new folders on tool boundaries to keep handlers, caches, and policy modules loosely coupled. Each package should have a clear, single responsibility:

- **Runtime**: Resource management and concurrency
- **Registry**: Tool definitions and MCP protocol handling  
- **Workbooks**: Excel file operations and data access
- **Integration**: End-to-end testing across tool boundaries

## Branching & PR Flow

- Branch from `main` using prefixes: `feat/`, `fix/`, `chore/`, `docs/`, `refactor/`.
- Keep commits focused; prefer squash-merge to maintain a clean history.
- Each PR must:
  - Pass CI (`make lint`, `make test`, `make test-race`).
  - Update related docs and configuration where applicable.
  - Include a concise, imperative subject and a validation summary.

## Releases

- Use Semantic Versioning (`vX.Y.Z`).
- After merging to `main`, tag a release (e.g., `v0.2.0`) and generate notes.
- Policy: bump the patch version for each completed task; bump the minor version once all tasks in `tasks.md` are complete. Reserve patch for hotfixes as needed.
- Ensure the `pkg/version` package reflects the release version when appropriate.

## Configuration Management

- **Runtime limits**: Versioned in `config/` or `internal/runtime/config.go`
- **Workbook allow-lists**: Configurable directory restrictions
- **Default limits**: Document effective defaults for MCP client understanding
- **Idempotency policy**: See design.md “Idempotency & Retries” for retry-safe reads and non-idempotent write guidance
