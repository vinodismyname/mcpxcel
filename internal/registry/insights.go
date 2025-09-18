package registry

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/vinodismyname/mcpxcel/internal/insights"
	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
)

// RegisterInsightsTools wires the sequential_insights planning tool.
func RegisterInsightsTools(s *server.MCPServer, reg *Registry, limits runtime.Limits, mgr *workbooks.Manager) {
	planner := &insights.Planner{Limits: limits, Mgr: mgr}

	// Define tool with typed schemas
	tool := mcp.NewTool(
		"sequential_insights",
		mcp.WithDescription("Domain-neutral planning for stepwise analysis with recommended tools and clarifying questions"),
		mcp.WithInputSchema[insights.SequentialInsightsInput](),
		mcp.WithOutputSchema[insights.SequentialInsightsOutput](),
	)

	s.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in insights.SequentialInsightsInput) (*mcp.CallToolResult, error) {
		out, err := planner.Plan(ctx, in)
		if err != nil {
			return mcp.NewToolResultError("PLANNING_FAILED: " + err.Error()), nil
		}
		// Attach a concise text summary for clients ignoring structured out
		summary := out.CurrentStep
		res := mcp.NewToolResultStructured(out, summary)
		res.Content = []mcp.Content{mcp.NewTextContent(summary)}
		return res, nil
	}))

	reg.Register(tool)

	// detect_tables
	detector := &insights.Detector{Limits: limits, Mgr: mgr}
	dt := mcp.NewTool(
		"detect_tables",
		mcp.WithDescription("Detect rectangular table regions (multiple tables per sheet) and return Top-K candidates"),
		mcp.WithInputSchema[insights.DetectTablesInput](),
		mcp.WithOutputSchema[insights.DetectTablesOutput](),
	)
	s.AddTool(dt, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in insights.DetectTablesInput) (*mcp.CallToolResult, error) {
		if strings.TrimSpace(in.Path) == "" {
			return mcp.NewToolResultError("VALIDATION: path is required"), nil
		}
		if strings.TrimSpace(in.Sheet) == "" {
			return mcp.NewToolResultError("VALIDATION: sheet is required"), nil
		}
		out, err := detector.DetectTables(ctx, in)
		if err != nil {
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			return mcp.NewToolResultError("DETECTION_FAILED: " + err.Error()), nil
		}
		// Build concise summary
		summary := fmt.Sprintf("candidates=%d scanned_rows=%d scanned_cols=%d truncated=%v", len(out.Candidates), out.Meta.ScannedRows, out.Meta.ScannedCols, out.Meta.Truncated)
		var lines []string
		lines = append(lines, summary)
		maxLines := len(out.Candidates)
		if maxLines > 5 {
			maxLines = 5
		}
		for i := 0; i < maxLines; i++ {
			c := out.Candidates[i]
			lines = append(lines, fmt.Sprintf("- %s rows=%d cols=%d conf=%.3f hdr=%v", c.Range, c.Rows, c.Cols, c.Confidence, previewHeader(c.Header, 6)))
		}
		text := strings.Join(lines, "\n")
		res := mcp.NewToolResultStructured(out, summary)
		res.Content = []mcp.Content{mcp.NewTextContent(text)}
		return res, nil
	}))
	reg.Register(dt)
}

// previewHeader returns a bounded preview slice for compact summaries.
func previewHeader(h []string, n int) []string {
	if len(h) <= n {
		return h
	}
	return h[:n]
}
