# Implementation Plan

- [x] 1. Initialize project scaffolding and tooling
  - Create Go module rooted at `cmd/server`, `internal`, and `pkg` packages with go1.22 toolchain and shared lint/test targets.
  - Add dependencies for `github.com/mark3labs/mcp-go`, `github.com/xuri/excelize/v2`, `github.com/tmc/langchaingo`, `golang.org/x/sync/semaphore`, and structured logging/testing helpers.
  - Provide developer ergonomics: make targets for `go test`, `go fmt`, race runs, and scripts for large workbook fixtures.
  - _Requirements: 15.1, 15.3_

- [x] 2. Stand up MCP server bootstrap and lifecycle management
  - Initialize `server.NewMCPServer` with `WithToolCapabilities`, `WithResourceCapabilities`, `WithRecovery`, hooks, and middleware following mark3labs/mcp-go advanced server patterns.
  - Implement `ServeStdio` startup with graceful shutdown handler (`context.WithTimeout`, signal capture) and panic-safe logging hooks.
  - Wire base telemetry hook invocations for session registration, tool calls, and resource reads.
  - _Requirements: 16.1, 16.2_

- [x] 3. Implement runtime limits and concurrency controller
  - Build `RuntimeLimits` struct hydrated from configuration for payload, timeout, request, and workbook caps.
  - Create `RuntimeController` wrapping `semaphore.Weighted` controls for global request concurrency and max open workbooks with context-aware acquire/release.
  - Add middleware ensuring busy responses (`BUSY_RESOURCE`) or queue/backoff when limits are hit, emitting structured metrics counters.
  - _Requirements: 12.1, 12.2, 12.4, 15.1_

- [x] 4. Create workbook lifecycle manager and stateless handle cache
  - Implement `WorkbookManager` with TTL-bearing handle records, UUID identifiers, and per-handle `sync.RWMutex` guarding concurrent reads vs writes.
  - Integrate Excelize open/close lifecycle so handles wrap `excelize.OpenFile`, enforce size/format checks, and release resources when TTL expires or explicit close is requested.
  - Ensure every tool accepts workbook IDs on each call (stateless operations) and validates handle existence before use.
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 10.1, 10.2, 10.3, 10.4, 11.1, 11.2, 12.2_

- [ ] 5. Build filesystem security and path validation guardrails
  - Construct allow-list policy resolving absolute paths, preventing traversal, and validating extensions before open/write operations.
  - Surface permission errors with actionable codes and log audit events for accepted/denied access attempts.
  - Add directory configuration validation on startup with fail-safe behavior and telemetry hooks.
  - _Requirements: 13.1, 13.2, 13.3, 13.4_

- [ ] 6. Implement MCP tool and discovery registry foundation
  - Define typed tool metadata using `mcp.NewTool` plus `mcp.NewTypedToolHandler`, including parameter schemas, defaults, and descriptions.
  - Register tool filters and middleware for permission/context awareness (e.g., admin-gated writes) leveraging server tool filtering APIs.
  - Ensure `list_tools` surfaces schemas, default limits, and capability flags consistent with protocol expectations.
  - _Requirements: 16.1, 16.2_

- [ ] 7. Implement workbook access and structure discovery tools
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 2.1, 2.2, 2.3, 2.4, 2.5, 10.1, 10.2, 10.3, 10.4, 11.2_
  - [ ] 7.1 Create open_workbook tool with validation and handle management
    - Build typed handler that validates allow-listed paths, file size, and supported formats before registering handles with TTL cache.
    - Return workbook IDs, effective limits, and warnings (e.g., nearing payload caps) in MCP-compliant responses.
    - Map Excelize/open errors to `FILE_TOO_LARGE` or `UNSUPPORTED_FORMAT` tool errors with recovery guidance.
    - _Requirements: 1.1, 1.2, 1.4, 10.1, 10.2, 10.3, 10.4, 11.2, 13.1_
  - [ ] 7.2 Implement list_structure tool for metadata discovery
    - Iterate sheets via Excelize `GetSheetMap`/`Rows` without loading cell data, streaming sheet summaries and header inference.
    - Support metadata-only mode and include row/column counts plus configurable preview guidance.
    - Return specific error codes for invalid handles or discovery failures per protocol catalog.
    - _Requirements: 2.1, 2.2, 2.4, 2.5_
  - [ ] 7.3 Build preview_sheet tool with streaming row access
    - Use Excelize `Rows` iterator with deferred `Close` and `Error` checks, enforcing row/payload limits and returning cursors.
    - Provide selectable encodings (JSON table vs CSV string) and include metadata fields `total`, `returned`, `truncated`, `nextCursor`.
    - Emit truncation guidance when previews exceed limits and advise targeted range calls.
    - _Requirements: 2.3, 2.4, 3.4, 14.1_

- [ ] 8. Create data access and manipulation tools
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 6.1, 6.2, 6.3, 6.4, 7.1, 7.2, 7.3, 11.3, 11.4, 12.3, 14.1_
  - [ ] 8.1 Implement read_range tool with streaming and pagination
    - Parse A1 ranges (support named ranges) with validation and clamp to configured cell/payload limits.
    - Stream results via `Rows.Columns()` while holding workbook read locks, handling sparse rows, and calling `rows.Close()`.
    - Maintain deterministic pagination with stable cursors and idempotent responses across retries.
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 11.3, 14.1_
  - [ ] 8.2 Build write_range tool with transactional operations
    - Serialize workbook writes with per-handle locks, buffering updates via Excelize `StreamWriter` (ascending row order) and flushing results.
    - Validate payload size, cell counts, and formulas before commit; roll back on partial failures and label non-idempotent mutations.
    - Return cell counts, diff metadata, and retry instructions distinguishing safe vs unsafe retries.
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 11.4, 12.3_
  - [ ] 8.3 Create apply_formula tool for bulk calculations
    - Apply formulas using batch helpers, respecting workbook locks and configurable slice sizes to avoid memory spikes.
    - Validate formula syntax, dependency range, and provide rollback plus conflict warnings when overwriting existing formulas.
    - Offer optional preview via read_range pipeline while maintaining payload guardrails.
    - _Requirements: 7.1, 7.2, 7.3_

- [ ] 9. Implement data analysis, search, and filtering capabilities
  - _Requirements: 4.1, 4.2, 4.3, 4.4, 5.1, 5.2, 5.3, 5.4, 8.1, 8.2, 8.3, 8.4_
  - [ ] 9.1 Build compute_statistics tool with streaming analysis
    - Aggregate stats per column using streaming reducers (count, sum, avg, min/max, distinct) supporting up to configured cell limits.
    - Add group-by aggregation using map reducers with memory budgeting and fallback to suggestions when limits exceed.
    - Surface structured result schema with units, precision, and resource usage hints for follow-on calls.
    - _Requirements: 4.1, 4.2, 4.3, 4.4_
  - [ ] 9.2 Implement search_data tool with pattern matching
    - Use Excelize `SearchSheet` for literal/regex searches with column filters, batching results to respect match caps.
    - Provide pagination cursors, total match counts, and bounded context rows per hit.
    - Emit empty result payloads with zero totals when no matches are found.
    - _Requirements: 5.1, 5.2, 5.3, 5.4_
  - [ ] 9.3 Build filter_data tool with predicate engine
    - Implement filter expression parser (comparisons, logical operators) and evaluate rows via streaming iteration.
    - Support configurable row limits, pagination, and stable cursor semantics aligned with read_range metadata.
    - Return validation errors for unsupported operators with corrective guidance.
    - _Requirements: 8.1, 8.2, 8.3, 8.4_

- [ ] 10. Create LangChainGo-powered insight generation system
  - _Requirements: 9.1, 9.2, 9.3, 9.4_
  - [ ] 10.1 Set up LangChainGo integration with memory management
    - Initialize LLM client and chain components using context-first APIs, wired to configurable timeouts and retries.
    - Choose memory strategy (token buffer/summary) per request, never sharing memory instances across chains per best practice.
    - Configure HTTP client pooling and fallback providers consistent with LangChainGo architecture guidance.
    - _Requirements: 9.1, 9.3_
  - [ ] 10.2 Implement generate_insight tool with statistical fusion
    - Compose sequential chains that combine deterministic statistics with LLM summarization, enforcing character caps.
    - Detect LLM failures and degrade gracefully to statistical-only responses with actionable guidance.
    - Exclude raw data tables from responses while surfacing trends, anomalies, and recommended next queries.
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

- [ ] 11. Build MCP resource system for metadata and previews
  - Register workbook metadata, preview snapshots, and configuration resources using stable URIs (e.g., `excel://workbooks/{id}/structure`).
  - Implement resource handlers returning declared MIME types, size bounds, and honoring allow-list validation.
  - Surface effective configuration limits and server capabilities through `list_resources` and discovery metadata.
  - _Requirements: 2.4, 15.2, 16.2_

- [ ] 12. Establish validation, error handling, and retry semantics
  - _Requirements: 11.3, 11.4, 14.1, 14.2, 14.3, 14.4, 14.5, 16.1_
  - [ ] 12.1 Build structured error catalog and mapping
    - Define canonical MCP error codes/messages aligned with requirements (e.g., `FILE_TOO_LARGE`, `BUSY_RESOURCE`, `CORRUPT_WORKBOOK`).
    - Provide helper to wrap internal errors into `mcp.NewToolResultError` including actionable `nextSteps` and retry hints.
    - _Requirements: 14.2, 14.5, 16.1_
  - [ ] 12.2 Implement input validation and JSON schema enforcement
    - Use struct validation tags and custom validators (filepath, range) in typed handlers to reject invalid inputs before execution.
    - Keep JSON schemas in sync with validators and surface validation errors with correction examples.
    - _Requirements: 14.4, 16.1_
  - [ ] 12.3 Document idempotency and retry guidance
    - Mark read operations as idempotent with safe retry flags and ensure response determinism across repeated calls.
    - Label write/transform tools with idempotency metadata and provide compensating action guidance where retries are unsafe.
    - _Requirements: 11.3, 11.4_
  - [ ] 12.4 Handle timeouts and cancellation consistently
    - Propagate `context.Context` through Excelize iterators and LangChainGo chains, aborting work when deadlines expire.
    - Convert timeout/cancellation to structured `TIMEOUT` errors with scope-narrowing recommendations.
    - _Requirements: 14.1, 14.3_

- [ ] 13. Add telemetry, monitoring, and audit systems
  - Integrate logging middleware capturing session/tool/resource events with timing and error annotations.
  - Expose metrics for request latency, concurrency semaphore saturation, workbook cache hits, and LangChain durations.
  - Emit audit logs for file access decisions and sensitive tool invocations with workbook IDs.
  - _Requirements: 12.4, 13.4_

- [ ] 14. Implement configuration management and deployment assets
  - Load hierarchical configuration (YAML + env + CLI) with validation, default limit documentation, and effective-value exposure.
  - Provide sample config files and documentation for tuning payload, concurrency, and directory guardrails.
  - Create containerization and build scripts (multi-stage Dockerfile, non-root user) aligned with deployment best practices.
  - _Requirements: 15.1, 15.2, 15.3_

- [ ] 15. Build comprehensive test suite
  - Author table-driven unit tests for each tool handler covering success, validation, concurrency, and error mapping paths.
  - Create concurrency tests using race detector and stress harnesses to verify semaphore behavior and workbook locks.
  - Add streaming/large-file fixtures ensuring memory bounds, pagination stability, and iterator closure semantics.
  - Mock LangChainGo chains/LLMs to test insight fallbacks and timeout handling; include protocol integration tests for `list_tools`/`list_resources`.
  - _Requirements: All requirements coverage verification_

- [ ] 16. Add production readiness features
  - Implement health/readiness endpoints (for HTTP transport) and CLI switches for modes (stdio vs HTTP).
  - Enable optional TLS HTTP transport with connection limits, auth middleware, and structured access logs.
  - Provide systemd unit, sample launch scripts, and operational runbooks covering scaling and observability hooks.
  - _Requirements: 12.4, 15.2, 16.2_
