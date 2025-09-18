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

	// profile_schema
	profiler := &insights.Profiler{Limits: limits, Mgr: mgr}
	ps := mcp.NewTool(
		"profile_schema",
		mcp.WithDescription("Infer column roles (measure/dimension/time/id/target) and run data quality checks over a bounded sample"),
		mcp.WithInputSchema[insights.ProfileSchemaInput](),
		mcp.WithOutputSchema[insights.ProfileSchemaOutput](),
	)
	s.AddTool(ps, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in insights.ProfileSchemaInput) (*mcp.CallToolResult, error) {
		if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Sheet) == "" || strings.TrimSpace(in.Range) == "" {
			return mcp.NewToolResultError("VALIDATION: path, sheet, and range are required"), nil
		}
		out, err := profiler.ProfileSchema(ctx, in)
		if err != nil {
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			if strings.Contains(low, "invalid range") || strings.Contains(low, "coordinates") {
				return mcp.NewToolResultError("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			return mcp.NewToolResultError("PROFILING_FAILED: " + err.Error()), nil
		}
		// Build concise text summary
		summary := fmt.Sprintf("cols=%d sampled_rows=%d truncated=%v", len(out.Columns), out.Meta.SampledRows, out.Meta.Truncated)
		var lines []string
		lines = append(lines, summary)
		max := len(out.Columns)
		if max > 8 {
			max = 8
		}
		for i := 0; i < max; i++ {
			c := out.Columns[i]
			lines = append(lines, fmt.Sprintf("$%d %q role=%s type=%s miss=%.1f%% uniq=%.3f warnings=%v", c.Index, c.Name, c.Role, c.Type, c.MissingPct, c.UniqueRatio, previewHeader(c.Warnings, 3)))
		}
		text := strings.Join(lines, "\n")
		res := mcp.NewToolResultStructured(out, summary)
		res.Content = []mcp.Content{mcp.NewTextContent(text)}
		return res, nil
	}))
	reg.Register(ps)

	// composition_shift
	composer := &insights.Composer{Limits: limits, Mgr: mgr}
	cs := mcp.NewTool(
		"composition_shift",
		mcp.WithDescription("Compute share-of-total by group across two periods and highlight mix shifts (±pp)"),
		mcp.WithInputSchema[insights.CompositionShiftInput](),
		mcp.WithOutputSchema[insights.CompositionShiftOutput](),
	)
	s.AddTool(cs, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in insights.CompositionShiftInput) (*mcp.CallToolResult, error) {
		if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Sheet) == "" || strings.TrimSpace(in.Range) == "" {
			return mcp.NewToolResultError("VALIDATION: path, sheet, and range are required"), nil
		}
		out, err := composer.CompositionShift(ctx, in)
		if err != nil {
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			if strings.Contains(low, "invalid range") || strings.Contains(low, "coordinates") {
				return mcp.NewToolResultError("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			return mcp.NewToolResultError("ANALYSIS_FAILED: " + err.Error()), nil
		}
		summary := fmt.Sprintf("periods=[%s→%s] groups=%d topN=%d truncated=%v", out.PeriodBaseline, out.PeriodCurrent, len(out.Groups), out.TopN, out.Meta.Truncated)
		res := mcp.NewToolResultStructured(out, summary)
		res.Content = []mcp.Content{mcp.NewTextContent(summary)}
		return res, nil
	}))
	reg.Register(cs)

	// concentration_metrics
	concentrator := &insights.Concentrator{Limits: limits, Mgr: mgr}
	cm := mcp.NewTool(
		"concentration_metrics",
		mcp.WithDescription("Compute Top-N share and HHI concentration metrics for a dimension"),
		mcp.WithInputSchema[insights.ConcentrationMetricsInput](),
		mcp.WithOutputSchema[insights.ConcentrationMetricsOutput](),
	)
	s.AddTool(cm, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in insights.ConcentrationMetricsInput) (*mcp.CallToolResult, error) {
		if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Sheet) == "" || strings.TrimSpace(in.Range) == "" {
			return mcp.NewToolResultError("VALIDATION: path, sheet, and range are required"), nil
		}
		out, err := concentrator.ConcentrationMetrics(ctx, in)
		if err != nil {
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			if strings.Contains(low, "invalid range") || strings.Contains(low, "coordinates") {
				return mcp.NewToolResultError("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			return mcp.NewToolResultError("ANALYSIS_FAILED: " + err.Error()), nil
		}
		summary := fmt.Sprintf("topN=%d HHI=%.3f band=%s groups=%d truncated=%v", out.TopN, out.HHI, out.Band, len(out.Groups), out.Meta.Truncated)
		res := mcp.NewToolResultStructured(out, summary)
		res.Content = []mcp.Content{mcp.NewTextContent(summary)}
		return res, nil
	}))
	reg.Register(cm)

	// funnel_analysis
	funneler := &insights.Funneler{Limits: limits, Mgr: mgr}
	fa := mcp.NewTool(
		"funnel_analysis",
		mcp.WithDescription("Compute stage and cumulative conversion across ordered funnel stages; detect bottlenecks"),
		mcp.WithInputSchema[insights.FunnelAnalysisInput](),
		mcp.WithOutputSchema[insights.FunnelAnalysisOutput](),
	)
	s.AddTool(fa, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in insights.FunnelAnalysisInput) (*mcp.CallToolResult, error) {
		if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Sheet) == "" || strings.TrimSpace(in.Range) == "" {
			return mcp.NewToolResultError("VALIDATION: path, sheet, and range are required"), nil
		}
		out, err := funneler.FunnelAnalysis(ctx, in)
		if err != nil {
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") {
				return mcp.NewToolResultError("INVALID_SHEET: sheet not found"), nil
			}
			if strings.Contains(low, "invalid range") || strings.Contains(low, "coordinates") {
				return mcp.NewToolResultError("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			return mcp.NewToolResultError("ANALYSIS_FAILED: " + err.Error()), nil
		}
		summary := fmt.Sprintf("stages=%d bottleneck=%s truncated=%v", len(out.Stages), out.Bottleneck, out.Meta.Truncated)
		res := mcp.NewToolResultStructured(out, summary)
		res.Content = []mcp.Content{mcp.NewTextContent(summary)}
		return res, nil
	}))
	reg.Register(fa)
}

// previewHeader returns a bounded preview slice for compact summaries.
func previewHeader(h []string, n int) []string {
	if len(h) <= n {
		return h
	}
	return h[:n]
}
