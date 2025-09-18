package registry

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
	"github.com/vinodismyname/mcpxcel/pkg/pagination"
	"github.com/xuri/excelize/v2"
)

var errCursorMtMismatch = errors.New("cursor mt mismatch")

// --- Input / Output Schemas (typed for discovery) ---

// SheetInfo summarizes a sheet without loading full data.
type SheetInfo struct {
	Name        string   `json:"name" jsonschema_description:"Sheet name"`
	RowCount    int      `json:"rowCount" jsonschema_description:"Approximate row count"`
	ColumnCount int      `json:"columnCount" jsonschema_description:"Approximate column count"`
	Headers     []string `json:"headers,omitempty" jsonschema_description:"Header row when inferred"`
}

// ListStructureInput defines parameters for structure discovery.
type ListStructureInput struct {
	Path         string `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
	MetadataOnly bool   `json:"metadata_only,omitempty" jsonschema_description:"Return only metadata even for small sheets"`
}

// ListStructureOutput summarizes workbook structure.
type ListStructureOutput struct {
	Path         string      `json:"path"`
	MetadataOnly bool        `json:"metadata_only"`
	Sheets       []SheetInfo `json:"sheets"`
}

// PreviewSheetInput defines parameters for previewing a sheet.
type PreviewSheetInput struct {
	Path     string `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
	Sheet    string `json:"sheet" jsonschema_description:"Sheet name to preview"`
	Rows     int    `json:"rows,omitempty" jsonschema_description:"Max rows to preview (bounded)"`
	Encoding string `json:"encoding,omitempty" jsonschema_description:"Output encoding: json or csv"`
	Cursor   string `json:"cursor,omitempty" jsonschema_description:"Opaque pagination cursor; takes precedence over sheet/rows"`
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
	Path     string   `json:"path"`
	Sheet    string   `json:"sheet"`
	Encoding string   `json:"encoding"`
	Meta     PageMeta `json:"meta"`
}

// ReadRangeInput defines parameters for reading a cell range.
type ReadRangeInput struct {
	Path     string `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
	Sheet    string `json:"sheet" jsonschema_description:"Sheet name"`
	RangeA1  string `json:"range" jsonschema_description:"A1-style cell range (e.g., A1:D50)"`
	MaxCells int    `json:"max_cells,omitempty" jsonschema_description:"Max cells to return (bounded)"`
	Cursor   string `json:"cursor,omitempty" jsonschema_description:"Opaque pagination cursor; takes precedence over sheet/range/max_cells"`
}

// ReadRangeOutput documents range read metadata.
type ReadRangeOutput struct {
	Path    string   `json:"path"`
	Sheet   string   `json:"sheet"`
	RangeA1 string   `json:"range"`
	Meta    PageMeta `json:"meta"`
}

// SearchDataInput defines parameters for searching values/patterns.
type SearchDataInput struct {
	Path         string `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
	Sheet        string `json:"sheet" jsonschema_description:"Sheet name"`
	Query        string `json:"query" jsonschema_description:"Search value or regex pattern"`
	Regex        bool   `json:"regex,omitempty" jsonschema_description:"Interpret query as regular expression"`
	Columns      []int  `json:"columns,omitempty" jsonschema_description:"Optional 1-based column indexes to restrict search"`
	MaxResults   int    `json:"max_results,omitempty" jsonschema_description:"Max matches to return per page (bounded)"`
	SnapshotCols int    `json:"snapshot_cols,omitempty" jsonschema_description:"Max columns to include in row snapshot (bounded)"`
	Cursor       string `json:"cursor,omitempty" jsonschema_description:"Opaque pagination cursor; takes precedence over sheet/query/columns/max_results"`
}

// SearchMatch captures a single search hit with bounded row snapshot.
type SearchMatch struct {
	Cell     string   `json:"cell"`
	Row      int      `json:"row"`
	Column   int      `json:"column"`
	Value    string   `json:"value"`
	Snapshot []string `json:"snapshot,omitempty"`
}

// SearchDataOutput documents search metadata.
type SearchDataOutput struct {
	Path    string        `json:"path"`
	Sheet   string        `json:"sheet"`
	Query   string        `json:"query"`
	Regex   bool          `json:"regex"`
	Results []SearchMatch `json:"results"`
	Meta    PageMeta      `json:"meta"`
}

// RegisterFoundationTools defines core tool schemas and placeholder handlers.
// Handlers intentionally return UNIMPLEMENTED until later tasks wire logic.
func RegisterFoundationTools(s *server.MCPServer, reg *Registry, limits runtime.Limits, mgr *workbooks.Manager) {

	// list_structure
	listStructure := mcp.NewTool(
		"list_structure",
		mcp.WithDescription("Return workbook structure: sheets, dimensions, headers (no cell data)"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or allowed path to an Excel workbook")),
		mcp.WithBoolean("metadata_only", mcp.DefaultBool(false), mcp.Description("Return only metadata even for small sheets")),
		mcp.WithOutputSchema[ListStructureOutput](),
	)
	s.AddTool(listStructure, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in ListStructureInput) (*mcp.CallToolResult, error) {
		p := strings.TrimSpace(in.Path)
		if p == "" {
			return mcp.NewToolResultError("VALIDATION: path is required"), nil
		}
		id, canonical, openErr := mgr.GetOrOpenByPath(ctx, p)
		if openErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("OPEN_FAILED: %v", openErr)), nil
		}

		var output ListStructureOutput
		output.Path = canonical
		output.MetadataOnly = in.MetadataOnly

		err := mgr.WithRead(id, func(f *excelize.File, _ int64) error {
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
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or allowed path to an Excel workbook")),
		mcp.WithString("sheet", mcp.Required(), mcp.Description("Sheet name to preview")),
		mcp.WithNumber("rows", mcp.DefaultNumber(float64(limits.PreviewRowLimit)), mcp.Min(1), mcp.Max(1000), mcp.Description("Max rows to preview")),
		mcp.WithString("encoding", mcp.DefaultString("json"), mcp.Enum("json", "csv"), mcp.Description("Output encoding")),
		mcp.WithString("cursor", mcp.Description("Opaque pagination cursor; takes precedence over sheet/rows/encoding")),
		mcp.WithOutputSchema[PreviewSheetOutput](),
	)
	s.AddTool(preview, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in PreviewSheetInput) (*mcp.CallToolResult, error) {
		p := strings.TrimSpace(in.Path)
		sheet := strings.TrimSpace(in.Sheet)
		curTok := strings.TrimSpace(in.Cursor)
		if p == "" {
			return mcp.NewToolResultError("VALIDATION: path is required"), nil
		}
		id, canonical, openErr := mgr.GetOrOpenByPath(ctx, p)
		if openErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("OPEN_FAILED: %v", openErr)), nil
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

		// Cursor precedence: when provided, override sheet/rows from token
		var startOffset int
		var parsedCur *pagination.Cursor
		if curTok != "" {
			pc, derr := pagination.DecodeCursor(curTok)
			if derr != nil {
				return mcp.NewToolResultError("CURSOR_INVALID: failed to decode cursor; reopen workbook and restart pagination"), nil
			}
			if pc.Pt != canonical {
				return mcp.NewToolResultError("CURSOR_INVALID: cursor path does not match provided path"), nil
			}
			if pc.U != pagination.UnitRows {
				return mcp.NewToolResultError("CURSOR_INVALID: unit mismatch; preview_sheet expects rows"), nil
			}
			sheet = pc.S
			startOffset = pc.Off
			if pc.Ps > 0 && pc.Ps < rowsLimit {
				rowsLimit = pc.Ps
			}
			parsedCur = pc
		} else {
			if sheet == "" {
				return mcp.NewToolResultError("VALIDATION: sheet is required (or supply cursor)"), nil
			}
		}

		meta := PageMeta{}
		// Accumulate preview in selected encoding
		var textOut string
		var sheetRange string
		var fileMT int64
		err := mgr.WithRead(id, func(f *excelize.File, _ int64) error {
			// Validate cursor file mtime under read lock
			if parsedCur != nil && parsedCur.Mt > 0 {
				if fi, serr := os.Stat(canonical); serr == nil {
					fileMT = fi.ModTime().Unix()
					if parsedCur.Mt != fileMT {
						return errCursorMtMismatch
					}
				}
			}

			// Total rows from dimension when available and capture range for cursor
			if dim, derr := f.GetSheetDimension(sheet); derr == nil && dim != "" {
				parts := strings.Split(dim, ":")
				if len(parts) == 2 {
					_, y1, e1 := excelize.CellNameToCoordinates(parts[0])
					_, y2, e2 := excelize.CellNameToCoordinates(parts[1])
					if e1 == nil && e2 == nil && y2 >= y1 {
						meta.Total = y2 - y1 + 1
						sheetRange = dim
					}
				}
			}

			r, rerr := f.Rows(sheet)
			if rerr != nil {
				return rerr
			}
			defer r.Close()

			// Skip rows up to startOffset when resuming
			if startOffset > 0 {
				skipped := 0
				for skipped < startOffset && r.Next() {
					skipped++
				}
				// If we reached end before skipping all, nothing left to return
				if meta.Total > 0 && startOffset >= meta.Total {
					if enc == "json" {
						textOut = "[]"
					} else {
						textOut = ""
					}
					meta.Returned = 0
					meta.Truncated = false
					return nil
				}
			}

			if enc == "json" {
				// Build a JSON array of rows (array of arrays)
				var buf bytes.Buffer
				buf.WriteByte('[')
				count := 0
				first := true
				for r.Next() {
					if count >= rowsLimit {
						break
					}
					row, cerr := r.Columns()
					if cerr != nil {
						return cerr
					}
					if !first {
						buf.WriteByte(',')
					}
					// serialize row as JSON array
					b, merr := json.Marshal(row)
					if merr != nil {
						return merr
					}
					buf.Write(b)
					count++
					first = false
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
			meta.Truncated = meta.Total > 0 && (startOffset+meta.Returned) < meta.Total
			if meta.Truncated {
				// Build opaque next cursor with rows unit
				var fileMT int64
				if fi, serr := os.Stat(canonical); serr == nil {
					fileMT = fi.ModTime().Unix()
				}
				next := pagination.Cursor{V: 1, Pt: canonical, S: sheet, R: sheetRange, U: pagination.UnitRows, Off: pagination.NextOffset(startOffset, meta.Returned), Ps: rowsLimit, Mt: fileMT}
				token, _ := pagination.EncodeCursor(next)
				meta.NextCursor = token
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			if errors.Is(err, errCursorMtMismatch) {
				return mcp.NewToolResultError("CURSOR_INVALID: file changed since cursor was issued; restart pagination"), nil
			}
			if strings.Contains(strings.ToLower(err.Error()), "doesn't exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("PREVIEW_FAILED: %v", err)), nil
		}

		out := PreviewSheetOutput{Path: canonical, Sheet: sheet, Encoding: enc, Meta: meta}
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
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or allowed path to an Excel workbook")),
		mcp.WithString("sheet", mcp.Required(), mcp.Description("Sheet name")),
		mcp.WithString("range", mcp.Required(), mcp.Description("A1-style cell range or named range (e.g., A1:D50)")),
		mcp.WithNumber("max_cells", mcp.DefaultNumber(float64(limits.MaxCellsPerOp)), mcp.Min(1), mcp.Description("Max cells to return before truncation")),
		mcp.WithString("cursor", mcp.Description("Opaque pagination cursor; takes precedence over sheet/range/max_cells")),
		mcp.WithOutputSchema[ReadRangeOutput](),
	)
	s.AddTool(readRange, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in ReadRangeInput) (*mcp.CallToolResult, error) {
		p := strings.TrimSpace(in.Path)
		sheet := strings.TrimSpace(in.Sheet)
		rng := strings.TrimSpace(in.RangeA1)
		curTok := strings.TrimSpace(in.Cursor)
		if p == "" {
			return mcp.NewToolResultError("VALIDATION: path is required"), nil
		}
		id, canonical, openErr := mgr.GetOrOpenByPath(ctx, p)
		if openErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("OPEN_FAILED: %v", openErr)), nil
		}
		maxCells := in.MaxCells
		if maxCells <= 0 || maxCells > limits.MaxCellsPerOp {
			maxCells = limits.MaxCellsPerOp
		}
		// Cursor precedence: when provided, override sheet/range/maxCells from token
		var startOffset int
		var parsedCur *pagination.Cursor
		if curTok != "" {
			pc, derr := pagination.DecodeCursor(curTok)
			if derr != nil {
				return mcp.NewToolResultError("CURSOR_INVALID: failed to decode cursor; reopen workbook and restart pagination"), nil
			}
			if pc.Pt != canonical {
				return mcp.NewToolResultError("CURSOR_INVALID: cursor path does not match provided path"), nil
			}
			if pc.U != pagination.UnitCells {
				return mcp.NewToolResultError("CURSOR_INVALID: unit mismatch; read_range expects cells"), nil
			}
			// Override inputs using cursor values
			sheet = pc.S
			rng = pc.R
			startOffset = pc.Off
			if pc.Ps > 0 && pc.Ps < maxCells {
				maxCells = pc.Ps
			}
			parsedCur = pc
		} else {
			if sheet == "" || rng == "" {
				return mcp.NewToolResultError("VALIDATION: sheet and range are required (or supply cursor)"), nil
			}
		}

		// We will build a JSON array-of-arrays payload in text form to keep memory bounded
		var textOut string
		var meta PageMeta
		var outRange = rng

		var fileMT int64
		err := mgr.WithRead(id, func(f *excelize.File, _ int64) error {
			// Validate mtime snapshot if resuming from a cursor
			if parsedCur != nil && parsedCur.Mt > 0 {
				if fi, serr := os.Stat(canonical); serr == nil {
					fileMT = fi.ModTime().Unix()
					if parsedCur.Mt != fileMT {
						return errCursorMtMismatch
					}
				}
			}
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

			// Compute resume position from startOffset (cells) if provided
			cols := x2 - x1 + 1
			startRow := y1
			startCol := x1
			if startOffset > 0 {
				startRow = y1 + (startOffset / cols)
				startCol = x1 + (startOffset % cols)
				if startCol > x2 {
					startCol = x1
					startRow++
				}
				if startRow > y2 {
					// Nothing left to return
					textOut = "[]"
					meta.Returned = 0
					meta.Truncated = false
					return nil
				}
			}

			// Iterate row-major from (startCol,startRow), but stop when we reach maxCells
			// Build JSON array-of-arrays
			var buf bytes.Buffer
			buf.WriteByte('[')
			writtenCells := 0
			emittedRows := 0
			stop := false
			for row := startRow; row <= y2 && !stop; row++ {
				// For each row, emit an array of columns
				if emittedRows > 0 {
					buf.WriteByte(',')
				}
				buf.WriteByte('[')
				colsWritten := 0
				cstart := x1
				if row == startRow {
					cstart = startCol
				}
				for col := cstart; col <= x2; col++ {
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
				emittedRows++
			}
			buf.WriteByte(']')
			textOut = buf.String()
			meta.Returned = writtenCells
			meta.Truncated = (startOffset + writtenCells) < total
			if meta.Truncated {
				// Build opaque next cursor
				next := pagination.Cursor{V: 1, Pt: canonical, S: sheet, R: outRange, U: pagination.UnitCells, Off: pagination.NextOffset(startOffset, writtenCells), Ps: maxCells, Mt: fileMT}
				token, _ := pagination.EncodeCursor(next)
				meta.NextCursor = token
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			if errors.Is(err, errCursorMtMismatch) {
				return mcp.NewToolResultError("CURSOR_INVALID: file changed since cursor was issued; restart pagination"), nil
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

		out := ReadRangeOutput{Path: canonical, Sheet: sheet, RangeA1: outRange, Meta: meta}
		// Text payload is the data; structured holds metadata
		res := mcp.NewToolResultStructured(out, "range read complete")
		res.Content = []mcp.Content{mcp.NewTextContent(textOut)}
		return res, nil
	}))
	reg.Register(readRange)

	// search_data
	searchTool := mcp.NewTool(
		"search_data",
		mcp.WithDescription("Search for values or regex patterns with optional column filters and bounded row snapshots"),
		mcp.WithInputSchema[SearchDataInput](),
		mcp.WithOutputSchema[SearchDataOutput](),
	)
	s.AddTool(searchTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in SearchDataInput) (*mcp.CallToolResult, error) {
		p := strings.TrimSpace(in.Path)
		sheet := strings.TrimSpace(in.Sheet)
		query := strings.TrimSpace(in.Query)
		curTok := strings.TrimSpace(in.Cursor)
		regex := in.Regex
		if p == "" {
			return mcp.NewToolResultError("VALIDATION: path is required"), nil
		}
		id, canonical, openErr := mgr.GetOrOpenByPath(ctx, p)
		if openErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("OPEN_FAILED: %v", openErr)), nil
		}
		maxResults := in.MaxResults
		if maxResults <= 0 || maxResults > 1000 {
			maxResults = 50
		}
		snapshotCols := in.SnapshotCols
		if snapshotCols <= 0 || snapshotCols > 256 {
			snapshotCols = 16
		}
		// We'll build the column filter after resolving cursor/inputs
		var colFilter map[int]struct{}

		// Cursor precedence: when provided, override sheet/maxResults from token; validate hash/version
		var startOffset int
		var parsedCur *pagination.Cursor
		if curTok != "" {
			pc, derr := pagination.DecodeCursor(curTok)
			if derr != nil {
				return mcp.NewToolResultError("CURSOR_INVALID: failed to decode cursor; reopen workbook and restart pagination"), nil
			}
			if pc.Pt != canonical {
				return mcp.NewToolResultError("CURSOR_INVALID: cursor path does not match provided path"), nil
			}
			if pc.U != pagination.UnitRows {
				return mcp.NewToolResultError("CURSOR_INVALID: unit mismatch; search_data expects rows"), nil
			}
			// When query/filters are provided alongside cursor, ensure they bind to the same parameters
			if query != "" || len(in.Columns) > 0 || in.Regex {
				qh := computeQueryHash(query, in.Regex, in.Columns)
				if pc.Qh != "" && pc.Qh != qh {
					return mcp.NewToolResultError("CURSOR_INVALID: cursor parameters do not match current query/filters"), nil
				}
			}
			sheet = pc.S
			// If query/regex/columns are not provided on resume, recover them from cursor when available
			if query == "" && pc.Q != "" {
				query = pc.Q
			}
			if !in.Regex && pc.Rg {
				regex = true
			}
			if len(in.Columns) == 0 && len(pc.Cl) > 0 {
				in.Columns = pc.Cl
			}
			startOffset = pc.Off
			if pc.Ps > 0 && pc.Ps < maxResults {
				maxResults = pc.Ps
			}
			parsedCur = pc
		} else {
			if sheet == "" || query == "" {
				return mcp.NewToolResultError("VALIDATION: sheet and query are required (or supply cursor)"), nil
			}
		}

		// Build column filter set from final in.Columns (possibly recovered from cursor)
		if len(in.Columns) > 0 {
			colFilter = make(map[int]struct{}, len(in.Columns))
			for _, c := range in.Columns {
				if c >= 1 {
					colFilter[c] = struct{}{}
				}
			}
		}

		// Perform search under workbook read lock; validate wbv for resumed cursors
		var output SearchDataOutput
		output.Path = canonical
		output.Sheet = sheet
		output.Query = query
		output.Regex = regex

		var fileMT int64
		err := mgr.WithRead(id, func(f *excelize.File, _ int64) error {
			if parsedCur != nil && parsedCur.Mt > 0 {
				if fi, serr := os.Stat(canonical); serr == nil {
					fileMT = fi.ModTime().Unix()
					if parsedCur.Mt != fileMT {
						return errCursorMtMismatch
					}
				}
			}

			// Resolve used range for sheet and derive snapshot anchoring and bounds
			maxCols := snapshotCols
			sheetRange := ""
			xLeft, xRight := 1, snapshotCols
			if dim, derr := f.GetSheetDimension(sheet); derr == nil && dim != "" {
				parts := strings.Split(dim, ":")
				if len(parts) == 2 {
					x1, _, e1 := excelize.CellNameToCoordinates(parts[0])
					x2, _, e2 := excelize.CellNameToCoordinates(parts[1])
					if e1 == nil && e2 == nil && x2 >= x1 {
						sheetRange = dim
						cols := x2 - x1 + 1
						if cols < maxCols {
							maxCols = cols
						}
						xLeft, xRight = x1, x1+maxCols-1
						if xRight > x2 {
							xRight = x2
						}
					}
				}
			}

			// Execute search
			var matches []string
			var sErr error
			if regex {
				matches, sErr = f.SearchSheet(sheet, query, true)
			} else {
				matches, sErr = f.SearchSheet(sheet, query)
			}
			if sErr != nil {
				return sErr
			}

			// Filter by columns if provided
			filtered := make([]string, 0, len(matches))
			if colFilter != nil {
				for _, cell := range matches {
					x, _, e := excelize.CellNameToCoordinates(cell)
					if e != nil {
						continue
					}
					if _, ok := colFilter[x]; ok {
						filtered = append(filtered, cell)
					}
				}
			} else {
				filtered = matches
			}

			// Build results page
			total := len(filtered)
			output.Meta.Total = total
			if startOffset > total {
				startOffset = total
			}
			end := startOffset + maxResults
			if end > total {
				end = total
			}
			page := filtered[startOffset:end]

			results := make([]SearchMatch, 0, len(page))
			for _, cell := range page {
				x, y, e := excelize.CellNameToCoordinates(cell)
				if e != nil {
					continue
				}
				val, _ := f.GetCellValue(sheet, cell)
				// Snapshot anchored to left bound of used range
				rowVals := make([]string, 0, maxCols)
				for c := xLeft; c <= xRight; c++ {
					cn, _ := excelize.CoordinatesToCellName(c, y)
					v, _ := f.GetCellValue(sheet, cn)
					rowVals = append(rowVals, v)
				}
				results = append(results, SearchMatch{Cell: cell, Row: y, Column: x, Value: val, Snapshot: rowVals})
			}
			output.Results = results
			output.Meta.Returned = len(results)
			output.Meta.Truncated = (startOffset + len(results)) < total
			if output.Meta.Truncated {
				// Preserve qh when resuming via cursor; otherwise compute from inputs
				qh := ""
				if parsedCur != nil && parsedCur.Qh != "" {
					qh = parsedCur.Qh
				} else {
					qh = computeQueryHash(query, regex, in.Columns)
				}
				next := pagination.Cursor{V: 1, Pt: canonical, S: sheet, R: sheetRange, U: pagination.UnitRows, Off: pagination.NextOffset(startOffset, len(results)), Ps: maxResults, Mt: fileMT, Qh: qh, Q: query, Rg: regex, Cl: in.Columns}
				token, encErr := pagination.EncodeCursor(next)
				if encErr != nil {
					return fmt.Errorf("CURSOR_BUILD_FAILED: %v", encErr)
				}
				output.Meta.NextCursor = token
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			if errors.Is(err, errCursorMtMismatch) {
				return mcp.NewToolResultError("CURSOR_INVALID: file changed since cursor was issued; restart pagination"), nil
			}
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			// Cursor build failure mapping
			if strings.HasPrefix(err.Error(), "CURSOR_BUILD_FAILED:") {
				return mcp.NewToolResultError("CURSOR_BUILD_FAILED: failed to encode next page cursor; retry or narrow scope"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("SEARCH_FAILED: %v", err)), nil
		}

		// Human-friendly summary
		summary := fmt.Sprintf("matches=%d returned=%d truncated=%v", output.Meta.Total, output.Meta.Returned, output.Meta.Truncated)
		if output.Meta.Truncated && output.Meta.NextCursor != "" {
			// Surface nextCursor in summary for clients that ignore structured meta
			summary = summary + " nextCursor=" + output.Meta.NextCursor
		}
		res := mcp.NewToolResultStructured(output, summary)
		// Attach a human-readable summary line followed by JSON results text
		if b, jerr := json.Marshal(output.Results); jerr == nil {
			var sb strings.Builder
			sb.WriteString(summary)
			sb.WriteByte('\n')
			sb.Write(b)
			res.Content = []mcp.Content{mcp.NewTextContent(sb.String())}
		} else {
			res.Content = []mcp.Content{mcp.NewTextContent(summary)}
		}
		return res, nil
	}))
	reg.Register(searchTool)

	// filter_data
	type FilterDataInput struct {
		Path         string `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
		Sheet        string `json:"sheet" jsonschema_description:"Sheet name"`
		Predicate    string `json:"predicate" jsonschema_description:"Predicate expression using $N column refs and operators (=,!=,>,<,>=,<=, contains) with AND/OR/NOT and parentheses"`
		Columns      []int  `json:"columns,omitempty" jsonschema_description:"Optional 1-based column indexes to include in cursor provenance"`
		MaxRows      int    `json:"max_rows,omitempty" jsonschema_description:"Max rows to return per page (bounded)"`
		SnapshotCols int    `json:"snapshot_cols,omitempty" jsonschema_description:"Max columns to include in row snapshot (bounded)"`
		Cursor       string `json:"cursor,omitempty" jsonschema_description:"Opaque pagination cursor; takes precedence over sheet/predicate/max_rows"`
	}

	type FilteredRow struct {
		Row      int      `json:"row"`
		Snapshot []string `json:"snapshot"`
	}

	type FilterDataOutput struct {
		Path      string        `json:"path"`
		Sheet     string        `json:"sheet"`
		Predicate string        `json:"predicate"`
		Results   []FilteredRow `json:"results"`
		Meta      PageMeta      `json:"meta"`
	}

	filterTool := mcp.NewTool(
		"filter_data",
		mcp.WithDescription("Filter rows by predicate ($N refs, comparison and boolean operators) with pagination"),
		mcp.WithInputSchema[FilterDataInput](),
		mcp.WithOutputSchema[FilterDataOutput](),
	)

	s.AddTool(filterTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in FilterDataInput) (*mcp.CallToolResult, error) {
		p := strings.TrimSpace(in.Path)
		sheet := strings.TrimSpace(in.Sheet)
		pred := strings.TrimSpace(in.Predicate)
		curTok := strings.TrimSpace(in.Cursor)
		if p == "" {
			return mcp.NewToolResultError("VALIDATION: path is required"), nil
		}
		id, canonical, openErr := mgr.GetOrOpenByPath(ctx, p)
		if openErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("OPEN_FAILED: %v", openErr)), nil
		}
		maxRows := in.MaxRows
		if maxRows <= 0 || maxRows > 1000 {
			maxRows = 200
		}
		snapshotCols := in.SnapshotCols
		if snapshotCols <= 0 || snapshotCols > 256 {
			snapshotCols = 16
		}

		// Cursor precedence and binding validation
		var startOffset int
		var parsedCur *pagination.Cursor
		if curTok != "" {
			pc, derr := pagination.DecodeCursor(curTok)
			if derr != nil {
				return mcp.NewToolResultError("CURSOR_INVALID: failed to decode cursor; reopen workbook and restart pagination"), nil
			}
			if pc.Pt != canonical {
				return mcp.NewToolResultError("CURSOR_INVALID: cursor path does not match provided path"), nil
			}
			if pc.U != pagination.UnitRows {
				return mcp.NewToolResultError("CURSOR_INVALID: unit mismatch; filter_data expects rows"), nil
			}
			// When predicate/columns are provided alongside cursor, ensure they bind to same parameters
			if pred != "" || len(in.Columns) > 0 {
				ph := computePredicateHash(pred, in.Columns)
				if pc.Ph != "" && pc.Ph != ph {
					return mcp.NewToolResultError("CURSOR_INVALID: cursor parameters do not match current predicate/columns"), nil
				}
			}
			sheet = pc.S
			if pred == "" && pc.P != "" {
				pred = pc.P
			}
			if len(in.Columns) == 0 && len(pc.Cl) > 0 {
				in.Columns = pc.Cl
			}
			startOffset = pc.Off
			if pc.Ps > 0 && pc.Ps < maxRows {
				maxRows = pc.Ps
			}
			parsedCur = pc
		} else {
			if sheet == "" || pred == "" {
				return mcp.NewToolResultError("VALIDATION: sheet and predicate are required (or supply cursor)"), nil
			}
		}

		// Compile predicate to evaluator
		eval, perr := compilePredicate(pred)
		if perr != nil {
			return mcp.NewToolResultError("VALIDATION: invalid predicate; examples: $1 = \"foo\", $3 > 100, $2 contains \"bar\", ($1 = \"x\" AND $4 >= 0.5) OR NOT $5 = \"y\""), nil
		}

		var output FilterDataOutput
		output.Path = canonical
		output.Sheet = sheet
		output.Predicate = pred

		var fileMT int64
		err := mgr.WithRead(id, func(f *excelize.File, _ int64) error {
			if parsedCur != nil && parsedCur.Mt > 0 {
				if fi, serr := os.Stat(canonical); serr == nil {
					fileMT = fi.ModTime().Unix()
					if parsedCur.Mt != fileMT {
						return errCursorMtMismatch
					}
				}
			}
			// Resolve used range and snapshot bounds
			sheetRange := ""
			xLeft, xRight := 1, snapshotCols
			yTop, yBot := 1, 0
			if dim, derr := f.GetSheetDimension(sheet); derr == nil && dim != "" {
				parts := strings.Split(dim, ":")
				if len(parts) == 2 {
					x1, y1, e1 := excelize.CellNameToCoordinates(parts[0])
					x2, y2, e2 := excelize.CellNameToCoordinates(parts[1])
					if e1 == nil && e2 == nil && x2 >= x1 && y2 >= y1 {
						sheetRange = dim
						xLeft = x1
						xRight = x1 + snapshotCols - 1
						if xRight > x2 {
							xRight = x2
						}
						yTop, yBot = y1, y2
					}
				}
			}

			rowsIter, rerr := f.Rows(sheet)
			if rerr != nil {
				return rerr
			}
			defer rowsIter.Close()

			total := 0
			returned := 0
			rowIdx := 0
			results := make([]FilteredRow, 0, maxRows)

			for rowsIter.Next() {
				rowIdx++
				if rowIdx < yTop {
					continue
				}
				if yBot > 0 && rowIdx > yBot {
					break
				}
				rowVals, cerr := rowsIter.Columns()
				if cerr != nil {
					return cerr
				}
				ok := eval(rowVals)
				if ok {
					total++
					if total > startOffset && returned < maxRows {
						// Build snapshot across [xLeft,xRight]
						snap := make([]string, 0, xRight-xLeft+1)
						for c := xLeft; c <= xRight; c++ {
							absCol := c - 1
							if absCol >= 0 && absCol < len(rowVals) {
								snap = append(snap, rowVals[absCol])
							} else {
								snap = append(snap, "")
							}
						}
						results = append(results, FilteredRow{Row: rowIdx, Snapshot: snap})
						returned++
					}
				}
			}

			output.Results = results
			output.Meta.Total = total
			output.Meta.Returned = returned
			output.Meta.Truncated = (startOffset + returned) < total
			if output.Meta.Truncated {
				ph := ""
				if parsedCur != nil && parsedCur.Ph != "" {
					ph = parsedCur.Ph
				} else {
					ph = computePredicateHash(pred, in.Columns)
				}
				next := pagination.Cursor{V: 1, Pt: canonical, S: sheet, R: sheetRange, U: pagination.UnitRows, Off: pagination.NextOffset(startOffset, returned), Ps: maxRows, Mt: fileMT, Ph: ph, P: pred, Cl: in.Columns}
				token, encErr := pagination.EncodeCursor(next)
				if encErr != nil {
					return fmt.Errorf("CURSOR_BUILD_FAILED: %v", encErr)
				}
				output.Meta.NextCursor = token
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, workbooks.ErrHandleNotFound) {
				return mcp.NewToolResultError("INVALID_HANDLE: workbook handle not found or expired"), nil
			}
			if errors.Is(err, errCursorMtMismatch) {
				return mcp.NewToolResultError("CURSOR_INVALID: file changed since cursor was issued; restart pagination"), nil
			}
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			if strings.HasPrefix(err.Error(), "CURSOR_BUILD_FAILED:") {
				return mcp.NewToolResultError("CURSOR_BUILD_FAILED: failed to encode next page cursor; retry or narrow scope"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("FILTER_FAILED: %v", err)), nil
		}

		// Attach human-readable summary and JSON results (like search_data)
		summary := fmt.Sprintf("matches=%d returned=%d truncated=%v", output.Meta.Total, output.Meta.Returned, output.Meta.Truncated)
		if output.Meta.Truncated && output.Meta.NextCursor != "" {
			summary = summary + " nextCursor=" + output.Meta.NextCursor
		}
		res := mcp.NewToolResultStructured(output, summary)
		if b, jerr := json.Marshal(output.Results); jerr == nil {
			var sb strings.Builder
			sb.WriteString(summary)
			sb.WriteByte('\n')
			sb.Write(b)
			res.Content = []mcp.Content{mcp.NewTextContent(sb.String())}
		} else {
			res.Content = []mcp.Content{mcp.NewTextContent(summary)}
		}
		return res, nil
	}))
	reg.Register(filterTool)

	// write_range
	type WriteRangeInput struct {
		Path    string     `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
		Sheet   string     `json:"sheet" jsonschema_description:"Target sheet name"`
		RangeA1 string     `json:"range" jsonschema_description:"Target A1 range (e.g., B2:D10)"`
		Values  [][]string `json:"values" jsonschema_description:"2D array of values matching the range dimensions"`
	}
	type WriteRangeOutput struct {
		Path         string `json:"path"`
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
		p := strings.TrimSpace(in.Path)
		sheet := strings.TrimSpace(in.Sheet)
		rng := strings.TrimSpace(in.RangeA1)
		if p == "" || sheet == "" || rng == "" {
			return mcp.NewToolResultError("VALIDATION: path, sheet, and range are required"), nil
		}
		id, canonical, openErr := mgr.GetOrOpenByPath(ctx, p)
		if openErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("OPEN_FAILED: %v", openErr)), nil
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

		out := WriteRangeOutput{Path: canonical, Sheet: sheet, RangeA1: rng, CellsUpdated: updated, Idempotent: false}
		summary := fmt.Sprintf("updated=%d nonIdempotent=true", updated)
		return mcp.NewToolResultStructured(out, summary), nil
	}))
	reg.Register(writeRange)

	// apply_formula
	type ApplyFormulaInput struct {
		Path    string `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
		Sheet   string `json:"sheet" jsonschema_description:"Target sheet name"`
		RangeA1 string `json:"range" jsonschema_description:"Target A1 range to apply the formula"`
		Formula string `json:"formula" jsonschema_description:"Formula string (e.g., =SUM(A1:B1))"`
	}
	type ApplyFormulaOutput struct {
		Path       string `json:"path"`
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
		p := strings.TrimSpace(in.Path)
		sheet := strings.TrimSpace(in.Sheet)
		rng := strings.TrimSpace(in.RangeA1)
		formula := strings.TrimSpace(in.Formula)
		if p == "" || sheet == "" || rng == "" || formula == "" {
			return mcp.NewToolResultError("VALIDATION: path, sheet, range, and formula are required"), nil
		}
		id, canonical, openErr := mgr.GetOrOpenByPath(ctx, p)
		if openErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("OPEN_FAILED: %v", openErr)), nil
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

		out := ApplyFormulaOutput{Path: canonical, Sheet: sheet, RangeA1: rng, CellsSet: cellsSet, Idempotent: false}
		summary := fmt.Sprintf("formulas_applied=%d nonIdempotent=true", cellsSet)
		return mcp.NewToolResultStructured(out, summary), nil
	}))
	reg.Register(applyFormula)

	// Annotate tool capability flags via log-friendly text until telemetry middleware is added
	_ = fmt.Sprintf("foundation tools registered: %d", 8)

	// compute_statistics
	type ComputeStatisticsInput struct {
		Path          string `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
		Sheet         string `json:"sheet" jsonschema_description:"Sheet name"`
		RangeA1       string `json:"range" jsonschema_description:"A1-style range or defined name to analyze"`
		ColumnIndices []int  `json:"columns,omitempty" jsonschema_description:"1-based column indexes within the range; omitted means all"`
		GroupByIndex  int    `json:"group_by_index,omitempty" jsonschema_description:"Optional 1-based column index within the range to group by"`
		MaxCells      int    `json:"max_cells,omitempty" jsonschema_description:"Max cells to process (bounded)"`
	}

	type ColumnStats struct {
		Count         int     `json:"count"`
		DistinctCount int     `json:"distinct"`
		Sum           float64 `json:"sum"`
		Average       float64 `json:"average"`
		Min           float64 `json:"min"`
		Max           float64 `json:"max"`
	}

	type ComputeStatisticsOutput struct {
		Path    string `json:"path"`
		Sheet   string `json:"sheet"`
		RangeA1 string `json:"range"`
		Meta    struct {
			ProcessedCells int  `json:"processedCells"`
			MaxCells       int  `json:"maxCells"`
			Truncated      bool `json:"truncated"`
		} `json:"meta"`
		// One of the following will be populated
		Columns []ColumnStats            `json:"columns,omitempty"`
		Groups  map[string][]ColumnStats `json:"groups,omitempty"`
	}

	computeStats := mcp.NewTool(
		"compute_statistics",
		mcp.WithDescription("Compute per-column summary statistics with optional group-by using streaming analysis"),
		mcp.WithInputSchema[ComputeStatisticsInput](),
		mcp.WithOutputSchema[ComputeStatisticsOutput](),
	)

	// Helper to parse string to float64; returns value and ok flag
	parseNumber := func(s string) (float64, bool) {
		if s == "" {
			return 0, false
		}
		// Try plain float
		if v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64); err == nil {
			return v, true
		}
		return 0, false
	}

	// Reducer update for a single observation
	updateStats := func(st *ColumnStats, val string, distinct map[string]struct{}) {
		if val == "" {
			return
		}
		// Distinct tracking by raw string
		if _, ok := distinct[val]; !ok {
			distinct[val] = struct{}{}
			st.DistinctCount = len(distinct)
		}
		if f, ok := parseNumber(val); ok {
			st.Count++
			st.Sum += f
			if st.Count == 1 {
				st.Min = f
				st.Max = f
			} else {
				st.Min = math.Min(st.Min, f)
				st.Max = math.Max(st.Max, f)
			}
			st.Average = st.Sum / float64(st.Count)
		}
	}

	s.AddTool(computeStats, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in ComputeStatisticsInput) (*mcp.CallToolResult, error) {
		p := strings.TrimSpace(in.Path)
		sheet := strings.TrimSpace(in.Sheet)
		rng := strings.TrimSpace(in.RangeA1)
		if p == "" || sheet == "" || rng == "" {
			return mcp.NewToolResultError("VALIDATION: path, sheet, and range are required"), nil
		}
		id, canonical, openErr := mgr.GetOrOpenByPath(ctx, p)
		if openErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("OPEN_FAILED: %v", openErr)), nil
		}
		maxCells := in.MaxCells
		if maxCells <= 0 || maxCells > limits.MaxCellsPerOp {
			maxCells = limits.MaxCellsPerOp
		}

		var out ComputeStatisticsOutput
		out.Path = canonical
		out.Sheet = sheet
		out.RangeA1 = rng
		out.Meta.MaxCells = maxCells

		err := mgr.WithRead(id, func(f *excelize.File, _ int64) error {
			// Resolve range coordinates and normalized textual range
			x1, y1, x2, y2, normalizedRange, perr := resolveRange(f, sheet, rng)
			if perr != nil {
				return perr
			}
			rng = normalizedRange
			out.RangeA1 = rng

			// Determine which columns to include (1-based within range)
			colCount := x2 - x1 + 1
			indices := in.ColumnIndices
			if len(indices) == 0 {
				indices = make([]int, colCount)
				for i := 0; i < colCount; i++ {
					indices[i] = i + 1 // 1-based within range
				}
			} else {
				// Validate provided indices
				for _, idx := range indices {
					if idx <= 0 || idx > colCount {
						return fmt.Errorf("invalid column index %d; range has %d columns", idx, colCount)
					}
				}
			}

			// Group-by bounds
			groupBy := in.GroupByIndex
			if groupBy < 0 || groupBy > colCount {
				return fmt.Errorf("invalid group_by_index %d; range has %d columns", groupBy, colCount)
			}

			rowsIter, rerr := f.Rows(sheet)
			if rerr != nil {
				return rerr
			}
			defer rowsIter.Close()

			processed := 0
			rowIdx := 0

			// Initialize reducers
			if groupBy == 0 {
				out.Columns = make([]ColumnStats, len(indices))
			}
			groupStats := map[string][]ColumnStats{}
			groupDistinctSets := map[string][]map[string]struct{}{}

			// Build distinct sets per column for non-grouped mode
			distinctSets := make([]map[string]struct{}, len(indices))
			for i := range distinctSets {
				distinctSets[i] = make(map[string]struct{})
			}

			maxGroups := maxCells / (len(indices) + 1)
			if maxGroups <= 0 {
				maxGroups = 1
			}

			for rowsIter.Next() {
				rowIdx++
				// Skip until first row of range
				if rowIdx < y1 {
					continue
				}
				if rowIdx > y2 {
					break
				}

				rowVals, cerr := rowsIter.Columns()
				if cerr != nil {
					return cerr
				}

				// Determine group key when requested
				var gkey string
				if groupBy > 0 {
					colIdx := x1 + (groupBy - 1) - 1 // zero-based absolute index
					if colIdx >= 0 && colIdx < len(rowVals) {
						gkey = rowVals[colIdx]
					}
					if gkey == "" {
						gkey = "(empty)"
					}
					// Initialize group reducers lazily
					if _, ok := groupStats[gkey]; !ok {
						if len(groupStats) >= maxGroups {
							return fmt.Errorf("too many groups: %d (max %d); narrow group_by or range", len(groupStats)+1, maxGroups)
						}
						groupStats[gkey] = make([]ColumnStats, len(indices))
						set := make([]map[string]struct{}, len(indices))
						for i := range set {
							set[i] = make(map[string]struct{})
						}
						groupDistinctSets[gkey] = set
					}
				}

				// Update stats for selected columns
				for i, idxWithinRange := range indices {
					absCol := x1 + (idxWithinRange - 1) - 1 // zero-based absolute column index
					var cell string
					if absCol >= 0 && absCol < len(rowVals) {
						cell = rowVals[absCol]
					}
					if groupBy > 0 {
						arr := groupStats[gkey]
						sets := groupDistinctSets[gkey]
						updateStats(&arr[i], cell, sets[i])
						groupStats[gkey] = arr
					} else {
						updateStats(&out.Columns[i], cell, distinctSets[i])
					}
				}

				processed += len(indices)
				if processed >= maxCells {
					out.Meta.Truncated = (rowIdx < y2)
					break
				}
			}

			out.Meta.ProcessedCells = processed
			if groupBy > 0 {
				out.Groups = groupStats
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
			if strings.Contains(lower, "too many groups") {
				return mcp.NewToolResultError("LIMIT_EXCEEDED: too many groups for available budget; narrow group_by or reduce range"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("STATISTICS_FAILED: %v", err)), nil
		}

		// Build concise summary string
		var summary string
		if len(out.Groups) > 0 {
			summary = fmt.Sprintf("grouped stats: groups=%d cols=%d processed=%d truncated=%v", len(out.Groups), func() int {
				if len(out.Groups) > 0 {
					for _, v := range out.Groups {
						return len(v)
					}
				}
				return 0
			}(), out.Meta.ProcessedCells, out.Meta.Truncated)
		} else {
			summary = fmt.Sprintf("stats: cols=%d processed=%d truncated=%v", len(out.Columns), out.Meta.ProcessedCells, out.Meta.Truncated)
		}
		return mcp.NewToolResultStructured(out, summary), nil
	}))
	reg.Register(computeStats)
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

// legacy cursor emission has been removed. Only opaque cursors are supported.

// computeQueryHash returns a short, deterministic hex hash that binds search parameters
// (query string, regex flag, and restricted columns). This is embedded in pagination
// cursors (qh) so resuming pages can be validated against the same parameters.
func computeQueryHash(query string, regex bool, columns []int) string {
	// Normalize inputs
	q := strings.TrimSpace(query)
	// Copy and sort columns for stable representation
	cols := make([]int, 0, len(columns))
	for _, c := range columns {
		if c >= 1 {
			cols = append(cols, c)
		}
	}
	sort.Ints(cols)
	var b strings.Builder
	b.WriteString(q)
	b.WriteString("|")
	if regex {
		b.WriteString("1")
	} else {
		b.WriteString("0")
	}
	b.WriteString("|")
	for i, c := range cols {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(c))
	}
	sum := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// computePredicateHash returns a deterministic hash binding predicate expression and column scope.
func computePredicateHash(predicate string, columns []int) string {
	// Normalize predicate by trimming redundant whitespace sequences to a single space
	norm := strings.TrimSpace(predicate)
	space := regexp.MustCompile(`\s+`)
	norm = space.ReplaceAllString(norm, " ")
	// Copy and sort columns for stable representation
	cols := make([]int, 0, len(columns))
	for _, c := range columns {
		if c >= 1 {
			cols = append(cols, c)
		}
	}
	sort.Ints(cols)
	var b strings.Builder
	b.WriteString(norm)
	b.WriteString("|")
	for i, c := range cols {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(c))
	}
	sum := sha1.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// Predicate parsing and evaluation
// Grammar (subset):
//   expr := orExpr
//   orExpr := andExpr { OR andExpr }
//   andExpr := unaryExpr { AND unaryExpr }
//   unaryExpr := [NOT] primary
//   primary := comparison | '(' expr ')'
//   comparison := value ( = | != | > | < | >= | <= | CONTAINS ) value
//   value := $N | number | string
// Columns referenced with $N are 1-based absolute column indices.

type tokenKind int

const (
	tkEOF tokenKind = iota
	tkLParen
	tkRParen
	tkAnd
	tkOr
	tkNot
	tkOp // comparison op or 'contains'
	tkCol
	tkString
	tkNumber
)

type token struct {
	kind tokenKind
	val  string
}

// compilePredicate compiles a predicate string into an evaluator function.
func compilePredicate(src string) (func([]string) bool, error) {
	toks, err := tokenizePredicate(src)
	if err != nil {
		return nil, err
	}
	rpn, err := toRPN(toks)
	if err != nil {
		return nil, err
	}
	return func(row []string) bool {
		ok, _ := evalRPN(rpn, row)
		return ok
	}, nil
}

func tokenizePredicate(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		ch := s[i]
		// whitespace
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			i++
			continue
		}
		// parentheses
		if ch == '(' {
			toks = append(toks, token{kind: tkLParen, val: "("})
			i++
			continue
		}
		if ch == ')' {
			toks = append(toks, token{kind: tkRParen, val: ")"})
			i++
			continue
		}
		// operators: >= <= != == = > <
		if ch == '>' || ch == '<' || ch == '!' || ch == '=' {
			if i+1 < len(s) {
				pair := s[i : i+2]
				switch pair {
				case ">=", "<=", "!=", "==":
					toks = append(toks, token{kind: tkOp, val: pair})
					i += 2
					continue
				}
			}
			// single-char ops
			toks = append(toks, token{kind: tkOp, val: string(ch)})
			i++
			continue
		}
		// column ref: $N
		if ch == '$' {
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j == i+1 {
				return nil, fmt.Errorf("invalid column reference at %d", i)
			}
			toks = append(toks, token{kind: tkCol, val: s[i:j]})
			i = j
			continue
		}
		// string literal '...' or "..."
		if ch == '\'' || ch == '"' {
			quote := ch
			j := i + 1
			var b strings.Builder
			for j < len(s) {
				if s[j] == '\\' && j+1 < len(s) {
					b.WriteByte(s[j+1])
					j += 2
					continue
				}
				if s[j] == quote {
					break
				}
				b.WriteByte(s[j])
				j++
			}
			if j >= len(s) || s[j] != quote {
				return nil, fmt.Errorf("unterminated string literal")
			}
			toks = append(toks, token{kind: tkString, val: b.String()})
			i = j + 1
			continue
		}
		// identifier: AND OR NOT CONTAINS (case-insensitive)
		if isAlpha(ch) {
			j := i + 1
			for j < len(s) && (isAlphaNum(s[j]) || s[j] == '_') {
				j++
			}
			word := strings.ToUpper(s[i:j])
			switch word {
			case "AND":
				toks = append(toks, token{kind: tkAnd, val: word})
			case "OR":
				toks = append(toks, token{kind: tkOr, val: word})
			case "NOT":
				toks = append(toks, token{kind: tkNot, val: word})
			case "CONTAINS":
				toks = append(toks, token{kind: tkOp, val: "contains"})
			default:
				// number? fallthrough
				// treat as bareword string value
				toks = append(toks, token{kind: tkString, val: s[i:j]})
			}
			i = j
			continue
		}
		// number literal (digits, optional dot, optional commas)
		if (ch >= '0' && ch <= '9') || ch == '-' || ch == '+' {
			j := i + 1
			for j < len(s) {
				c := s[j]
				if (c >= '0' && c <= '9') || c == '.' || c == ',' {
					j++
					continue
				}
				break
			}
			toks = append(toks, token{kind: tkNumber, val: s[i:j]})
			i = j
			continue
		}
		return nil, fmt.Errorf("unexpected character %q at %d", ch, i)
	}
	return toks, nil
}

func isAlpha(b byte) bool    { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isAlphaNum(b byte) bool { return isAlpha(b) || (b >= '0' && b <= '9') }

func precedence(t token) int {
	switch t.kind {
	case tkNot:
		return 3
	case tkOp:
		return 2
	case tkAnd:
		return 1
	case tkOr:
		return 0
	default:
		return -1
	}
}

func toRPN(toks []token) ([]token, error) {
	var out []token
	var ops []token
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch t.kind {
		case tkCol, tkString, tkNumber:
			out = append(out, t)
		case tkNot, tkAnd, tkOr, tkOp:
			for len(ops) > 0 {
				top := ops[len(ops)-1]
				if top.kind == tkLParen {
					break
				}
				if precedence(top) >= precedence(t) {
					out = append(out, top)
					ops = ops[:len(ops)-1]
					continue
				}
				break
			}
			ops = append(ops, t)
		case tkLParen:
			ops = append(ops, t)
		case tkRParen:
			found := false
			for len(ops) > 0 {
				top := ops[len(ops)-1]
				ops = ops[:len(ops)-1]
				if top.kind == tkLParen {
					found = true
					break
				}
				out = append(out, top)
			}
			if !found {
				return nil, fmt.Errorf("mismatched parentheses")
			}
		default:
			return nil, fmt.Errorf("unexpected token in expression")
		}
	}
	for i := len(ops) - 1; i >= 0; i-- {
		if ops[i].kind == tkLParen || ops[i].kind == tkRParen {
			return nil, fmt.Errorf("mismatched parentheses")
		}
		out = append(out, ops[i])
	}
	return out, nil
}

// evalRPN evaluates the predicate in RPN form for a given row of cell strings.
func evalRPN(rpn []token, row []string) (bool, error) {
	// helper to fetch string for a token value
	getString := func(t token) (string, error) {
		switch t.kind {
		case tkString:
			return t.val, nil
		case tkNumber:
			return t.val, nil
		case tkCol:
			// t.val like "$12"
			num := strings.TrimPrefix(t.val, "$")
			idx, err := strconv.Atoi(num)
			if err != nil || idx <= 0 {
				return "", fmt.Errorf("invalid column index")
			}
			pos := idx - 1
			if pos >= 0 && pos < len(row) {
				return row[pos], nil
			}
			return "", nil
		default:
			return "", fmt.Errorf("unexpected token for value")
		}
	}

	// helper parse number
	parseNum := func(s string) (float64, bool) {
		s = strings.ReplaceAll(s, ",", "")
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return v, true
		}
		return 0, false
	}

	var st []bool
	for _, t := range rpn {
		switch t.kind {
		case tkString, tkNumber, tkCol:
			// push placeholder on a temp value stack by encoding string onto a marker
			// We'll encode as pushing a boolean marker onto st along with a special op in a parallel stack is overkill.
			// Instead, treat operands by pushing onto an auxiliary stack of tokens.
			// For simplicity: maintain a stack of tokens representing intermediate values.
			// We'll implement a small inner stack here.
			// Defer: we change approach below to use a value token stack.
		}
	}
	// Re-implement using a token stack for values:
	var valStack []token
	for _, t := range rpn {
		switch t.kind {
		case tkString, tkNumber, tkCol:
			valStack = append(valStack, t)
		case tkNot:
			if len(st) == 0 {
				// Evaluate the next value token into boolean then apply not
				if len(valStack) == 0 {
					return false, fmt.Errorf("invalid NOT operand")
				}
				v := valStack[len(valStack)-1]
				valStack = valStack[:len(valStack)-1]
				// non-empty string or non-zero number considered truthy
				vs, _ := getString(v)
				b := false
				if vs != "" {
					b = true
				}
				st = append(st, !b)
			} else {
				b := st[len(st)-1]
				st = st[:len(st)-1]
				st = append(st, !b)
			}
		case tkAnd, tkOr:
			// ensure two booleans on stack; if missing, try evaluate from valStack
			for len(st) < 2 {
				if len(valStack) == 0 {
					return false, fmt.Errorf("invalid boolean operands")
				}
				v := valStack[len(valStack)-1]
				valStack = valStack[:len(valStack)-1]
				vs, _ := getString(v)
				st = append(st, vs != "")
			}
			b2 := st[len(st)-1]
			b1 := st[len(st)-2]
			st = st[:len(st)-2]
			if t.kind == tkAnd {
				st = append(st, b1 && b2)
			} else {
				st = append(st, b1 || b2)
			}
		case tkOp:
			// binary comparison: need two value tokens
			if len(valStack) < 2 {
				return false, fmt.Errorf("invalid comparison operands")
			}
			r := valStack[len(valStack)-1]
			l := valStack[len(valStack)-2]
			valStack = valStack[:len(valStack)-2]
			ls, _ := getString(l)
			rs, _ := getString(r)
			var res bool
			switch strings.ToLower(t.val) {
			case "contains":
				res = strings.Contains(strings.ToLower(ls), strings.ToLower(rs))
			case ">", ">=", "<", "<=":
				ln, lok := parseNum(ls)
				rn, rok := parseNum(rs)
				if lok && rok {
					switch t.val {
					case ">":
						res = ln > rn
					case ">=":
						res = ln >= rn
					case "<":
						res = ln < rn
					case "<=":
						res = ln <= rn
					}
				} else {
					res = false
				}
			case "=", "==":
				res = ls == rs
			case "!=":
				res = ls != rs
			default:
				return false, fmt.Errorf("unsupported operator %q", t.val)
			}
			st = append(st, res)
		default:
			return false, fmt.Errorf("unexpected token during evaluation")
		}
	}
	if len(st) != 1 {
		// If booleans not consolidated, try reducing from remaining valStack
		for len(st) > 1 {
			b := st[len(st)-1]
			st = st[:len(st)-1]
			st[len(st)-1] = st[len(st)-1] && b
		}
		if len(st) != 1 {
			return false, fmt.Errorf("invalid expression result")
		}
	}
	return st[0], nil
}
