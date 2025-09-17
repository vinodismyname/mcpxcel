package registry

import (
    "context"
    "fmt"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    "github.com/vinoddu/mcpxcel/internal/runtime"
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
func RegisterFoundationTools(s *server.MCPServer, reg *Registry, limits runtime.Limits) {
    // open_workbook
    openTool := mcp.NewTool(
        "open_workbook",
        mcp.WithDescription("Open a workbook and return a handle ID with effective limits"),
        mcp.WithString("path", mcp.Required(), mcp.Description("Absolute or allowed path to an Excel workbook (.xlsx, .xlsm, .xltx, .xltm)")),
        mcp.WithOutputSchema[OpenWorkbookOutput](),
    )
    s.AddTool(openTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in OpenWorkbookInput) (*mcp.CallToolResult, error) {
        // Placeholder implementation; real logic in task 7
        return mcp.NewToolResultError("UNIMPLEMENTED: open_workbook"), nil
    }))
    reg.Register(openTool)

    // close_workbook
    closeTool := mcp.NewTool(
        "close_workbook",
        mcp.WithDescription("Close a previously opened workbook handle"),
        mcp.WithString("workbook_id", mcp.Required(), mcp.Description("Workbook handle ID")),
        mcp.WithOutputSchema[struct{
            Success bool `json:"success" jsonschema_description:"True when the handle was closed"`
        }](),
    )
    s.AddTool(closeTool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in CloseWorkbookInput) (*mcp.CallToolResult, error) {
        return mcp.NewToolResultError("UNIMPLEMENTED: close_workbook"), nil
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
        return mcp.NewToolResultError("UNIMPLEMENTED: list_structure"), nil
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
        return mcp.NewToolResultError("UNIMPLEMENTED: preview_sheet"), nil
    }))
    reg.Register(preview)

    // read_range
    readRange := mcp.NewTool(
        "read_range",
        mcp.WithDescription("Return a bounded cell range with pagination metadata"),
        mcp.WithString("workbook_id", mcp.Required(), mcp.Description("Workbook handle ID")),
        mcp.WithString("sheet", mcp.Required(), mcp.Description("Sheet name")),
        mcp.WithString("range", mcp.Required(), mcp.Description("A1-style cell range (e.g., A1:D50)")),
        mcp.WithNumber("max_cells", mcp.DefaultNumber(float64(limits.MaxCellsPerOp)), mcp.Min(1), mcp.Description("Max cells to return before truncation")),
        mcp.WithOutputSchema[ReadRangeOutput](),
    )
    s.AddTool(readRange, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in ReadRangeInput) (*mcp.CallToolResult, error) {
        return mcp.NewToolResultError("UNIMPLEMENTED: read_range"), nil
    }))
    reg.Register(readRange)

    // Annotate tool capability flags via log-friendly text until telemetry middleware is added
    _ = fmt.Sprintf("foundation tools registered: %d", 5)
}
