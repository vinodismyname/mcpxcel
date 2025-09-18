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
		mcp.WithDescription("A sequential thinking planner for dynamic, reflective Excel analysis. It breaks an ambiguous objective into concrete steps, recommends MCP tools with parameters and rationale, and tracks progress so you can revise, branch, or continue as understanding deepens. Planning‑only by default; deterministic compute primitives are gated by config and disabled by default.\n\nWhen to use: breaking down complex or multi‑step workbook tasks; planning with room for revision; maintaining context across steps; deciding which tools to call and in what order; filtering out irrelevant details while staying within limits.\n\nKey behaviors: adjust total_steps up or down as you go; mark next_step_needed when more iteration is required; revise or branch using revision/branch fields; generate a hypothesis and verify it via subsequent steps; emit recommended_tools with confidence, rationale, priority, suggested_inputs, and alternatives; ask clarifying questions when the context is ambiguous; surface effective limits and truncation in meta.\n\nParameters (cursor takes precedence over path): objective (required); path or cursor (cursor binds to canonical path + file mtime); hints (e.g., sheet, range, date_col, id_col, measure, target, stages); constraints (e.g., max_rows, top_n, max_groups); step_number (1‑based), total_steps (estimate), next_step_needed (bool), revision (free‑form), branch (free‑form).\n\nOutputs: current_step (concise step description), recommended_tools[{tool_name, confidence (0–1), rationale, priority, suggested_inputs, alternatives}], questions[], optional insight_cards[] (planning‑only by default), and meta{limits, planning_only, compute_enabled, truncated}. Guidance: start with list_structure/preview_sheet to ground context; keep steps small and reversible; prefer cursor‑first pagination; only set next_step_needed=false when a satisfactory answer is reached."),
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
		mcp.WithDescription("Detect multiple rectangular table regions within a sheet using a bounded streaming scan and simple header heuristics. Returns Top‑K ranked candidates with range, header preview, confidence, and optional header samples. Use when a sheet contains several tables separated by blanks and you need a suggested range to analyze. Limits/caps constrain scan rows/cols; errors include INVALID_SHEET and DETECTION_FAILED."),
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
		mcp.WithDescription("Profile a bounded range to infer column roles (measure, dimension, time, id, target) and run data quality checks (missingness, duplicates, negative values in nonnegative fields, >100% in percent‑like, mixed types). Use this after choosing a table/range to ground downstream analysis. Sampling is bounded by config; errors include VALIDATION (range), INVALID_SHEET, and PROFILING_FAILED."),
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
		mcp.WithDescription("Compute share‑of‑total by group across two periods and highlight mix shifts in percentage points. Accepts 1‑based indices for dimension/measure (and optional time), detects baseline/current periods when not provided, and caps results to Top‑N with the rest grouped into 'Other'. Limits cap processed cells; errors include VALIDATION (range/indices), INVALID_SHEET, and ANALYSIS_FAILED."),
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
		mcp.WithDescription("Compute Top‑N share and Herfindahl‑Hirschman Index (HHI) for a grouping dimension. Accepts 1‑based indices for dimension and numeric measure within the range; returns Top‑N group shares, 'Other' share, HHI value, and a concentration band. Limits cap processed cells; errors include VALIDATION (range/indices), INVALID_SHEET, and ANALYSIS_FAILED."),
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
		mcp.WithDescription("Compute stage and cumulative conversion across ordered funnel stages and identify bottlenecks. Stages are detected from header names when not provided, or specified via 1‑based stage_indices within the range. Use this for pipeline/step data; results include per‑stage and cumulative conversion. Limits cap processed cells; errors include VALIDATION (range/indices), INVALID_SHEET, and ANALYSIS_FAILED."),
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
