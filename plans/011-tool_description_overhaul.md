# Task 11 Plan — Tool Description Overhaul (LLM-Friendly)

## Objective
Elevate all tool descriptions and parameter help to be explicit, comprehensive, and LLM-friendly. Each tool will explain: what it does, when to use it (and when not), parameter semantics and precedence (especially cursor vs. positional inputs), output structure and pagination metadata, limits/caps, error mappings, and security caveats. Sequential insights will adopt a “sequential thinking” style description with step guidance.

## References to first read
- MCP sequential thinking servers and schemas
  - modelcontextprotocol/servers — sequentialthinking: src/sequentialthinking/index.ts
  - spences10/mcp-sequentialthinking-tools: src/schema.ts
- mark3labs/mcp-go documentation
  - Tool fundamentals and typed tools: www/docs/pages/servers/tools.mdx
- MCP docs/spec for tool structure and semantics
  - modelcontextprotocol/docs: docs/concepts/tools.mdx
  - modelcontextprotocol/specification: docs/specification/*/server/tools.mdx

## Cross-Tool Standards (apply to all)
- Path-first API and allow-listing. Inputs accept a canonical file `path`; access is restricted to configured allow-list directories.
- Cursor precedence. If `cursor` is provided, it takes precedence over positional inputs. The cursor’s `unit` must match the tool (rows or cells). Cursor binds to canonical `path` and file `mtime`; mismatches yield `CURSOR_INVALID`.
- Pagination metadata. Responses include `meta.total`, `meta.returned`, `meta.truncated`, and `meta.nextCursor` (opaque, URL-safe base64 JSON). Preview/read text outputs begin with a one-line summary; structured metadata remains authoritative.
- Limits and caps. Bounded by configured defaults (payload size, rows/cells per op, snapshot widths, timeouts). Tools surface effective defaults via list_tools.
- Error mapping. Use structured messages such as `VALIDATION: …`, `OPEN_FAILED: …`, `INVALID_SHEET: sheet not found`, `CURSOR_INVALID: …`, `READ_FAILED: …`, etc., consistent with current handlers.

## Per-Tool Description Drafts (to paste into mcp.WithDescription)

### list_structure
A discovery tool that returns workbook structure without loading cell data. It lists all sheets in index order with approximate row/column counts and (optionally) a best‑effort header row inference from the first row only. Use this to ground subsequent steps (e.g., choosing a sheet/range) and avoid streaming large content. Do not use for retrieving cell data.
Parameters: `path` (allow‑listed absolute file path), `metadata_only` (true to skip header inference). Returns `sheets[]` with `name`, `rowCount`, `columnCount`, and optional `headers`. No pagination or cell content is returned. Errors include `OPEN_FAILED`, `DISCOVERY_FAILED`, and invalid handle.

### preview_sheet
Streams a bounded preview of the first N rows of a sheet for quick inspection of headers and data types. Use when you need a lightweight snapshot to confirm structure before targeted reads/filters. Supports pagination with unit=rows; when `cursor` is provided it overrides `sheet`/`rows`.
Parameters: `path`, `sheet`, `rows` (bounded; defaults to PreviewRowLimit), `encoding` (`json` or `csv`), and optional `cursor` (rows unit, bound to path+mtime). Output includes a concise summary line and preview data in text plus `meta{total,returned,truncated,nextCursor}`. Errors map invalid sheets and cursor mismatches.

### read_range
Returns a bounded rectangular cell range (A1 or named range) with cell‑level pagination (unit=cells). Use when you know the exact range to extract and want deterministic row‑major pagination. When `cursor` is provided it overrides `sheet`/`range`/`max_cells` and resumes by cell offset.
Parameters: `path`, `sheet`, `range` (A1 or named), `max_cells` (bounded; defaults to MaxCellsPerOp), optional `cursor` (cells unit, bound to path+mtime). Text payload is a JSON array‑of‑arrays preceded by a one‑line summary; structured output includes `meta`. Errors cover invalid ranges, invalid sheet, cursor mismatch, and read failures.

### search_data
Finds values or regex matches in a sheet and returns a bounded page of results with cell coordinates and a limited row snapshot. Use to locate records of interest without scanning entire sheets in the LLM context. Supports pagination with unit=rows and binds cursors to query parameters.
Parameters: `path`, `sheet`, `query`, `regex` (treat query as regular expression), `columns` (optional 1‑based restrictor), `max_results` (bounded), `snapshot_cols` (bounded), and optional `cursor` (rows unit, query/hash bound, path+mtime bound). Output: `results[]` with `cell`, `row`, `column`, `value`, `snapshot`, and `meta`. Caveats: regex complexity/escaping; large sheets may require narrowing scope. Errors include `INVALID_SHEET`, `CURSOR_INVALID`, and validation.

### filter_data
Filters rows using a boolean predicate over 1‑based column references and returns a bounded page of matching rows with a limited snapshot. Use when you can express conditions like `$1 = "foo" AND $3 > 100`. Supports `AND`, `OR`, `NOT`, `=`, `!=`, `>`, `>=`, `<`, `<=`, and `contains` on string values. Pagination uses unit=rows and binds cursors to predicate/column scope.
Parameters: `path`, `sheet`, `predicate` (grammar above), `columns` (optional 1‑based columns to evaluate), `max_rows` (bounded), `snapshot_cols` (bounded), and optional `cursor` (rows unit, predicate/columns hash bound, path+mtime bound). Output: matched rows with snapshots and `meta`. Errors include predicate validation, `INVALID_SHEET`, and cursor mismatches.

### sequential_insights
Domain‑neutral planning tool that helps orchestrate multi‑step spreadsheet analysis without a server‑embedded LLM. Provides clarifying questions, recommended tool calls (with confidence, rationale, priority, and suggested inputs), and optionally lightweight insight cards when bounded compute is enabled. Use for complex objectives that benefit from iterative planning and course correction (sequential thinking style). Planning‑only by default (no compute); client LLM narrates and executes recommended tools.
Parameters: `objective` (analysis goal), `path` or `cursor` (cursor takes precedence), `hints` (e.g., sheet, range, date_col, id_col, measure, target, stages), `constraints` (caps such as max_rows, top_n), step‑tracking fields (`step_number`, `total_steps`, `next_step_needed`), and optional `revision`/`branch` identifiers. Output: `current_step`, `recommended_tools[]` (tool_name, confidence, rationale, priority, suggested_inputs, alternatives), `questions[]`, `insight_cards[]` (often empty), and `meta{limits,planning_only,compute_enabled,truncated}`. Errors: `PLANNING_FAILED` on internal errors.

### detect_tables
Detects rectangular table regions within a sheet via a streaming scan (multiple tables per sheet) and returns Top‑K candidates with header previews and confidence. Use to quickly identify where structured data resides before further profiling/reads. Works on large sheets using streaming heuristics and respects global caps.
Parameters: `path`, `sheet`, and optional scan caps (implementation defaults apply). Output: `candidates[]` with `range`, `rows`, `cols`, `header` (bounded preview), `confidence`, plus `meta{scanned_rows,scanned_cols,truncated}`. Errors: `INVALID_SHEET` and `DETECTION_FAILED`.

### profile_schema
Profiles a bounded sample of a rectangular range to infer column roles (measure, dimension, time, id, target) and data quality indicators (missingness, uniqueness, type mix, impossible values). Use to validate assumptions and drive subsequent analysis steps. Sampling keeps memory bounded.
Parameters: `path`, `sheet`, `range`, plus optional sampling thresholds (implementation defaults). Output: `columns[]` with `index`, `name`, `role`, `type`, `missingPct`, `uniqueRatio`, `warnings`, and `meta{sampled_rows,truncated}`. Errors: `INVALID_SHEET`, `VALIDATION` for invalid ranges, `PROFILING_FAILED`.

### composition_shift
Computes share‑of‑total by group across two periods and highlights mix shifts in percentage points (±pp). Use to quantify how composition changes contribute to KPI movement. Applies Top‑N with “Other” when groups exceed caps; includes truncation metadata.
Parameters: `path`, `sheet`, `range`, and hints for group/time/measure selection. Output: baseline vs current shares per group, Top‑N, `meta{...}` and a concise summary. Errors: `INVALID_SHEET`, `VALIDATION` for invalid ranges, `ANALYSIS_FAILED`.

### concentration_metrics
Calculates Top‑N share and Herfindahl‑Hirschman Index (HHI) concentration metrics for a chosen dimension. Use to assess market/provider/customer concentration risks. Reports HHI value with banding (<0.15 unconcentrated, 0.15–0.25 moderate, >0.25 high) and includes truncation metadata.
Parameters: `path`, `sheet`, `range`, with required dimension/measure hints. Output: group shares, Top‑N, HHI value and band, and `meta`. Errors: `INVALID_SHEET`, `VALIDATION` for invalid ranges, `ANALYSIS_FAILED`.

### funnel_analysis
Derives stage and cumulative conversion across an ordered set of funnel stages; highlights bottlenecks and optionally overlays simple segments. Use for adoption/sales/process funnels when stage columns are ordered or inferable by name patterns. Outputs are bounded and include truncation metadata.
Parameters: `path`, `sheet`, `range`, plus stage name patterns/hints and optional segment overlays. Output: `stages[]` with step and cumulative conversion, `bottleneck`, and `meta`. Errors: `INVALID_SHEET`, `VALIDATION` for invalid ranges, `ANALYSIS_FAILED`.

## Implementation Steps
1) Update descriptions in `internal/registry/tools_foundation.go` for: list_structure, preview_sheet, read_range, search_data, filter_data.
2) Update descriptions in `internal/registry/insights.go` for: sequential_insights (sequential thinking style), detect_tables, profile_schema, composition_shift, concentration_metrics, funnel_analysis.
3) Strengthen parameter `mcp.Description(...)` for cursor precedence, units, column indexing (1‑based), predicate grammar, regex semantics, encoding, and snapshot bounds.
4) Build and validate: `make lint && make test && make test-race`.
5) Smoke test: run server, call `list_tools`, confirm descriptions and parameter help render fully and accurately.

## Acceptance Criteria
- Each tool description is ≥4 sentences and covers: purpose, when to use, parameter semantics (including cursor precedence), output & pagination metadata, limits/caps, and key caveats.
- Sequential insights description reflects sequential thinking guidance and step‑oriented recommendations.
- Parameter descriptions document indexing and grammar where relevant (predicates, regex, encoding, snapshot bounds, units).
- `list_tools` exposes updated long‑form descriptions and enriched parameter help without breaking schemas.
- Lint and tests pass (`make lint`, `make test`, `make test-race`).

## Validation Notes
- Ensure preview/read text content remains prefixed with a one‑line summary; structured metadata is unchanged.
- Verify cursor unit expectations per tool: preview/search/filter → rows; read_range → cells.
- Confirm error mapping text remains stable to avoid client regressions.

