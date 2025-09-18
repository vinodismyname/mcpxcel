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
	"github.com/vinodismyname/mcpxcel/pkg/validation"
	"github.com/vinodismyname/mcpxcel/pkg/mcperr"
)

// RegisterInsightsTools wires the sequential_insights planning tool.
func RegisterInsightsTools(s *server.MCPServer, reg *Registry, limits runtime.Limits, mgr *workbooks.Manager) {
	planner := &insights.Planner{Limits: limits, Sessions: insights.NewSessionStore(20)}

	// Define tool with typed schemas
	tool := mcp.NewTool(
		"sequential_insights",
		mcp.WithDescription(`A generalized thought-tracking tool to guide iterative Excel analysis.
  Use this to externalize your reasoning steps and maintain a lightweight plan while you call domain tools.

  Behavior:
  - Records thought_number/total_thoughts, branches, and session_id
  - Always includes a tiny planning card with a next-action cue
  - Optionally lists available tools when show_available_tools=true
  - No recommendations or questions are generated; you choose domain tools

  When to use:
  - Start your analysis with an initial thought and plan
  - After each domain tool call, summarize what you learned and your next step
  - When revising prior steps or branching your approach

  Parameters:
  - thought: Your current step (analysis, revision, hypothesis)
  - next_thought_needed: Continue planning (true) or complete (false)
  - thought_number: Current step index (can exceed initial total)
  - total_thoughts: Estimated steps (adjustable during process)
  - is_revision/revises_thought: Mark corrections to previous thinking
  - branch_from_thought/branch_id: Explore alternative analysis paths
  - session_id: Resume session or start new (auto-created if omitted)
  - reset_session: Clear and restart the referenced session
  - show_available_tools: Include MCP tool catalog in response

  Outputs:
  - thought_number/total_thoughts/next_thought_needed/session_id
  - branches[] and thought_history_length
  - insight_cards[]: Always-on tiny planning card with next-action cue
  - meta: limits and planning_only=true

  Guidance:
  - Interleave: call this tool between domain tool calls (list_structure, preview_sheet, read_range, detect_tables, profile_schema, etc.)
  - If unsure what to do next, set show_available_tools=true to review the tool catalog
  - Keep thoughts concise and focused on the immediate next action`),
		mcp.WithInputSchema[insights.SequentialInsightsInput](),
		mcp.WithOutputSchema[insights.SequentialInsightsOutput](),
	)

s.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in insights.SequentialInsightsInput) (*mcp.CallToolResult, error) {
	if msg := validation.ValidateStruct(in); msg != "" {
		return mcperr.FromText(msg), nil
	}
	out, err := planner.Plan(ctx, in)
		if err != nil {
			return mcperr.FromText("PLANNING_FAILED: " + err.Error()), nil
		}

		// Build a readable text response for clients that only render text
		var lines []string
		// Thought summary with loop tracking
		lines = append(lines, fmt.Sprintf("Thought %d/%d next=%v", out.ThoughtNumber, out.TotalThoughts, out.NextThoughtNeeded))
		lines = append(lines, fmt.Sprintf("Session: %s", out.SessionID))

		if len(out.Branches) > 0 {
			lines = append(lines, fmt.Sprintf("Branches: %v", out.Branches))
		}
		lines = append(lines, fmt.Sprintf("History length: %d", out.ThoughtHistoryLength))

		// Interleaving cue to encourage calling this tool between domain actions
		lines = append(lines, "NextAction: summarize findings here, then call your next MCP tool; loop back with your next thought.")

		// Available tools (help list), gated by input flag
		if in.ShowAvailableTools {
			if reg != nil {
				if tools, err := reg.Tools(ctx); err == nil && len(tools) > 0 {
					lines = append(lines, "")
					lines = append(lines, "Available tools:")
					for _, t := range tools {
						desc := t.Description
						if strings.TrimSpace(desc) == "" {
							desc = "(no description)"
						}
						desc = truncateText(desc, 160)
						lines = append(lines, fmt.Sprintf("- %s — %s", t.Name, desc))
					}
				}
			}
		} else {
			// Light nudge to surface the catalog early when not requested
			if out.ThoughtNumber <= 2 {
				lines = append(lines, "Tip: set show_available_tools=true to list available MCP tools.")
			}
		}

		text := strings.Join(lines, "\n")

		summary := fmt.Sprintf("thought %d/%d", out.ThoughtNumber, out.TotalThoughts)
		res := mcp.NewToolResultStructured(out, summary)
		res.Content = []mcp.Content{mcp.NewTextContent(text)}
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
        if msg := validation.ValidateStruct(in); msg != "" {
            return mcperr.FromText(msg), nil
        }
        if strings.TrimSpace(in.Path) == "" {
            return mcperr.FromText("VALIDATION: path is required"), nil
        }
        if strings.TrimSpace(in.Sheet) == "" {
            return mcperr.FromText("VALIDATION: sheet is required"), nil
		}
		out, err := detector.DetectTables(ctx, in)
		if err != nil {
			if mcperr.IsInvalidSheet(err) {
				return mcperr.FromText("INVALID_SHEET: sheet not found"), nil
			}
			return mcperr.FromText("DETECTION_FAILED: " + err.Error()), nil
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
        if msg := validation.ValidateStruct(in); msg != "" {
            return mcperr.FromText(msg), nil
        }
        if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Sheet) == "" || strings.TrimSpace(in.Range) == "" {
            return mcperr.FromText("VALIDATION: path, sheet, and range are required"), nil
        }
		out, err := profiler.ProfileSchema(ctx, in)
		if err != nil {
			low := strings.ToLower(err.Error())
			if mcperr.IsInvalidSheet(err) {
				return mcperr.FromText("INVALID_SHEET: sheet not found"), nil
			}
			if strings.Contains(low, "invalid range") || strings.Contains(low, "coordinates") {
				return mcperr.FromText("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			return mcperr.FromText("PROFILING_FAILED: " + err.Error()), nil
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
        if msg := validation.ValidateStruct(in); msg != "" {
            return mcperr.FromText(msg), nil
        }
        if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Sheet) == "" || strings.TrimSpace(in.Range) == "" {
            return mcperr.FromText("VALIDATION: path, sheet, and range are required"), nil
        }
		out, err := composer.CompositionShift(ctx, in)
		if err != nil {
			low := strings.ToLower(err.Error())
			if mcperr.IsInvalidSheet(err) {
				return mcperr.FromText("INVALID_SHEET: sheet not found"), nil
			}
			if strings.Contains(low, "invalid range") || strings.Contains(low, "coordinates") {
				return mcperr.FromText("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			return mcperr.FromText("ANALYSIS_FAILED: " + err.Error()), nil
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
        if msg := validation.ValidateStruct(in); msg != "" {
            return mcperr.FromText(msg), nil
        }
        if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Sheet) == "" || strings.TrimSpace(in.Range) == "" {
            return mcperr.FromText("VALIDATION: path, sheet, and range are required"), nil
        }
		out, err := concentrator.ConcentrationMetrics(ctx, in)
		if err != nil {
			low := strings.ToLower(err.Error())
			if mcperr.IsInvalidSheet(err) {
				return mcperr.FromText("INVALID_SHEET: sheet not found"), nil
			}
			if strings.Contains(low, "invalid range") || strings.Contains(low, "coordinates") {
				return mcperr.FromText("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			return mcperr.FromText("ANALYSIS_FAILED: " + err.Error()), nil
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
        if msg := validation.ValidateStruct(in); msg != "" {
            return mcperr.FromText(msg), nil
        }
        if strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Sheet) == "" || strings.TrimSpace(in.Range) == "" {
            return mcperr.FromText("VALIDATION: path, sheet, and range are required"), nil
        }
		out, err := funneler.FunnelAnalysis(ctx, in)
		if err != nil {
			low := strings.ToLower(err.Error())
			if mcperr.IsInvalidSheet(err) {
				return mcperr.FromText("INVALID_SHEET: sheet not found"), nil
			}
			if strings.Contains(low, "invalid range") || strings.Contains(low, "coordinates") {
				return mcperr.FromText("VALIDATION: invalid range; use A1:D50 or a defined name"), nil
			}
			return mcperr.FromText("ANALYSIS_FAILED: " + err.Error()), nil
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

// truncateText returns a rune-safe truncated string with an ellipsis when needed.
func truncateText(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
