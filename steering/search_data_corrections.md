# Search Data Tool – Corrections Plan

This plan addresses functional issues observed during MCP client testing of `search_data` against `Data_definition_v1.xlsx` and prepares precise implementation steps.

## Summary of Issues

- Missing `nextCursor` on truncated results
  - Symptom: Truncated searches did not return a cursor.
  - Root cause: Cursor field `r` was left empty (`""`); `pkg/pagination.EncodeCursor` requires non-empty `r`, failing silently where error was ignored.

- Invalid sheet error mapping
  - Symptom: Searches on non-existent sheets returned `SEARCH_FAILED` instead of `INVALID_SHEET`.
  - Root cause: Error message check only matched “doesn’t exist” but excelize uses “does not exist”.

- Results not visible in some clients
  - Symptom: UI showed only the fallback summary (e.g., `matches=...`) without result rows/snapshots.
  - Root cause: Handler didn’t set a text content payload for results; some clients display the fallback when no explicit content is provided.

- Snapshot anchoring
  - Symptom: Snapshots may start at absolute column `A` instead of the sheet’s leftmost used column.
  - Root cause: Snapshot loop starts at 1, not the left bound from `GetSheetDimension`.

- Cursor `qh` stability across pages
  - Symptom: On cursor-only resume, a new `qh` could be computed as empty, breaking future validation.
  - Root cause: Next-page cursor always recomputed `qh` from inputs even when resuming from an existing cursor.

## Changes to Implement

1. Cursor construction and validation
   - Set `Cursor.R` to the sheet used range from `GetSheetDimension` (normalized `A1:DNN`).
   - On pagination resume (`cursor` provided): propagate `pc.Qh` into the next cursor; only recompute `qh` for the first page or when inputs are explicitly provided and validated to match.
   - Handle encode failures: if `EncodeCursor` returns error, map to `CURSOR_BUILD_FAILED` with guidance.

2. Error mapping for invalid sheet
   - Map both “does not exist” and “doesn’t exist” (and optionally excelize `ErrSheetNotExist`) to `INVALID_SHEET`.

3. Results content for client visibility
   - Attach `res.Content = []mcp.Content{ mcp.NewTextContent(<JSON results>) }` alongside structured output (which includes metadata). Keep summary as fallback text.

4. Snapshot anchoring and bounds
   - Derive `x1..x2` from `GetSheetDimension` and snapshot columns `[x1, min(x1+snapshotCols-1, x2)]` for each matched row.

5. Minor polish
   - Optionally echo the effective query in output when resuming via cursor only (e.g., set from `pc.Qh` with a note, or omit field when unknown).

## Implementation Pointers (file: internal/registry/tools_foundation.go)

- In `search_data` handler:
  - Capture `sheetRange`, `x1`, `x2` from `GetSheetDimension`.
  - Replace snapshot loop to start at `x1`.
  - When `meta.Truncated`:
    - If `parsedCur != nil`, set `qh := parsedCur.Qh`; else `qh := computeQueryHash(query, regex, in.Columns)`.
    - Build cursor with `R: sheetRange` and `Qh: qh`; on encode error, return `CURSOR_BUILD_FAILED`.
  - Error mapping: check for both spellings of “does not exist”.
  - After assembling `output`, marshal `output.Results` to compact JSON and set `res.Content` with that string.

## Acceptance Criteria

- Truncated search responses include a non-empty `meta.nextCursor` that decodes via `pagination.DecodeCursor`.
- Resuming with the returned cursor returns the next page; mismatched query/filters yield `CURSOR_INVALID`.
- Invalid sheet searches return `INVALID_SHEET` with clear guidance.
- Clients display a text payload containing the serialized `results` while structured metadata remains available.
- Snapshots contain the expected number of columns and align to the left bound of the sheet’s used range.

## Test Plan (MCP client prompts)

1. Truncation + cursor
   - Call `search_data` on a sheet with many matches (e.g., regex `"[0-9]"`, `max_results=2`).
   - Verify `meta.nextCursor` is present; decode succeeds; resume returns different hits.

2. Cursor parameter mismatch
   - Resume with the cursor but pass a different `query`; expect `CURSOR_INVALID`.

3. Invalid sheet
   - `sheet="DoesNotExist"` returns `INVALID_SHEET`.

4. Results visibility
   - Confirm the client shows a text payload (JSON array of results) in addition to metadata.

5. Snapshot bounds
   - Run with `snapshot_cols=3` and `snapshot_cols=50`; verify snapshot widths and left anchoring.

6. QH stability
   - First call (no cursor): ensure `nextCursor` includes `qh`.
   - Resume with cursor-only inputs; ensure subsequent `nextCursor` preserves identical `qh`.

## Out of Scope

- Performance optimizations (e.g., batch row reads) and advanced regex safety tuning are deferred.

## Rollback Plan

- Changes are localized to the `search_data` handler. Revert by restoring the previous version of `internal/registry/tools_foundation.go` if needed.

