Title: Path-Only API Refactor

Summary
- Replace client-visible `workbook_id` flow with a path-only API across all tools.
- Cursors become path-bound: include canonical `pt` (path) and `mt` (file mtime) to survive server restarts and detect mid-stream edits.
- Internals keep a TTL-based handle cache keyed by canonical path; transparent to clients.

Scope
- Tools: list_structure, preview_sheet, read_range, search_data, filter_data, write_range, apply_formula, compute_statistics.
- Inputs: add/require `path` (unless `cursor` is provided, which takes precedence).
- Remove open_workbook/close_workbook tools from the surface area.

Cursor Changes
- Add fields: `pt` (canonical path), `mt` (file mtime, unix sec).
- Keep `q/rg/cl` (search provenance) and `p/cl` (filter provenance); keep `ps`, `off`, `u`.
- Drop `wid/wbv`.

Server Changes
- internal/workbooks: add by-path lookup and `GetOrOpenByPath` helpers; refresh TTL on access; preserve concurrency limits.
- internal/registry: update input schemas and handlers to accept `path` or `cursor`. Cursor takes precedence and carries `pt`.
- pkg/pagination: extend cursor struct to add `Pt` and `Mt`.

Error Semantics
- CURSOR_INVALID if decode fails, file missing/inaccessible, or `mt` mismatch.

Migration Notes
- Remove `open_workbook` and `close_workbook` from registry and docs.
- Update steering docs and requirements to reflect path-first design.

Validation Plan
- Make lint/test/test-race green.
- Manual MCP client test: search + filter + pagination with resume after server restart.

