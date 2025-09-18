package mcperr

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// Code defines a canonical MCP error code used across tools.
type Code string

const (
	// Validation & Input
	Validation        Code = "VALIDATION"
	InvalidHandle     Code = "INVALID_HANDLE"
	InvalidSheet      Code = "INVALID_SHEET"
	CursorInvalid     Code = "CURSOR_INVALID"
	CursorBuildFailed Code = "CURSOR_BUILD_FAILED"

	// Resource & Limits
	BusyResource    Code = "BUSY_RESOURCE"
	Timeout         Code = "TIMEOUT"
	LimitExceeded   Code = "LIMIT_EXCEEDED"
	PayloadTooLarge Code = "PAYLOAD_TOO_LARGE"
	FileTooLarge    Code = "FILE_TOO_LARGE"

	// IO & Formats
	OpenFailed         Code = "OPEN_FAILED"
	DiscoveryFailed    Code = "DISCOVERY_FAILED"
	PreviewFailed      Code = "PREVIEW_FAILED"
	ReadFailed         Code = "READ_FAILED"
	WriteFailed        Code = "WRITE_FAILED"
	ApplyFormulaFailed Code = "APPLY_FORMULA_FAILED"
	SearchFailed       Code = "SEARCH_FAILED"
	FilterFailed       Code = "FILTER_FAILED"

	// Analysis/Insights
	PlanningFailed  Code = "PLANNING_FAILED"
	DetectionFailed Code = "DETECTION_FAILED"
	AnalysisFailed  Code = "ANALYSIS_FAILED"

	// Integrity
	CorruptWorkbook   Code = "CORRUPT_WORKBOOK"
	UnsupportedFormat Code = "UNSUPPORTED_FORMAT"
	PermissionDenied  Code = "PERMISSION_DENIED"
)

// Entry documents a code's standard message, retry semantics, and next steps.
type Entry struct {
	Code      Code
	Message   string
	Retryable bool
	NextSteps []string
}

// catalog maps canonical codes to guidance. Messages can be overridden per error.
var catalog = map[Code]Entry{
	Validation:        {Code: Validation, Message: "invalid inputs", Retryable: true, NextSteps: []string{"Correct the inputs per schema and retry", "See examples in tool description"}},
	InvalidHandle:     {Code: InvalidHandle, Message: "workbook handle not found or expired", Retryable: true, NextSteps: []string{"Reopen the workbook via path and retry"}},
	InvalidSheet:      {Code: InvalidSheet, Message: "sheet not found", Retryable: true, NextSteps: []string{"Call list_structure to verify sheet names", "Check case and spacing"}},
	CursorInvalid:     {Code: CursorInvalid, Message: "cursor is invalid for current context", Retryable: true, NextSteps: []string{"Restart pagination from the first page", "Avoid edits between pages or reissue query"}},
	CursorBuildFailed: {Code: CursorBuildFailed, Message: "failed to encode next page cursor", Retryable: true, NextSteps: []string{"Retry or narrow scope (smaller pages)"}},

	BusyResource:    {Code: BusyResource, Message: "concurrent request limit reached", Retryable: true, NextSteps: []string{"Retry after a short delay"}},
	Timeout:         {Code: Timeout, Message: "operation exceeded configured time limit", Retryable: true, NextSteps: []string{"Narrow scope (rows/cells) or increase timeout", "Prefer cursor-first pagination"}},
	LimitExceeded:   {Code: LimitExceeded, Message: "operation exceeded configured limits", Retryable: true, NextSteps: []string{"Narrow range, reduce groups, or lower page size"}},
	PayloadTooLarge: {Code: PayloadTooLarge, Message: "payload exceeds configured size", Retryable: true, NextSteps: []string{"Reduce range size or split into batches"}},
	FileTooLarge:    {Code: FileTooLarge, Message: "file exceeds configured size", Retryable: false, NextSteps: []string{"Use a smaller workbook or increase the limit"}},

	OpenFailed:         {Code: OpenFailed, Message: "failed to open workbook", Retryable: true, NextSteps: []string{"Verify path, permissions, and format"}},
	DiscoveryFailed:    {Code: DiscoveryFailed, Message: "failed to discover structure", Retryable: true, NextSteps: []string{"Retry or open the workbook and inspect"}},
	PreviewFailed:      {Code: PreviewFailed, Message: "failed to generate preview", Retryable: true, NextSteps: []string{"Retry with fewer rows or JSON encoding"}},
	ReadFailed:         {Code: ReadFailed, Message: "failed to read range", Retryable: true, NextSteps: []string{"Verify A1 range and retry", "Reduce max_cells if needed"}},
	WriteFailed:        {Code: WriteFailed, Message: "failed to write range", Retryable: false, NextSteps: []string{"Validate range and values", "Avoid relying on implicit dimension growth"}},
	ApplyFormulaFailed: {Code: ApplyFormulaFailed, Message: "failed to apply formula", Retryable: false, NextSteps: []string{"Verify formula syntax and range dimensions"}},
	SearchFailed:       {Code: SearchFailed, Message: "search execution failed", Retryable: true, NextSteps: []string{"Simplify query or disable regex", "Reduce snapshot_cols"}},
	FilterFailed:       {Code: FilterFailed, Message: "filter execution failed", Retryable: true, NextSteps: []string{"Simplify predicate or reduce snapshot_cols"}},

	PlanningFailed:  {Code: PlanningFailed, Message: "planning failed", Retryable: true, NextSteps: []string{"Retry with a simpler objective or provide hints"}},
	DetectionFailed: {Code: DetectionFailed, Message: "table detection failed", Retryable: true, NextSteps: []string{"Specify an approximate range or reduce scan bounds"}},
	AnalysisFailed:  {Code: AnalysisFailed, Message: "analysis failed", Retryable: true, NextSteps: []string{"Verify range and indices", "Reduce max_cells or top_n"}},

	CorruptWorkbook:   {Code: CorruptWorkbook, Message: "workbook appears corrupt or unreadable", Retryable: false, NextSteps: []string{"Open in Excel and re-save or repair", "Provide a clean copy"}},
	UnsupportedFormat: {Code: UnsupportedFormat, Message: "unsupported workbook format", Retryable: false, NextSteps: []string{"Convert to .xlsx and retry"}},
	PermissionDenied:  {Code: PermissionDenied, Message: "insufficient permissions to access path", Retryable: false, NextSteps: []string{"Adjust permissions or choose an allowed directory"}},
}

// normalize builds a standard error string including next steps for MCP clients that
// surface only a message string. Format: "CODE: message" followed by a guidance tail.
func normalize(code Code, msg string) string {
	base := strings.TrimSpace(msg)
	e, ok := catalog[code]
	if !ok {
		// Unknown code; preserve as-is
		if base == "" {
			return string(code)
		}
		return fmt.Sprintf("%s: %s", string(code), base)
	}
	if base == "" {
		base = e.Message
	}
	// Append compact nextSteps guidance inline to aid clients lacking structured fields.
	guidance := ""
	if len(e.NextSteps) > 0 {
		guidance = " | nextSteps: " + strings.Join(e.NextSteps, "; ")
	}
	return fmt.Sprintf("%s: %s%s", e.Code, base, guidance)
}

// FromText parses a "CODE: message" string, enriches it with catalog guidance,
// and returns an MCP tool error result.
func FromText(text string) *mcp.CallToolResult {
	t := strings.TrimSpace(text)
	if t == "" {
		return mcp.NewToolResultError(normalize(Validation, ""))
	}
	parts := strings.SplitN(t, ":", 2)
	if len(parts) == 0 {
		return mcp.NewToolResultError(normalize(Validation, t))
	}
	code := Code(strings.TrimSpace(parts[0]))
	msg := ""
	if len(parts) > 1 {
		msg = strings.TrimSpace(parts[1])
	}
	return mcp.NewToolResultError(normalize(code, msg))
}

// New returns an MCP error result for a given code and optional message override.
func New(code Code, message string) *mcp.CallToolResult {
	return mcp.NewToolResultError(normalize(code, message))
}

// Wrapf formats details and returns an MCP error result for the code.
func Wrapf(code Code, format string, args ...any) *mcp.CallToolResult {
	return mcp.NewToolResultError(normalize(code, fmt.Sprintf(format, args...)))
}

// Helpers for common mappings

// IsInvalidSheet returns true if an error matches common excelize "sheet does not exist" messages.
func IsInvalidSheet(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist")
}
