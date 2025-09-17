package registry

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/vinoddu/mcpxcel/internal/runtime"
	"github.com/vinoddu/mcpxcel/internal/workbooks"
	"github.com/xuri/excelize/v2"
)

// --- Input / Output Schemas (typed for discovery) ---

// OpenWorkbookInput defines parameters for opening a workbook.
type OpenWorkbookInput struct {
	Path string `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
}

// OpenWorkbookOutput documents the response fields for open_workbook.
type OpenWorkbookOutput struct {
	WorkbookID      string `json:"workbook_id" jsonschema_description:"Server-assigned workbook handle ID"`
	MaxPayloadBytes int    `json:"maxPayloadBytes" jsonschema_description:"Effective payload size limit in bytes"`
	PreviewRowLimit int    `json:"previewRowLimit" jsonschema_description:"Default row limit for previews"`
}

// CloseWorkbookInput defines parameters for closing a workbook.
type CloseWorkbookInput struct {
	WorkbookID string `json:"workbook_id" jsonschema_description:"Workbook handle ID to close"`
}

// SheetInfo summarizes a sheet without loading full data.
type SheetInfo struct {
	Name        string   `json:"name" jsonschema_description:"Sheet name"`
	RowCount    int      `json:"rowCount" jsonschema_description:"Approximate row count"`
	ColumnCount int      `json:"columnCount" jsonschema_description:"Approximate column count"`
	Headers     []string `json:"headers,omitempty" jsonschema_description:"Header row when inferred"`
}

// ListStructureInput defines parameters for structure discovery.
type ListStructureInput struct {
	WorkbookID   string `json:"workbook_id" jsonschema_description:"Workbook handle ID"`
	MetadataOnly bool   `json:"metadata_only,omitempty" jsonschema_description:"Return only metadata even for small sheets"`
}

// ListStructureOutput summarizes workbook structure.
type ListStructureOutput struct {
	WorkbookID   string      `json:"workbook_id"`
	MetadataOnly bool        `json:"metadata_only"`
	Sheets       []SheetInfo `json:"sheets"`
}

// PreviewSheetInput defines parameters for previewing a sheet.
type PreviewSheetInput struct {
	WorkbookID string `json:"workbook_id" jsonschema_description:"Workbook handle ID"`
	Sheet      string `json:"sheet" jsonschema_description:"Sheet name to preview"`
	Rows       int    `json:"rows,omitempty" jsonschema_description:"Max rows to preview (bounded)"`
	Encoding   string `json:"encoding,omitempty" jsonschema_description:"Output encoding: json or csv"`
}

// PageMeta captures paging/truncation metadata.
type PageMeta struct {
	Total      int    `json:"total"`
	Returned   int    `json:"returned"`
	Truncated  bool   `json:"truncated"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// PreviewSheetOutput documents preview metadata.
type PreviewSheetOutput struct {
	WorkbookID string   `json:"workbook_id"`
	Sheet      string   `json:"sheet"`
	Encoding   string   `json:"encoding"`
	Meta       PageMeta `json:"meta"`
}

// ReadRangeInput defines parameters for reading a cell range.
type ReadRangeInput struct {
	WorkbookID string `json:"workbook_id" jsonschema_description:"Workbook handle ID"`
	Sheet      string `json:"sheet" jsonschema_description:"Sheet name"`
	RangeA1    string `json:"range" jsonschema_description:"A1-style cell range (e.g., A1:D50)"`
	MaxCells   int    `json:"max_cells,omitempty" jsonschema_description:"Max cells to return (bounded)"`
}

// ReadRangeOutput documents range read metadata.
type ReadRangeOutput struct {
	WorkbookID string   `json:"workbook_id"`
	Sheet      string   `json:"sheet"`
	RangeA1    string   `json:"range"`
	Meta       PageMeta `json:"meta"`
}

// RegisterFoundationTools defines core tool schemas and placeholder handlers.
// Handlers intentionally return UNIMPLEMENTED until later tasks wire logic.
func RegisterFoundationTools(s *server.MCPServer, reg *Registry, limits runtime.Limits, mgr *workbooks.Manager) {
	// open_workbook
	openTool := mcp.NewTool(
		"open_workbook",
		mcp.WithDescription("Open a workbook and return a handle ID with effective limits"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or allowed path to an Excel workbook (.xlsx, .xlsm, .xltx, .xltm)")),
		mcp.WithOutputSchema[OpenWorkbookOutput](),
	)
	s.AddTool(openTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in OpenWorkbookInput) (*mcp.CallToolResult, error) {
		if strings.TrimSpace(in.Path) == "" {
			return mcp.NewToolResultError("VALIDATION: path is required"), nil
		}

		id, err := mgr.Open(ctx, in.Path)
		if err != nil {
			// Map common error categories to actionable messages
			msg := err.Error()
			lower := strings.ToLower(msg)
			switch {
			case strings.Contains(lower, "unsupported format"):
				return mcp.NewToolResultError("UNSUPPORTED_FORMAT: only .xlsx, .xlsm, .xltx, .xltm supported"), nil
			case strings.Contains(lower, "denied") || strings.Contains(lower, "not allowed"):
				return mcp.NewToolResultError("NOT_ALLOWED: path outside allowed directories"), nil
			case strings.Contains(lower, "not found"):
				return mcp.NewToolResultError("NOT_FOUND: file not found or inaccessible"), nil
			case err == context.DeadlineExceeded:
				return mcp.NewToolResultError("BUSY_RESOURCE: open workbook capacity reached; retry later"), nil
			default:
				return mcp.NewToolResultError(fmt.Sprintf("OPEN_FAILED: %v", err)), nil
			}
		}

		out := OpenWorkbookOutput{
			WorkbookID:      id,
			MaxPayloadBytes: limits.MaxPayloadBytes,
			PreviewRowLimit: limits.PreviewRowLimit,
		}
		fallback := fmt.Sprintf("workbook_id=%s previewRowLimit=%d", out.WorkbookID, out.PreviewRowLimit)
		return mcp.NewToolResultStructured(out, fallback), nil
	}))
	reg.Register(openTool)

	// close_workbook
	closeTool := mcp.NewTool(
		"close_workbook",
		mcp.WithDescription("Close a previously opened workbook handle"),
		mcp.WithString("workbook_id", mcp.Required(), mcp.Description("Workbook handle ID")),
		mcp.WithOutputSchema[struct {
			Success bool `json:"success" jsonschema_description:"True when the handle was closed"`
		}](),
	)
	s.AddTool(closeTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in CloseWorkbookInput) (*mcp.CallToolResult, error) {
		id := strings.TrimSpace(in.WorkbookID)
		if id == "" {
			return mcp.NewToolResultError("VALIDATION: workbook_id is required"), nil
		}
		if err := mgr.CloseHandle(ctx, id); err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("CLOSE_FAILED: %v", err)), nil
		}
		out := struct {
			Success bool `json:"success" jsonschema_description:"True when the handle was closed"`
		}{Success: true}
		return mcp.NewToolResultStructured(out, "closed"), nil
	}))
	reg.Register(closeTool)

	// list_structure
	listStructure := mcp.NewTool(
		"list_structure",
		mcp.WithDescription("Return workbook structure: sheets, dimensions, headers (no cell data)"),
		mcp.WithString("workbook_id", mcp.Required(), mcp.Description("Workbook handle ID")),
		mcp.WithBoolean("metadata_only", mcp.DefaultBool(false), mcp.Description("Return only metadata even for small sheets")),
		mcp.WithOutputSchema[ListStructureOutput](),
	)
	s.AddTool(listStructure, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in ListStructureInput) (*mcp.CallToolResult, error) {
		id := strings.TrimSpace(in.WorkbookID)
		if id == "" {
			return mcp.NewToolResultError("VALIDATION: workbook_id is required"), nil
		}

		var output ListStructureOutput
		output.WorkbookID = id
		output.MetadataOnly = in.MetadataOnly

		err := mgr.WithRead(id, func(f *excelize.File) error {
			// Gather sheet names in index order
			sheetMap := f.GetSheetMap()
			idx := make([]int, 0, len(sheetMap))
			for i := range sheetMap {
				idx = append(idx, i)
			}
			sort.Ints(idx)

			sheets := make([]SheetInfo, 0, len(idx))
			for _, i := range idx {
				name := sheetMap[i]
				si := SheetInfo{Name: name}

				if dim, derr := f.GetSheetDimension(name); derr == nil && dim != "" {
					// dim like "A1:D50"; parse right cell for bounds
					parts := strings.Split(dim, ":")
					if len(parts) == 2 {
						x1, y1, e1 := excelize.CellNameToCoordinates(parts[0])
						x2, y2, e2 := excelize.CellNameToCoordinates(parts[1])
						if e1 == nil && e2 == nil {
							if x2 >= x1 {
								si.ColumnCount = x2 - x1 + 1
							}
							if y2 >= y1 {
								si.RowCount = y2 - y1 + 1
							}
						}
					}
				}

				if !in.MetadataOnly {
					// Infer header from first row via streaming iterator
					rows, rerr := f.Rows(name)
					if rerr == nil {
						if rows.Next() {
							if hdr, herr := rows.Columns(); herr == nil {
								si.Headers = hdr
							}
						}
						_ = rows.Close()
					}
				}

				sheets = append(sheets, si)
			}
			output.Sheets = sheets
			return nil
		})
		if err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("DISCOVERY_FAILED: %v", err)), nil
		}

		// Build a human-readable summary including sheet names and dimensions
		var b strings.Builder
		fmt.Fprintf(&b, "sheets=%d metadata_only=%v\n", len(output.Sheets), output.MetadataOnly)
		for _, sh := range output.Sheets {
			fmt.Fprintf(&b, "- %q rows=%d cols=%d", sh.Name, sh.RowCount, sh.ColumnCount)
			if len(sh.Headers) > 0 {
				// show up to first 8 headers to keep concise
				max := len(sh.Headers)
				if max > 8 {
					max = 8
				}
				fmt.Fprintf(&b, " headers=%v", sh.Headers[:max])
				if len(sh.Headers) > max {
					b.WriteString("…")
				}
			}
			b.WriteByte('\n')
		}
		summary := b.String()

		res := mcp.NewToolResultStructured(output, summary)
		// Ensure clients that ignore structured content still see the summary
		res.Content = []mcp.Content{mcp.NewTextContent(summary)}
		return res, nil
	}))
	reg.Register(listStructure)

	// preview_sheet
	preview := mcp.NewTool(
		"preview_sheet",
		mcp.WithDescription("Stream a bounded preview of the first N rows of a sheet"),
		mcp.WithString("workbook_id", mcp.Required(), mcp.Description("Workbook handle ID")),
		mcp.WithString("sheet", mcp.Required(), mcp.Description("Sheet name to preview")),
		mcp.WithNumber("rows", mcp.DefaultNumber(float64(limits.PreviewRowLimit)), mcp.Min(1), mcp.Max(1000), mcp.Description("Max rows to preview")),
		mcp.WithString("encoding", mcp.DefaultString("json"), mcp.Enum("json", "csv"), mcp.Description("Output encoding")),
		mcp.WithOutputSchema[PreviewSheetOutput](),
	)
	s.AddTool(preview, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in PreviewSheetInput) (*mcp.CallToolResult, error) {
		id := strings.TrimSpace(in.WorkbookID)
		sheet := strings.TrimSpace(in.Sheet)
		if id == "" || sheet == "" {
			return mcp.NewToolResultError("VALIDATION: workbook_id and sheet are required"), nil
		}
		rowsLimit := in.Rows
		if rowsLimit <= 0 || rowsLimit > 1000 {
			rowsLimit = limits.PreviewRowLimit
		}
		enc := strings.ToLower(strings.TrimSpace(in.Encoding))
		if enc == "" {
			enc = "json"
		}
		if enc != "json" && enc != "csv" {
			return mcp.NewToolResultError("VALIDATION: encoding must be 'json' or 'csv'"), nil
		}

		meta := PageMeta{}
		// Accumulate preview in selected encoding
		var textOut string
		err := mgr.WithRead(id, func(f *excelize.File) error {
			// Total rows from dimension when available
			if dim, derr := f.GetSheetDimension(sheet); derr == nil && dim != "" {
				parts := strings.Split(dim, ":")
				if len(parts) == 2 {
					_, y1, e1 := excelize.CellNameToCoordinates(parts[0])
					_, y2, e2 := excelize.CellNameToCoordinates(parts[1])
					if e1 == nil && e2 == nil && y2 >= y1 {
						meta.Total = y2 - y1 + 1
					}
				}
			}

			r, rerr := f.Rows(sheet)
			if rerr != nil {
				return rerr
			}
			defer r.Close()

			// Prepare encoders
			if enc == "json" {
				// Build a JSON array of rows (array of arrays)
				var buf bytes.Buffer
				buf.WriteByte('[')
				count := 0
				for r.Next() {
					if count >= rowsLimit {
						break
					}
					row, cerr := r.Columns()
					if cerr != nil {
						return cerr
					}
					if count > 0 {
						buf.WriteByte(',')
					}
					// serialize row as JSON array
					b, merr := json.Marshal(row)
					if merr != nil {
						return merr
					}
					buf.Write(b)
					count++
				}
				buf.WriteByte(']')
				textOut = buf.String()
				meta.Returned = count
			} else {
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				count := 0
				for r.Next() {
					if count >= rowsLimit {
						break
					}
					row, cerr := r.Columns()
					if cerr != nil {
						return cerr
					}
					if err := w.Write(row); err != nil {
						return err
					}
					count++
				}
				w.Flush()
				if err := w.Error(); err != nil {
					return err
				}
				textOut = buf.String()
				meta.Returned = count
			}

			// Compute truncation and cursor
			meta.Truncated = meta.Total > 0 && meta.Returned < meta.Total
			if meta.Truncated {
				meta.NextCursor = fmt.Sprintf("sheet=%s&offset=%d", sheet, meta.Returned)
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			if strings.Contains(strings.ToLower(err.Error()), "doesn't exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("PREVIEW_FAILED: %v", err)), nil
		}

		out := PreviewSheetOutput{
			WorkbookID: id,
			Sheet:      sheet,
			Encoding:   enc,
			Meta:       meta,
		}
		// Text content carries the actual preview data; structured carries metadata
		res := mcp.NewToolResultStructured(out, "preview generated")
		res.Content = []mcp.Content{mcp.NewTextContent(textOut)}
		return res, nil
	}))
	reg.Register(preview)

	// read_range
	readRange := mcp.NewTool(
		"read_range",
		mcp.WithDescription("Return a bounded cell range with pagination metadata"),
		mcp.WithString("workbook_id", mcp.Required(), mcp.Description("Workbook handle ID")),
		mcp.WithString("sheet", mcp.Required(), mcp.Description("Sheet name")),
		mcp.WithString("range", mcp.Required(), mcp.Description("A1-style cell range or named range (e.g., A1:D50)")),
		mcp.WithNumber("max_cells", mcp.DefaultNumber(float64(limits.MaxCellsPerOp)), mcp.Min(1), mcp.Description("Max cells to return before truncation")),
		mcp.WithOutputSchema[ReadRangeOutput](),
	)
	s.AddTool(readRange, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in ReadRangeInput) (*mcp.CallToolResult, error) {
		id := strings.TrimSpace(in.WorkbookID)
		sheet := strings.TrimSpace(in.Sheet)
		rng := strings.TrimSpace(in.RangeA1)
		if id == "" || sheet == "" || rng == "" {
			return mcp.NewToolResultError("VALIDATION: workbook_id, sheet, and range are required"), nil
		}
		maxCells := in.MaxCells
		if maxCells <= 0 || maxCells > limits.MaxCellsPerOp {
			maxCells = limits.MaxCellsPerOp
		}

		// We will build a JSON array-of-arrays payload in text form to keep memory bounded
		var textOut string
		var meta PageMeta
		var outRange = rng

		err := mgr.WithRead(id, func(f *excelize.File) error {
			// Resolve named range if needed
			var x1, y1, x2, y2 int
			var parseErr error
			x1, y1, x2, y2, outRange, parseErr = resolveRange(f, sheet, rng)
			if parseErr != nil {
				return parseErr
			}

			if x2 < x1 || y2 < y1 {
				return fmt.Errorf("invalid range bounds after parse")
			}

			total := (x2 - x1 + 1) * (y2 - y1 + 1)
			meta.Total = total

			// Iterate row-major, but stop when we reach maxCells
			// Build JSON array-of-arrays
			var buf bytes.Buffer
			buf.WriteByte('[')
			writtenCells := 0
			returnedRows := 0
			stop := false
			for row := y1; row <= y2 && !stop; row++ {
				// For each row, emit an array of columns
				if returnedRows > 0 {
					buf.WriteByte(',')
				}
				buf.WriteByte('[')
				colsWritten := 0
				for col := x1; col <= x2; col++ {
					if writtenCells >= maxCells {
						stop = true
						break
					}
					if colsWritten > 0 {
						buf.WriteByte(',')
					}
					cellName, _ := excelize.CoordinatesToCellName(col, row)
					val, _ := f.GetCellValue(sheet, cellName)
					b, _ := json.Marshal(val)
					buf.Write(b)
					colsWritten++
					writtenCells++
				}
				buf.WriteByte(']')
				returnedRows++
			}
			buf.WriteByte(']')
			textOut = buf.String()
			meta.Returned = writtenCells
			meta.Truncated = writtenCells < total
			if meta.Truncated {
				meta.NextCursor = fmt.Sprintf("sheet=%s&range=%s&offset=%d", sheet, outRange, writtenCells)
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			// Map validation-ish errors
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "invalid range") || strings.Contains(lower, "coordinates") {
				return mcp.NewToolResultError("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			if strings.Contains(lower, "doesn't exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("READ_FAILED: %v", err)), nil
		}

		out := ReadRangeOutput{
			WorkbookID: id,
			Sheet:      sheet,
			RangeA1:    outRange,
			Meta:       meta,
		}
		// Text payload is the data; structured holds metadata
		res := mcp.NewToolResultStructured(out, "range read complete")
		res.Content = []mcp.Content{mcp.NewTextContent(textOut)}
		return res, nil
	}))
	reg.Register(readRange)

	// write_range
	type WriteRangeInput struct {
		WorkbookID string     `json:"workbook_id" jsonschema_description:"Workbook handle ID"`
		Sheet      string     `json:"sheet" jsonschema_description:"Target sheet name"`
		RangeA1    string     `json:"range" jsonschema_description:"Target A1 range (e.g., B2:D10)"`
		Values     [][]string `json:"values" jsonschema_description:"2D array of values matching the range dimensions"`
	}
	type WriteRangeOutput struct {
		WorkbookID   string `json:"workbook_id"`
		Sheet        string `json:"sheet"`
		RangeA1      string `json:"range"`
		CellsUpdated int    `json:"cellsUpdated"`
		Idempotent   bool   `json:"idempotent"`
	}

	writeRange := mcp.NewTool(
		"write_range",
		mcp.WithDescription("Write a bounded block of values to a range using a transactional stream writer"),
		mcp.WithInputSchema[WriteRangeInput](),
		mcp.WithOutputSchema[WriteRangeOutput](),
	)
	s.AddTool(writeRange, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in WriteRangeInput) (*mcp.CallToolResult, error) {
		id := strings.TrimSpace(in.WorkbookID)
		sheet := strings.TrimSpace(in.Sheet)
		rng := strings.TrimSpace(in.RangeA1)
		if id == "" || sheet == "" || rng == "" {
			return mcp.NewToolResultError("VALIDATION: workbook_id, sheet, and range are required"), nil
		}
		if len(in.Values) == 0 {
			return mcp.NewToolResultError("VALIDATION: values must be a non-empty 2D array"), nil
		}

		var updated int
		err := mgr.WithWrite(id, func(f *excelize.File) error {
			// Resolve range and verify dimensions match values
			x1, y1, x2, y2, resolvedRange, perr := resolveRange(f, sheet, rng)
			if perr != nil {
				return perr
			}
			rng = resolvedRange
			rows := y2 - y1 + 1
			cols := x2 - x1 + 1
			if rows <= 0 || cols <= 0 {
				return fmt.Errorf("invalid range bounds")
			}
			if len(in.Values) != rows {
				return fmt.Errorf("values row count (%d) does not match range rows (%d)", len(in.Values), rows)
			}
			for i := range in.Values {
				if len(in.Values[i]) != cols {
					return fmt.Errorf("values column count at row %d (%d) does not match range cols (%d)", i, len(in.Values[i]), cols)
				}
			}
			cells := rows * cols
			if cells > limits.MaxCellsPerOp {
				return fmt.Errorf("payload exceeds max cells per operation: %d > %d", cells, limits.MaxCellsPerOp)
			}

			sw, err := f.NewStreamWriter(sheet)
			if err != nil {
				return err
			}
			// Write each row in ascending order
			for r := 0; r < rows; r++ {
				startCell, _ := excelize.CoordinatesToCellName(x1, y1+r)
				// Convert []string to []interface{}
				rowVals := make([]interface{}, cols)
				for c := 0; c < cols; c++ {
					rowVals[c] = in.Values[r][c]
				}
				if err := sw.SetRow(startCell, rowVals); err != nil {
					return err
				}
			}
			if err := sw.Flush(); err != nil {
				return err
			}
			// Persist changes to disk
			if err := f.Save(); err != nil {
				return err
			}
			updated = cells
			return nil
		})
		if err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "invalid range") || strings.Contains(lower, "coordinates") {
				return mcp.NewToolResultError("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			if strings.Contains(lower, "payload exceeds") {
				return mcp.NewToolResultError("PAYLOAD_TOO_LARGE: reduce range size or split into batches"), nil
			}
			if strings.Contains(lower, "doesn't exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("WRITE_FAILED: %v", err)), nil
		}

		out := WriteRangeOutput{
			WorkbookID:   id,
			Sheet:        sheet,
			RangeA1:      rng,
			CellsUpdated: updated,
			Idempotent:   false,
		}
		summary := fmt.Sprintf("updated=%d nonIdempotent=true", updated)
		return mcp.NewToolResultStructured(out, summary), nil
	}))
	reg.Register(writeRange)

	// apply_formula
	type ApplyFormulaInput struct {
		WorkbookID string `json:"workbook_id" jsonschema_description:"Workbook handle ID"`
		Sheet      string `json:"sheet" jsonschema_description:"Target sheet name"`
		RangeA1    string `json:"range" jsonschema_description:"Target A1 range to apply the formula"`
		Formula    string `json:"formula" jsonschema_description:"Formula string (e.g., =SUM(A1:B1))"`
	}
	type ApplyFormulaOutput struct {
		WorkbookID string `json:"workbook_id"`
		Sheet      string `json:"sheet"`
		RangeA1    string `json:"range"`
		CellsSet   int    `json:"cellsSet"`
		Idempotent bool   `json:"idempotent"`
	}

	applyFormula := mcp.NewTool(
		"apply_formula",
		mcp.WithDescription("Apply a formula to each cell in the given range"),
		mcp.WithInputSchema[ApplyFormulaInput](),
		mcp.WithOutputSchema[ApplyFormulaOutput](),
	)
	s.AddTool(applyFormula, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in ApplyFormulaInput) (*mcp.CallToolResult, error) {
		id := strings.TrimSpace(in.WorkbookID)
		sheet := strings.TrimSpace(in.Sheet)
		rng := strings.TrimSpace(in.RangeA1)
		formula := strings.TrimSpace(in.Formula)
		if id == "" || sheet == "" || rng == "" || formula == "" {
			return mcp.NewToolResultError("VALIDATION: workbook_id, sheet, range, and formula are required"), nil
		}

		var cellsSet int
		err := mgr.WithWrite(id, func(f *excelize.File) error {
			x1, y1, x2, y2, resolved, perr := resolveRange(f, sheet, rng)
			if perr != nil {
				return perr
			}
			rng = resolved
			rows := y2 - y1 + 1
			cols := x2 - x1 + 1
			cells := rows * cols
			if cells > limits.MaxCellsPerOp {
				return fmt.Errorf("payload exceeds max cells per operation: %d > %d", cells, limits.MaxCellsPerOp)
			}
			// Apply formula per cell; Excel interprets relative references appropriately
			for r := y1; r <= y2; r++ {
				for c := x1; c <= x2; c++ {
					cell, _ := excelize.CoordinatesToCellName(c, r)
					if err := f.SetCellFormula(sheet, cell, formula); err != nil {
						return err
					}
					cellsSet++
				}
			}
			if err := f.Save(); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "invalid range") || strings.Contains(lower, "coordinates") {
				return mcp.NewToolResultError("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			if strings.Contains(lower, "exceeds max cells") {
				return mcp.NewToolResultError("PAYLOAD_TOO_LARGE: reduce range size or split into batches"), nil
			}
			if strings.Contains(lower, "doesn't exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("APPLY_FORMULA_FAILED: %v", err)), nil
		}

		out := ApplyFormulaOutput{WorkbookID: id, Sheet: sheet, RangeA1: rng, CellsSet: cellsSet, Idempotent: false}
		summary := fmt.Sprintf("formulas_applied=%d nonIdempotent=true", cellsSet)
		return mcp.NewToolResultStructured(out, summary), nil
	}))
	reg.Register(applyFormula)

	// Annotate tool capability flags via log-friendly text until telemetry middleware is added
	_ = fmt.Sprintf("foundation tools registered: %d", 7)
}

// resolveRange parses an A1-style range or resolves a named range into coordinates.
// It returns x1,y1,x2,y2 and the resolved textual range (without sheet qualifier).
func resolveRange(f *excelize.File, sheet, input string) (int, int, int, int, string, error) {
	in := strings.TrimSpace(input)
	// If input contains '!' then it may specify a sheet-qualified range.
	if strings.Contains(in, "!") {
		parts := strings.SplitN(in, "!", 2)
		if len(parts) == 2 {
			s := strings.Trim(parts[0], "'")
			if s != "" && !strings.EqualFold(s, sheet) {
				return 0, 0, 0, 0, "", fmt.Errorf("invalid range: sheet mismatch")
			}
			in = parts[1]
		}
	}
	// Normal A1 range like A1:D50
	if strings.Contains(in, ":") {
		parts := strings.Split(in, ":")
		if len(parts) != 2 {
			return 0, 0, 0, 0, "", fmt.Errorf("invalid range: %s", input)
		}
		x1, y1, err1 := excelize.CellNameToCoordinates(parts[0])
		x2, y2, err2 := excelize.CellNameToCoordinates(parts[1])
		if err1 != nil || err2 != nil {
			return 0, 0, 0, 0, "", fmt.Errorf("invalid range coordinates")
		}
		if x2 < x1 {
			x1, x2 = x2, x1
		}
		if y2 < y1 {
			y1, y2 = y2, y1
		}
		// return normalized range text
		left, _ := excelize.CoordinatesToCellName(x1, y1)
		right, _ := excelize.CoordinatesToCellName(x2, y2)
		return x1, y1, x2, y2, left + ":" + right, nil
	}
	// Treat as a defined (named) range; find first that matches the sheet
	// Note: DefinedName.RefersTo typically looks like 'Sheet1!$A$1:$B$2'
	names := f.GetDefinedName()
	for _, dn := range names {
		if dn.Name == input {
			refers := dn.RefersTo
			// Remove leading '=' if present
			refers = strings.TrimPrefix(refers, "=")
			// If refers contains '!' and a sheet name, strip it since we already have the sheet param
			if strings.Contains(refers, "!") {
				parts := strings.SplitN(refers, "!", 2)
				if len(parts) == 2 {
					s := strings.Trim(parts[0], "'")
					if s != "" && !strings.EqualFold(s, sheet) {
						continue // not our sheet
					}
					refers = parts[1]
				}
			}
			// Now parse the range part after optional sheet qualifier
			if strings.Contains(refers, ":") {
				p := strings.Split(refers, ":")
				if len(p) != 2 {
					continue
				}
				// Remove any absolute markers '$'
				a := strings.ReplaceAll(p[0], "$", "")
				b := strings.ReplaceAll(p[1], "$", "")
				x1, y1, e1 := excelize.CellNameToCoordinates(a)
				x2, y2, e2 := excelize.CellNameToCoordinates(b)
				if e1 != nil || e2 != nil {
					continue
				}
				if x2 < x1 {
					x1, x2 = x2, x1
				}
				if y2 < y1 {
					y1, y2 = y2, y1
				}
				left, _ := excelize.CoordinatesToCellName(x1, y1)
				right, _ := excelize.CoordinatesToCellName(x2, y2)
				return x1, y1, x2, y2, left + ":" + right, nil
			}
		}
	}
	return 0, 0, 0, 0, "", fmt.Errorf("invalid or unsupported range: %s", input)
}

// errorsIsHandleNotFound reports whether the error is from the workbooks package
// indicating a missing handle. We compare by string to avoid importing internal error vars.
// Removed helper in favor of errors.Is with workbooks.ErrHandleNotFound
