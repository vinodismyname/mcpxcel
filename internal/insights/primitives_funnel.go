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

// FunnelAnalysisInput computes stage and cumulative conversion across ordered stages.
type FunnelAnalysisInput struct {
    Path         string `json:"path" validate:"required,filepath_ext" jsonschema_description:"Canonical Excel file path (allowed directories enforced)"`
    Sheet        string `json:"sheet" validate:"required" jsonschema_description:"Sheet name"`
    Range        string `json:"range" validate:"required,a1orname" jsonschema_description:"A1-style range or defined name covering header + data"`
    StageIndices []int  `json:"stage_indices,omitempty" validate:"dive,min=1" jsonschema_description:"Ordered 1-based column indices within the range for funnel stages; if omitted, detect from header names"`
    MaxCells     int    `json:"max_cells,omitempty" validate:"omitempty,min=1" jsonschema_description:"Max cells to process (bounded by global limits)"`
}

type StageMetric struct {
	Name           string  `json:"name"`
	Total          float64 `json:"total"`
	StepConversion float64 `json:"step_conversion"`
	CumulativeConv float64 `json:"cumulative_conversion"`
}

type FunnelAnalysisOutput struct {
	Path       string        `json:"path"`
	Sheet      string        `json:"sheet"`
	Range      string        `json:"range"`
	StageNames []string      `json:"stage_names"`
	Stages     []StageMetric `json:"stages"`
	Bottleneck string        `json:"bottleneck_stage"`
	Meta       struct {
		ProcessedRows  int  `json:"processed_rows"`
		ProcessedCells int  `json:"processed_cells"`
		MaxCells       int  `json:"max_cells"`
		Truncated      bool `json:"truncated"`
	} `json:"meta"`
}

// Funneler performs funnel analysis via streaming reads.
type Funneler struct {
	Limits runtime.Limits
	Mgr    *workbooks.Manager
}

var stageRe = regexp.MustCompile(`(?i)^(impressions|views|visits|sessions|clicks|adds?[_\s-]?to[_\s-]?cart|cart|checkout|payments?|purchases?|orders?)$`)

// FunnelAnalysis computes total counts per stage and conversion rates.
func (f *Funneler) FunnelAnalysis(ctx context.Context, in FunnelAnalysisInput) (FunnelAnalysisOutput, error) {
	var out FunnelAnalysisOutput
	out.Sheet = strings.TrimSpace(in.Sheet)

	id, canonical, err := f.Mgr.GetOrOpenByPath(ctx, in.Path)
	if err != nil {
		return out, err
	}
	out.Path = canonical

	maxCells := in.MaxCells
	if maxCells <= 0 || maxCells > f.Limits.MaxCellsPerOp {
		maxCells = f.Limits.MaxCellsPerOp
	}
	out.Meta.MaxCells = maxCells

	// Stage indices within range; detect when empty
	var stageIdx []int

	err = f.Mgr.WithRead(id, func(ef *excelize.File, _ int64) error {
		x1, y1, x2, y2, normalized, rerr := resolveRangeLocal(ef, out.Sheet, in.Range)
		if rerr != nil {
			return rerr
		}
		out.Range = normalized
		colCount := x2 - x1 + 1
		// Build header names
		headers := make([]string, colCount)
		r, rerr := ef.Rows(out.Sheet)
		if rerr != nil {
			return rerr
		}
		defer r.Close()
		rowIdx := 0
		for r.Next() {
			rowIdx++
			vals, cerr := r.Columns()
			if cerr != nil {
				return cerr
			}
			if rowIdx == y1 { // header row
				for i := 0; i < colCount; i++ {
					abs := x1 + i - 1
					if abs >= 0 && abs < len(vals) {
						headers[i] = strings.TrimSpace(vals[abs])
					}
				}
				break
			}
		}
		if err := r.Error(); err != nil {
			return err
		}
		if len(in.StageIndices) > 0 {
			// Validate provided indices
			for _, idx := range in.StageIndices {
				if idx < 1 || idx > colCount {
					return fmt.Errorf("invalid stage index %d; range has %d columns", idx, colCount)
				}
			}
			stageIdx = append(stageIdx, in.StageIndices...)
		} else {
			// Detect by header name patterns; maintain left-to-right order
			for i, h := range headers {
				if stageRe.MatchString(strings.ToLower(strings.TrimSpace(h))) {
					stageIdx = append(stageIdx, i+1)
				}
			}
			if len(stageIdx) < 2 {
				return fmt.Errorf("unable to detect at least 2 funnel stages by header; specify stage_indices")
			}
		}

		// Accumulate totals per stage
		totals := make([]float64, len(stageIdx))
		// Iterate data rows
		r2, er2 := ef.Rows(out.Sheet)
		if er2 != nil {
			return er2
		}
		defer r2.Close()
		cells := 0
		row := 0
		for r2.Next() {
			row++
			if row <= y1 {
				continue
			}
			if row > y2 {
				break
			}
			vals, cerr := r2.Columns()
			if cerr != nil {
				return cerr
			}
			cells += minInt(len(vals), colCount)
			if cells > maxCells {
				out.Meta.Truncated = true
				break
			}
			for i, idx := range stageIdx {
				abs := x1 + (idx - 1) - 1
				if abs >= 0 && abs < len(vals) {
					if v, ok := parseFloatStrict(vals[abs]); ok {
						totals[i] += v
					}
				}
			}
			out.Meta.ProcessedRows++
		}
		out.Meta.ProcessedCells = cells

		// Stage names from headers
		for _, idx := range stageIdx {
			name := headers[idx-1]
			if strings.TrimSpace(name) == "" {
				name = fmt.Sprintf("$%d", idx)
			}
			out.StageNames = append(out.StageNames, name)
		}

		// Compute conversions
		stages := make([]StageMetric, len(stageIdx))
		var first float64
		if len(totals) > 0 {
			first = totals[0]
		}
		for i := range totals {
			step := 0.0
			if i == 0 {
				step = 1.0
			} else if totals[i-1] > 0 {
				step = totals[i] / totals[i-1]
			}
			cum := 0.0
			if first > 0 {
				cum = totals[i] / first
			}
			stages[i] = StageMetric{
				Name:           out.StageNames[i],
				Total:          round3(totals[i]),
				StepConversion: round3(step),
				CumulativeConv: round3(cum),
			}
		}
		out.Stages = stages
		// Bottleneck: minimal step conversion among transitions
		type bi struct {
			name string
			v    float64
		}
		var pairs []bi
		for i := 1; i < len(stages); i++ {
			pairs = append(pairs, bi{name: stages[i].Name, v: stages[i].StepConversion})
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].v < pairs[j].v })
		if len(pairs) > 0 {
			out.Bottleneck = pairs[0].name
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// round3 provided in detect_tables.go; reuse within package
