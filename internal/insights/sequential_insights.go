package insights

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
	"github.com/xuri/excelize/v2"
)

// Input schema for the sequential_insights planning tool.
type SequentialInsightsInput struct {
	Objective      string            `json:"objective" jsonschema_description:"High-level goal for analysis, e.g., 'identify KPI change drivers'"`
	Path           string            `json:"path,omitempty" jsonschema_description:"Absolute or allowed path to an Excel workbook (ignored if cursor provided)"`
	Cursor         string            `json:"cursor,omitempty" jsonschema_description:"Opaque pagination or workflow cursor; takes precedence over path"`
	Hints          map[string]string `json:"hints,omitempty" jsonschema_description:"Optional hints such as sheet, range, date_col, id_col, measure, target, stages"`
	Constraints    map[string]int    `json:"constraints,omitempty" jsonschema_description:"Optional constraints like max_rows, top_n, max_groups"`
	StepNumber     int               `json:"step_number,omitempty" jsonschema_description:"Current planner step number (1-based)"`
	TotalSteps     int               `json:"total_steps,omitempty" jsonschema_description:"Total steps planned (advisory)"`
	NextStepNeeded bool              `json:"next_step_needed,omitempty" jsonschema_description:"Whether a next step should be taken after this planning call"`
	Revision       string            `json:"revision,omitempty" jsonschema_description:"Optional revision identifier used by callers for plan tracking"`
	Branch         string            `json:"branch,omitempty" jsonschema_description:"Optional branch identifier used by callers for plan tracking"`
}

// Recommendation surfaced by the planner.
type Recommendation struct {
	ToolName        string           `json:"tool_name"`
	Confidence      float64          `json:"confidence"`
	Rationale       string           `json:"rationale"`
	Priority        int              `json:"priority"`
	SuggestedInputs map[string]any   `json:"suggested_inputs,omitempty"`
	Alternatives    []map[string]any `json:"alternatives,omitempty"`
}

// InsightCard is a compact, optional summary of bounded compute results.
// In planning-only mode, this will typically be empty.
type InsightCard struct {
	Title       string   `json:"title"`
	Finding     string   `json:"finding"`
	Evidence    []string `json:"evidence,omitempty"`
	Assumptions []string `json:"assumptions,omitempty"`
	NextAction  string   `json:"next_action,omitempty"`
}

// PlannerMeta returns effective limits and flags indicating whether compute is enabled.
type PlannerMeta struct {
	Limits         runtime.Limits `json:"limits"`
	PlanningOnly   bool           `json:"planning_only"`
	ComputeEnabled bool           `json:"compute_enabled"`
	Truncated      bool           `json:"truncated"`
}

// Output schema for the sequential_insights tool.
type SequentialInsightsOutput struct {
	CurrentStep      string           `json:"current_step"`
	RecommendedTools []Recommendation `json:"recommended_tools"`
	Questions        []string         `json:"questions,omitempty"`
	InsightCards     []InsightCard    `json:"insight_cards,omitempty"`
	Meta             PlannerMeta      `json:"meta"`
}

// Planner encapsulates dependencies the planning tool needs.
type Planner struct {
	Limits runtime.Limits
	Mgr    *workbooks.Manager
}

// Plan computes a deterministic set of recommendations and clarifying questions.
func (p *Planner) Plan(ctx context.Context, in SequentialInsightsInput) (SequentialInsightsOutput, error) {
	// Cursor precedence over path: if a recognized pagination cursor contains a path, prefer it.
	// We keep this planning tool cursor-agnostic, but allow callers to pass either.
	// For now, we'll use the provided Path as-is when non-empty.

	var out SequentialInsightsOutput
	out.Meta = PlannerMeta{Limits: p.Limits, PlanningOnly: true, ComputeEnabled: false, Truncated: false}

	objective := strings.TrimSpace(strings.ToLower(in.Objective))
	if objective == "" {
		out.CurrentStep = "clarify-objective"
		out.Questions = append(out.Questions, "What is the analysis objective? (e.g., 'summarize KPI by month', 'find outliers', 'search for value')")
		out.RecommendedTools = []Recommendation{
			{ToolName: "list_structure", Confidence: 0.55, Priority: 1, Rationale: "Establish workbook context: sheets, sizes, and headers"},
		}
		return out, nil
	}

	// Inspect workbook to drive questions (sheet count, header preview) when a path is provided.
	var sheetNames []string
	var headersPreview []string
	canonical := ""
	if strings.TrimSpace(in.Path) != "" {
		if id, cpath, err := p.Mgr.GetOrOpenByPath(ctx, in.Path); err == nil {
			canonical = cpath
			_ = p.Mgr.WithRead(id, func(f *excelize.File, _ int64) error {
				// Collect sheet names in index order
				m := f.GetSheetMap()
				idx := make([]int, 0, len(m))
				for i := range m {
					idx = append(idx, i)
				}
				sort.Ints(idx)
				for _, i := range idx {
					sheetNames = append(sheetNames, m[i])
				}
				if len(sheetNames) > 0 {
					// Best-effort: fetch first row as headers for the first sheet
					rows, rerr := f.Rows(sheetNames[0])
					if rerr == nil {
						if rows.Next() {
							if hdr, herr := rows.Columns(); herr == nil {
								headersPreview = hdr
							}
						}
						_ = rows.Close()
					}
				}
				return nil
			})
		}
	}

	// Heuristics to map objective → recommended tools
	var recs []Recommendation
	var questions []string

	// Common intents via regex shortcuts
	reSearch := regexp.MustCompile(`\b(search|find|lookup)\b`)
	reFilter := regexp.MustCompile(`\b(filter|where|subset|rows matching)\b`)
	rePreview := regexp.MustCompile(`\b(preview|sample|first\s*\d+\s*rows?)\b`)
	reStats := regexp.MustCompile(`\b(stat|summary|mean|median|std|variance|min|max|count)\b`)
	reWrite := regexp.MustCompile(`\b(write|update|set|apply formula|formula)\b`)
	reStructure := regexp.MustCompile(`\b(structure|sheet|header|schema)\b`)
	reInsight := regexp.MustCompile(`\b(insight|driver|trend|change|variance|composition|mix|concentration|outlier|funnel)\b`)

	// Clarify sheet when multiple exist and no hint provided
	if len(sheetNames) > 1 {
		if _, ok := in.Hints["sheet"]; !ok {
			questions = append(questions, fmt.Sprintf("Which sheet should we analyze? Options: %v", sheetNames))
		}
	}

	// Default to structure discovery as step 1 for most intents
	if reStructure.MatchString(objective) || len(sheetNames) == 0 {
		recs = append(recs, Recommendation{
			ToolName:   "list_structure",
			Confidence: 0.75,
			Priority:   1,
			Rationale:  "Identify sheets, approximate dimensions, and headers to ground subsequent steps",
			SuggestedInputs: map[string]any{
				"path":          canonicalOr(in.Path, canonical),
				"metadata_only": false,
			},
		})
	}

	// Preview intent
	if rePreview.MatchString(objective) {
		recs = append(recs, Recommendation{
			ToolName:   "preview_sheet",
			Confidence: 0.7,
			Priority:   2,
			Rationale:  "Stream a bounded preview to verify headers and data types",
			SuggestedInputs: map[string]any{
				"path":  canonicalOr(in.Path, canonical),
				"sheet": hintOr(in.Hints, "sheet", firstOr(sheetNames)),
				"rows":  p.Limits.PreviewRowLimit,
			},
			Alternatives: []map[string]any{{
				"tool_name": "read_range",
				"reason":    "Use when a specific A1 range is known",
			}},
		})
	}

	// Search intent
	if reSearch.MatchString(objective) {
		recs = append(recs, Recommendation{
			ToolName:   "search_data",
			Confidence: 0.72,
			Priority:   2,
			Rationale:  "Find matching values or patterns with pagination and snapshots",
			SuggestedInputs: map[string]any{
				"path":  canonicalOr(in.Path, canonical),
				"sheet": hintOr(in.Hints, "sheet", firstOr(sheetNames)),
				"query": hintOr(in.Hints, "query", "<value or /regex/>")},
			Alternatives: []map[string]any{{
				"tool_name": "filter_data",
				"reason":    "Use structured predicates when column positions are known",
			}},
		})
	}

	// Filter intent
	if reFilter.MatchString(objective) {
		recs = append(recs, Recommendation{
			ToolName:   "filter_data",
			Confidence: 0.7,
			Priority:   2,
			Rationale:  "Filter rows using boolean predicates and paginate results",
			SuggestedInputs: map[string]any{
				"path":      canonicalOr(in.Path, canonical),
				"sheet":     hintOr(in.Hints, "sheet", firstOr(sheetNames)),
				"predicate": hintOr(in.Hints, "predicate", "$1 contains \"foo\" AND $4 > 0"),
			},
		})
	}

	// Statistics intent
	if reStats.MatchString(objective) {
		recs = append(recs, Recommendation{
			ToolName:   "compute_statistics",
			Confidence: 0.68,
			Priority:   3,
			Rationale:  "Compute summary statistics on a bounded range",
			SuggestedInputs: map[string]any{
				"path":   canonicalOr(in.Path, canonical),
				"sheet":  hintOr(in.Hints, "sheet", firstOr(sheetNames)),
				"range":  hintOr(in.Hints, "range", "A1:D100"),
				"reduce": hintOr(in.Hints, "reduce", "column"),
			},
		})
	}

	// Write/Formula intent
	if reWrite.MatchString(objective) {
		recs = append(recs, Recommendation{
			ToolName:   "apply_formula",
			Confidence: 0.6,
			Priority:   4,
			Rationale:  "Apply a formula across a bounded range using streaming writes",
			SuggestedInputs: map[string]any{
				"path":    canonicalOr(in.Path, canonical),
				"sheet":   hintOr(in.Hints, "sheet", firstOr(sheetNames)),
				"range":   hintOr(in.Hints, "range", "B2:D10"),
				"formula": hintOr(in.Hints, "formula", "=SUM(A1:B1)"),
			},
			Alternatives: []map[string]any{{"tool_name": "write_range", "reason": "When writing literal values"}},
		})
	}

	// Higher-level insights intent: keep planning-only and request clarifiers until downstream tools exist
	if reInsight.MatchString(objective) {
		// Ask clarifiers typical for multi-step insight workflows
		if _, ok := in.Hints["date_col"]; !ok {
			questions = append(questions, "Which column is the time dimension (date/time)? Provide 1-based index or name.")
		}
		if _, ok := in.Hints["measure"]; !ok {
			questions = append(questions, "Which column is the primary KPI/measure to analyze?")
		}
		// Recommend preview and statistics as groundwork
		recs = append(recs, Recommendation{
			ToolName:   "preview_sheet",
			Confidence: 0.62,
			Priority:   2,
			Rationale:  "Ground analysis with a bounded preview and header verification",
			SuggestedInputs: map[string]any{
				"path":  canonicalOr(in.Path, canonical),
				"sheet": hintOr(in.Hints, "sheet", firstOr(sheetNames)),
				"rows":  p.Limits.PreviewRowLimit,
			},
		})
	}

	// If nothing matched, suggest structure + preview as safe starting point
	if len(recs) == 0 {
		recs = append(recs,
			Recommendation{ToolName: "list_structure", Confidence: 0.7, Priority: 1, Rationale: "Identify sheets, dimensions, headers"},
			Recommendation{ToolName: "preview_sheet", Confidence: 0.6, Priority: 2, Rationale: "Verify header row and data types", SuggestedInputs: map[string]any{"path": canonicalOr(in.Path, canonical), "sheet": hintOr(in.Hints, "sheet", firstOr(sheetNames)), "rows": p.Limits.PreviewRowLimit}},
		)
	}

	// Normalize priorities and sort by priority asc then confidence desc
	for i := range recs {
		if recs[i].Priority <= 0 {
			recs[i].Priority = 5
		}
		if recs[i].Confidence < 0 {
			recs[i].Confidence = 0
		}
		if recs[i].Confidence > 1 {
			recs[i].Confidence = 1
		}
	}
	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].Priority == recs[j].Priority {
			return recs[i].Confidence > recs[j].Confidence
		}
		return recs[i].Priority < recs[j].Priority
	})

	// Current step summary
	out.CurrentStep = summarizeStep(objective, sheetNames, headersPreview)
	out.RecommendedTools = recs
	out.Questions = questions
	return out, nil
}

func summarizeStep(objective string, sheets, headers []string) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("objective=%q", objective))
	if len(sheets) > 0 {
		parts = append(parts, fmt.Sprintf("sheets=%d", len(sheets)))
		if len(headers) > 0 {
			max := len(headers)
			if max > 6 {
				max = 6
			}
			parts = append(parts, fmt.Sprintf("headers=%v", headers[:max]))
		}
	}
	return strings.Join(parts, " ")
}

func firstOr(xs []string) string {
	if len(xs) > 0 {
		return xs[0]
	}
	return ""
}

func hintOr(m map[string]string, key, def string) string {
	if m == nil {
		return def
	}
	if v, ok := m[key]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func canonicalOr(input, canonical string) string {
	if strings.TrimSpace(canonical) != "" {
		return canonical
	}
	return input
}
