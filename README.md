# MCPXcel — Excel Analysis MCP Server

<p align="center">
  <img src="banner.png" alt="MCPXcel banner">
 </p>

Go-based Model Context Protocol (MCP) server for targeted Excel analysis. MCPXcel lets AI assistants work with large spreadsheets efficiently by returning only the slices and summaries they need, and by providing deterministic, bounded analytics primitives — all without embedding an LLM in the server.

Works over stdio with any MCP-compatible client.

## Features
- Path-first API: tools accept a canonical `path` (or `cursor` for pagination) — no workbook IDs.
- Structure and preview: discover sheet layout and stream small previews to ground follow-up steps.
- Targeted reads: fetch a bounded A1 range with stable, resumable pagination.
- Find and filter: literal/regex search with row snapshots and boolean-predicate filtering (`$N` refs).
- Statistics: bounded per-column stats with optional group-by and distinct counts.
- Insights (deterministic): multi-table detection, schema profiling, composition/mix shift, concentration/HHI, funnel analysis.
- Sequential planning: a lightweight `sequential_insights` tool to track thoughts between domain calls.
- Guardrails: directory allow-list, concurrency caps, 10k-cell and 128KB payload bounds, timeouts.
- Optional writes: gated behind `MCPXCEL_ENABLE_WRITES` to avoid unintended mutations.

## Getting Started

### Prerequisites
- Go 1.25+
- An MCP client (e.g., Claude Desktop, VS Code MCP extension, or any client that launches an MCP server over stdio)
- Optional: GitHub CLI (`gh`) for PR/release workflows

### Installation

Quick install (recommended):
```bash
# Install Go (macOS):
brew install go

# Install the MCP server binary:
go install github.com/vinodismyname/mcpxcel/cmd/server@latest

# Ensure your PATH includes the Go bin dir (if needed):
export PATH="$(go env GOPATH)/bin:$PATH"
```

Alternative: build from source
```bash
git clone https://github.com/vinodismyname/mcpxcel.git
cd mcpxcel
make build   # or: go build ./cmd/server
```

### Running the Server
The server speaks MCP over stdio. Set an allow-list for workbook directories and run with `--stdio`:

```bash
export MCPXCEL_ALLOWED_DIRS="$HOME/Documents:/data"
server --stdio                      # if installed via `go install`
# or
./cmd/server --stdio                # if built from source
```

Tip: keep logs out of the transport by writing only to stderr. This server uses structured logging and recovery hooks by default.

## Usage

### Connecting From an MCP Client
Configure your MCP client to launch the binary with stdio and pass environment variables. Example (pseudoconfig):

```json
{
  "mcpServers": {
    "mcpxcel": {
    "command": "/absolute/path/to/server",  
    // or the path to your built binary at ./cmd/server
      "args": ["--stdio"],
      "env": {
        "MCPXCEL_ALLOWED_DIRS": "/Users/you/Documents:/data",
        "MCPXCEL_ENABLE_WRITES": "false"
      }
    }
  }
}
```

Once connected, call `list_tools` in your client to discover schemas and defaults.

### Available Tools (Overview)
- `list_structure` — Summarize workbook sheets (name, rows, cols, optional header inference). Use first.
- `preview_sheet` — Stream first N rows (encoding `json` or `csv`). Paginates by rows; emits `meta.total/returned/truncated/nextCursor` and a one-line summary prefix in text output.
- `read_range` — Return a bounded A1 range (array-of-arrays). Paginates by cells; emits meta and summary prefix.
- `search_data` — Find literal or RE2 regex matches, optionally restricted to specific columns; returns cell coords plus a left-anchored row snapshot. Row-pagination with cursor.
- `filter_data` — Apply boolean predicates with `$N` (1-based) column refs and AND/OR/NOT; returns matched rows with bounded snapshots. Row-pagination with cursor.
- `compute_statistics` — Per-column stats (count, sum, avg, min, max, distinct), optional group-by within a range; truncation-safe.
- `write_range` — Write a bounded 2D block using a stream writer; hidden unless `MCPXCEL_ENABLE_WRITES=true`.
- `sequential_insights` — Planning-only thought tracker to interleave with domain tools; includes a tiny “NextAction” card.
- `detect_tables` — Identify multiple rectangular table regions in a sheet with header samples and confidence.
- `profile_schema` — Infer column roles/types and surface quality flags/questions over a bounded sample.
- `composition_shift` — Top-N share across two periods with percent-point mix shifts (groups + Other).
- `concentration_metrics` — Top-N share breakdown plus HHI and band (unconcentrated/moderate/high).
- `funnel_analysis` — Stage and cumulative conversion across ordered stages; detects stages from headers or accepts indices.

All read/analysis tools return structured metadata with at least: `total`, `returned`, `truncated`, and `nextCursor` (when applicable). Cursors bind to file `path` and `mtime` for deterministic resume.

### Example Interactions

1) Discover structure
```json
{
  "name": "list_structure",
  "arguments": { "path": "/data/sales.xlsx", "metadata_only": false }
}
```

2) Preview first 10 rows as JSON
```json
{
  "name": "preview_sheet",
  "arguments": { "path": "/data/sales.xlsx", "sheet": "Sheet1", "rows": 10, "encoding": "json" }
}
```

3) Read a small range with pagination
```json
{
  "name": "read_range",
  "arguments": { "path": "/data/sales.xlsx", "sheet": "Sheet1", "range": "A1:D500", "max_cells": 500 }
}
```
When `meta.truncated` is true, pass `meta.nextCursor` back as `cursor` to resume.

4) Search with a regex and left-anchored snapshots
```json
{
  "name": "search_data",
  "arguments": { "path": "/data/sales.xlsx", "sheet": "Sheet1", "query": "^ACME.*", "regex": true, "max_results": 50, "snapshot_cols": 16 }
}
```

5) Filter with a predicate
```json
{
  "name": "filter_data",
  "arguments": { "path": "/data/sales.xlsx", "sheet": "Sheet1", "predicate": "$1 contains 'west' AND $3 > 1000", "snapshot_cols": 12 }
}
```

6) Compute statistics with group-by
```json
{
  "name": "compute_statistics",
  "arguments": { "path": "/data/sales.xlsx", "sheet": "Sheet1", "range": "A1:D2000", "column_indices": [2,4], "group_by_index": 2, "max_cells": 8000 }
}
```

7) Insights and profiling examples
- `detect_tables`: `{ path, sheet, max_tables, header_sample_rows, header_sample_cols }`
- `profile_schema`: `{ path, sheet, range, max_sample_rows }`
- `composition_shift`: `{ path, sheet, range, dimension_index, measure_index, time_index, top_n, mix_threshold_pp }`
- `concentration_metrics`: `{ path, sheet, range, dimension_index, measure_index, top_n }`
- `funnel_analysis`: `{ path, sheet, range, stage_indices }` (or let stages be detected from headers)

## Configuration

### Environment Variables
- `MCPXCEL_ALLOWED_DIRS` (required) — OS path-list of directories that the server may read/write (e.g., `"/Users/you/Documents:/data"`). Requests outside these roots are denied.
- `MCPXCEL_ENABLE_WRITES` (optional, default false) — When `true` (or `1`/`yes`), exposes write/transform tools such as `write_range` in `list_tools`.

### Effective Limits (defaults)
Defined in `config/defaults.go` and surfaced in responses where relevant:
- Concurrency: `MaxConcurrentRequests=10`, `MaxOpenWorkbooks=4`
- Payload/cell bounds: `MaxPayloadBytes=128KB`, `MaxCellsPerOp=10,000`, `PreviewRowLimit=10`
- Timeouts: `OperationTimeout=30s`, `AcquireRequestTimeout=2s`
- Workbook cache: idle TTL `5m`, cleanup period `30s`

## Development
- `make run` — start the server with `--stdio`
- `make build` — compile into `./cmd/server`
- `make lint` — gofmt/goimports (if available) + go vet
- `make test` — unit tests with coverage
- `make test-race` — race-enabled tests for `internal/...`

Module import path: `github.com/vinodismyname/mcpxcel`

## Releases
- See releases: https://github.com/vinodismyname/mcpxcel/releases
