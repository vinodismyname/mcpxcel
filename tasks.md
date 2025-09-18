# Implementation Plan

- [x] 0. Bootstrap GitHub repository, CI, and v0.1.0
  - Set module path to `github.com/vinodismyname/mcpxcel`; bump Go to 1.25.0.
  - Add CI workflow at `.github/workflows/ci.yml` to run `make lint`, `make test`, `make test-race`.
  - Create README and MIT LICENSE; open PR, merge with squash, and tag `v0.1.0`.
  - Establish branch protection on `main` and standard per-task PR flow.

- [x] 1. Initialize project scaffolding and tooling
  - Create Go module rooted at `cmd/server`, `internal`, and `pkg` packages with go1.22 toolchain and shared lint/test targets.
  - Add dependencies for `github.com/mark3labs/mcp-go`, `github.com/xuri/excelize/v2`, `github.com/tmc/langchaingo`, `golang.org/x/sync/semaphore`, and structured logging/testing helpers.
  - Provide developer ergonomics: make targets for `go test`, `go fmt`, race runs, and scripts for large workbook fixtures.
  - _Requirements: 15.1, 15.3_

- [x] 2. Stand up MCP server bootstrap and lifecycle management
  - Initialize `server.NewMCPServer` with `WithToolCapabilities`, `WithRecovery`, hooks, and middleware following mark3labs/mcp-go advanced server patterns.
  - Implement `ServeStdio` startup with graceful shutdown handler (`context.WithTimeout`, signal capture) and panic-safe logging hooks.
  - Wire base logging hook invocations for session registration and tool calls.
  - _Requirements: 16.1_

- [x] 3. Implement runtime limits and concurrency controller
  - Build `RuntimeLimits` struct hydrated from configuration for payload, timeout, request, and workbook caps.
  - Create `RuntimeController` wrapping `semaphore.Weighted` controls for global request concurrency and max open workbooks with context-aware acquire/release.
  - Add middleware ensuring busy responses (`BUSY_RESOURCE`) or queue/backoff when limits are hit.
  - _Requirements: 12.1, 12.2, 12.4, 15.1_

- [x] 4. Create workbook lifecycle manager and stateless handle cache
  - Implement `WorkbookManager` with TTL-bearing handle records, UUID identifiers, and per-handle `sync.RWMutex` guarding concurrent reads vs writes.
  - Integrate Excelize open/close lifecycle so handles wrap `excelize.OpenFile`, enforce size/format checks, and release resources when TTL expires or explicit close is requested.
  - Ensure every tool accepts workbook IDs on each call (stateless operations) and validates handle existence before use.
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 10.1, 10.2, 10.3, 10.4, 11.1, 11.2, 12.2_

- [x] 5. Build filesystem security and path validation guardrails
  - Construct allow-list policy resolving absolute paths, preventing traversal, and validating extensions before open/write operations.
  - Surface permission errors with actionable codes and log audit events for accepted/denied access attempts.
  - Add directory configuration validation on startup with fail-safe behavior and logging hooks.
  - _Requirements: 13.1, 13.2, 13.3, 13.4_

- [x] 6. Implement MCP tool and discovery registry foundation
  - Define typed tool metadata using `mcp.NewTool` plus `mcp.NewTypedToolHandler`, including parameter schemas, defaults, and descriptions.
  - Register tool filters and middleware for permission/context awareness (e.g., admin-gated writes) leveraging server tool filtering APIs.
  - Ensure `list_tools` surfaces schemas, default limits, and capability flags consistent with protocol expectations.
  - _Requirements: 16.1_

- [x] 7. Implement workbook access and structure discovery tools
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 2.1, 2.2, 2.3, 2.4, 2.5, 10.1, 10.2, 10.3, 10.4, 11.2_
  - [x] 7.1 Create open_workbook tool with validation and handle management
    - Inject `internal/security.Manager` into the workbook manager/registry at bootstrap so `Open` enforces allow-list validation.
    - Build typed handler that validates allow-listed paths, file size, and supported formats before registering handles with TTL cache.
    - Return workbook IDs, effective limits, and warnings (e.g., nearing payload caps) in MCP-compliant responses.
    - Map Excelize/open errors to `FILE_TOO_LARGE` or `UNSUPPORTED_FORMAT` tool errors with recovery guidance.
    - _Requirements: 1.1, 1.2, 1.4, 10.1, 10.2, 10.3, 10.4, 11.2, 13.1_
  - [x] 7.2 Implement list_structure tool for metadata discovery
    - Iterate sheets via Excelize `GetSheetMap`/`Rows` without loading cell data, streaming sheet summaries and header inference.
    - Support metadata-only mode and include row/column counts plus configurable preview guidance.
    - Return specific error codes for invalid handles or discovery failures per protocol catalog.
    - _Requirements: 2.1, 2.2, 2.4, 2.5_
  - [x] 7.3 Build preview_sheet tool with streaming row access
    - Use Excelize `Rows` iterator with deferred `Close` and `Error` checks, enforcing row/payload limits and returning cursors.
    - Provide selectable encodings (JSON table vs CSV string) and include metadata fields `total`, `returned`, `truncated`, `nextCursor`.
    - Emit truncation guidance when previews exceed limits and advise targeted range calls.
    - _Requirements: 2.3, 2.4, 3.4, 14.1_

- [x] 8. Create data access and manipulation tools
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 6.1, 6.2, 6.3, 6.4, 7.1, 7.2, 7.3, 11.3, 11.4, 12.3, 14.1_
  - [x] 8.1 Implement read_range tool with streaming and pagination
    - Parse A1 ranges (support named ranges) with validation and clamp to configured cell/payload limits.
    - Stream results via `Rows.Columns()` while holding workbook read locks, handling sparse rows, and calling `rows.Close()`.
    - Maintain deterministic pagination with stable cursors and idempotent responses across retries.
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 11.3, 14.1_
  - [x] 8.2 Build write_range tool with transactional operations
    - Serialize workbook writes with per-handle locks, buffering updates via Excelize `StreamWriter` (ascending row order) and flushing results.
    - Validate payload size, cell counts, and formulas before commit; roll back on partial failures and label non-idempotent mutations.
    - Return cell counts, diff metadata, and retry instructions distinguishing safe vs unsafe retries.
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 11.4, 12.3_
  - [x] 8.3 Create apply_formula tool for bulk calculations
    - Apply formulas using batch helpers, respecting workbook locks and configurable slice sizes to avoid memory spikes.
    - Validate formula syntax, dependency range, and provide rollback plus conflict warnings when overwriting existing formulas.
    - Offer optional preview via read_range pipeline while maintaining payload guardrails.
    - _Requirements: 7.1, 7.2, 7.3_

- [x] 8.4 Standardize pagination cursors and resume semantics
  - Design and document an opaque, URL-safe base64 JSON cursor spec in `design.md` including fields: version, `path`, `sheet`, normalized `range`, `unit` (rows|cells), `offset`, `pageSize`, `mtime`, `issuedAt`, and tool-specific hashes (`queryHash`, `predicateHash`).
  - Create `pkg/pagination` helpers for `EncodeCursor`, `DecodeCursor`, `NextOffset`, and validation (unit handling, bounds, required fields). Include fuzz tests for malformed tokens.
  - Update `read_range` to accept an optional `cursor` param that takes precedence over `sheet/range/max_cells`; compute resume start from the token; emit opaque `nextCursor`.
  - Emit only opaque cursors; legacy query-string cursors removed pre‑GA (no env flag fallback).
  - _Requirements: 14.1, 14.3, 3.1, 3.2, 3.4_

- [x] 8.5 Add workbook write-versioning for cursor stability
  - Extend `internal/workbooks` handle state with a `version` (uint64) incremented on successful mutations (`write_range`, `apply_formula`, future writes).
  - Expose a `Version()` snapshot accessor and embed `wbv` into cursors. On resume, if versions differ, return `CURSOR_INVALID` with retry guidance.
  - Add table-driven tests simulating writes between paginated reads to assert invalidation and guidance text.
  - _Requirements: 11.4, 12.3, 14.1, 14.2_

- [x] 8.6 Unify preview/search/filter cursor semantics
  - Update `preview_sheet` to emit/accept the opaque cursor with `unit=rows` and row-based offset; preserve defaults for preview size.
  - Define and adopt tool-specific fields for `search_data` (`queryHash`) and `filter_data` (`predicateHash`) to bind cursors to the same parameters across pages.
  - Ensure all three tools return consistent metadata (`total`, `returned`, `truncated`, `nextCursor`) and validation on resume.
  - _Requirements: 2.3, 2.4, 5.1, 5.2, 5.4, 8.1, 8.2, 8.4, 14.1_

- [ ] 9. Implement data analysis, search, and filtering capabilities
  - _Requirements: 4.1, 4.2, 4.3, 4.4, 5.1, 5.2, 5.3, 5.4, 8.1, 8.2, 8.3, 8.4_
  - [x] 9.1 Build compute_statistics tool with streaming analysis
    - Aggregate stats per column using streaming reducers (count, sum, avg, min/max, distinct) supporting up to configured cell limits.
    - Add group-by aggregation using map reducers with memory budgeting and fallback to suggestions when limits exceed.
    - Surface structured result schema with units, precision, and resource usage hints for follow-on calls.
    - _Requirements: 4.1, 4.2, 4.3, 4.4_
  - [x] 9.2 Implement search_data tool with pattern matching
    - Use Excelize `SearchSheet` for literal/regex searches with column filters, batching results to respect match caps.
    - Provide opaque pagination cursors (unit=rows) embedding `queryHash` (`qh`); include total match counts and bounded context rows per hit; validate `wbv` and `qh` on resume and return `CURSOR_INVALID` when mismatched.
    - Emit empty result payloads with zero totals when no matches are found.
    - _Requirements: 5.1, 5.2, 5.3, 5.4_
  - [x] 9.2.1 Correct search_data pagination, visibility, and errors
    - Implement corrections per `steering/search_data_corrections.md`:
      - Set cursor `r` to sheet used range; preserve `qh` when resuming; map encode failures to `CURSOR_BUILD_FAILED`.
      - Map excelize "does not exist"/"doesn't exist" to `INVALID_SHEET`.
      - Attach results as JSON text content so clients render hits alongside metadata.
      - Anchor snapshots to left bound of used range; cap by `snapshot_cols` and actual columns.
      - Embed original search params (`q`, `rg`, `cl`) in cursor to enable deterministic cursor-only resume; continue validating `qh` for mismatches.
    - Add MCP client validation steps: truncated page returns `nextCursor`, resume works/mismatch invalidates, snapshots sized correctly.
    - _Requirements: 5.1, 5.2, 14.1, 14.2, 16.1_
  - [x] 9.3 Build filter_data tool with predicate engine
    - Implement filter expression parser (comparisons, logical operators) and evaluate rows via streaming iteration.
    - Support configurable row limits and opaque pagination cursors (unit=rows) embedding `predicateHash` (`ph`); align metadata with read_range.
    - Mirror search_data: include predicate provenance in cursor (e.g., original predicate string and optional column scope) to allow cursor-only resume without explicit inputs.
    - Return validation errors for unsupported operators with corrective guidance.
    - _Requirements: 8.1, 8.2, 8.3, 8.4_

- [ ] 10. Build Sequential Insights (domain-neutral, client-orchestrated)
  - Provide a planning tool and bounded, deterministic insight primitives; no server-embedded LLM. The MCP client (LLM) drives clarification, executes recommended tools, and narrates.
  - _Requirements: 4, 9, 14, 15, 16.1_
  - Reference: plans/010-sequential_insights.md
  - [x] 10.1 Add sequential_insights planning tool
    - Typed schema: `objective`, `path|cursor`, `hints`, `constraints`, `step_number`, `total_steps`, `next_step_needed`, revision/branch fields.
    - Output: `current_step`, `recommended_tools[{tool_name, confidence, rationale, priority, suggested_inputs, alternatives}]`, `questions[]`, `insight_cards[]`, `meta`.
    - Cursor precedence over path; include limits and truncation metadata.
    - Default planning-only mode; configurable bounded compute.
  - [x] 10.2 Implement table detection (multiple tables per sheet)
    - Detect rectangular data blocks via streaming scan, header heuristics, and blank-row/column separators; return Top-K candidates with confidence and previews.
    - Ask clarifying question when multiple plausible tables exist; proceed per chosen range.
  - [x] 10.3 Add schema profiling & role inference
    - Sample ≤100 rows/col to infer roles: measure, dimension, time, id, target. Emit clarifying questions on ambiguity.
    - Data quality checks: missingness, duplicate IDs, negative in nonnegative fields, >100% in percent-like, mixed types.
  - [x] 10.4 Add bounded insight primitives
    - Change over time & variance to baseline/target; driver ranking (Top ± movers), Top-N + "Other" capping.
    - Composition/mix shift (±5pp threshold), concentration metrics (Top-N share, HHI bands), robust outliers (modified z-score |z|≥3.5, ≤5 reported).
    - Funnel analysis: stage detection from column names/hints, stage conversion and bottleneck detection; segment overlays optional.
  - [ ] 10.5 Tests and documentation
    - Planner test matrix, table detection and role inference tests on fixtures, primitive correctness on small XLSX.
    - Update steering/product.md, steering/tech.md, steering/structure.md, design.md, requirements.md (Req. 9), and AGENTS.md.
  - [ ] 10.6 Config flags and safety
    - Add config to enable bounded compute and set thresholds (max_groups, outlier limit, mix threshold); keep path-only API and cursor semantics.

 

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

 

- [ ] 14. Implement configuration management
  - Load hierarchical configuration (YAML + env + CLI) with validation, default limit documentation, and effective-value exposure.
  - Provide sample config files and documentation for tuning payload, concurrency, and directory guardrails.
  - Document environment variable `MCPXCEL_ALLOWED_DIRS` (path list) in config docs and reference in design.md.
  - _Requirements: 15.1, 15.2, 15.3_

 

 

## Per-Task GitHub Workflow

For every task in this plan, follow the same branch/PR/release flow:

- Branch from `main`: `git checkout -b <prefix>/<short-task-name>` (prefix: `feat|fix|chore|docs|refactor`).
- Implement changes and update docs (`steering/*`, `design.md`, `requirements.md`, `tasks.md`) as needed.
- Validate locally: `make lint && make test && make test-race`.
- Commit using imperative subject and clear scope; push branch.
- Open PR: `gh pr create -B main -H <branch> -t "<scope>: <subject>" -b "Validation: make lint/test; Notes: ..."`.
- Ensure CI is green; address review feedback.
- Merge with squash and delete branch: `gh pr merge --squash --delete-branch`.
- Sync local `main`: `git checkout main && git pull`.
- If release-worthy, tag and publish: `git tag vX.Y.Z -m "..." && git push origin vX.Y.Z && gh release create vX.Y.Z --generate-notes`.

---

- [x] 9.4 Path-only API refactor (no workbook IDs)
  - Replace client-visible `workbook_id` with `path` for all tools; `cursor` takes precedence when present.
  - Bind cursors to file path (`pt`) and modification time (`mt`) for stateless resume.
  - Update internal/workbooks to support `GetOrOpenByPath` and by-path caching with TTL.
  - Remove open_workbook/close_workbook and update schemas/docs accordingly.
  - Reference: .specify/specs/002-path-only-api.md
  - _Requirements: 1 (path-only), 14.1 (cursor stability), 15, 16.1_

- [x] 9.5 Cursor/Errors polish and meta visibility
  - Fix and verify first-page cursor `mt` binding across tools; standardize invalid-sheet error mapping; surface one-line meta summaries in text outputs for clients that ignore structured metadata.
  - Changes:
    - Always set `mt` (file mtime) when emitting a first-page `nextCursor` for: `preview_sheet`, `read_range`, `search_data`, `filter_data`.
      - Compute `mt` under the existing read lock via `os.Stat(canonicalPath)` and include it in `pagination.Cursor{ Mt: <mtime> }` for the emitted token (regardless of whether the call was a fresh page or a resume).
      - Remove any code paths that only populate `mt` when resuming from an existing cursor.
    - Standardize INVALID_SHEET mapping for preview/list/read/search/filter/write tools:
      - Map excelize error messages containing either "doesn't exist" OR "does not exist" (case-insensitive) to `INVALID_SHEET: sheet not found`.
      - Ensure `preview_sheet` and `read_range` adopt the same mapping already used in `search_data`/`filter_data`.
    - Meta visibility for preview/read text outputs:
      - Prepend a single-line summary to the text response content (before the data) using the format:
        - `total=<n> returned=<m> truncated=<bool> nextCursor=<token-or-empty>`
      - Keep structured metadata (`meta.total`, `meta.returned`, `meta.truncated`, `meta.nextCursor`) unchanged; summary is an additive UX improvement for clients that ignore structured fields.
      - Respect payload bounds; keep summary concise and avoid duplicating large data.
    - Documentation:
      - Update `design.md` to note that `preview_sheet` and `read_range` include a one-line summary prefix in text content (similar to search/filter), while structured metadata remains authoritative.
  - Tests (table-driven where practical):
    - Cursor `mt` population:
      - Invoke each tool with parameters that guarantee truncation (small `rows`/`max_cells`/`max_results`), capture `meta.nextCursor`, decode with `pkg/pagination.DecodeCursor`, and assert: `pt` equals canonical path; `mt > 0`; `u` matches tool (`rows` for preview/search/filter, `cells` for read_range); offsets/page sizes are correct.
    - INVALID_SHEET mapping:
      - Simulate invalid sheet errors for `preview_sheet` and `read_range` and assert error code `INVALID_SHEET` for both "doesn't exist" and "does not exist" message variants (can stub/force messages or use an excelize call that produces each message on different platforms).
    - Meta summary presence:
      - For `preview_sheet` and `read_range`, assert that the first text content item includes the summary line; verify `meta` in structured payload matches the summary values.
  - Manual verification (MCP client):
    - Read tools: Force truncation and confirm the summary line appears in text output and `meta.nextCursor` is present; decode cursor to verify `pt` and `mt` non-zero.
    - Cursor path-binding: Copy the workbook to a new path, obtain a `nextCursor` on the original path, then call the same tool with `{ path: COPY_PATH, cursor: <oldCursor> }` and assert `CURSOR_INVALID` (path mismatch).
  - Validation: `make lint && make test && make test-race`.
  - _Requirements: 14.1 (cursor stability), 14.2 (error catalog), 16.1 (schemas/behavior documentation)_
