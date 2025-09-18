package insights

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
	"github.com/xuri/excelize/v2"
)

// ConcentrationMetricsInput computes Top-N share and HHI over a grouping dimension.
type ConcentrationMetricsInput struct {
	Path         string `json:"path" jsonschema_description:"Canonical Excel file path (allowed directories enforced)"`
	Sheet        string `json:"sheet" jsonschema_description:"Sheet name"`
	Range        string `json:"range" jsonschema_description:"A1-style range or defined name covering header + data"`
	DimIndex     int    `json:"dimension_index" jsonschema_description:"1-based column index within the range for the grouping dimension"`
	MeasureIndex int    `json:"measure_index" jsonschema_description:"1-based column index within the range for the numeric measure"`
	TopN         int    `json:"top_n,omitempty" jsonschema_description:"Top-N groups to report and to compute Top-N share (default 5)"`
	MaxCells     int    `json:"max_cells,omitempty" jsonschema_description:"Max cells to process (bounded by global limits)"`
}

type GroupShare struct {
	Name  string  `json:"name"`
	Share float64 `json:"share"`
	Total float64 `json:"total"`
}

// ConcentrationMetricsOutput provides HHI banding and Top-N share with breakdown.
type ConcentrationMetricsOutput struct {
	Path       string       `json:"path"`
	Sheet      string       `json:"sheet"`
	Range      string       `json:"range"`
	TopN       int          `json:"top_n"`
	Groups     []GroupShare `json:"groups"`
	OtherShare float64      `json:"other_share"`
	HHI        float64      `json:"hhi"`
	Band       string       `json:"band"`
	Meta       struct {
		ProcessedRows  int  `json:"processed_rows"`
		ProcessedCells int  `json:"processed_cells"`
		MaxCells       int  `json:"max_cells"`
		Truncated      bool `json:"truncated"`
	} `json:"meta"`
}

// Concentrator executes Top-N and HHI concentration analysis via streaming.
type Concentrator struct {
	Limits runtime.Limits
	Mgr    *workbooks.Manager
}

// ConcentrationMetrics computes share distribution and HHI.
func (c *Concentrator) ConcentrationMetrics(ctx context.Context, in ConcentrationMetricsInput) (ConcentrationMetricsOutput, error) {
	var out ConcentrationMetricsOutput
	out.Sheet = strings.TrimSpace(in.Sheet)
	out.TopN = in.TopN
	if out.TopN <= 0 || out.TopN > 10 {
		out.TopN = 5
	}

	id, canonical, err := c.Mgr.GetOrOpenByPath(ctx, in.Path)
	if err != nil {
		return out, err
	}
	out.Path = canonical

	maxCells := in.MaxCells
	if maxCells <= 0 || maxCells > c.Limits.MaxCellsPerOp {
		maxCells = c.Limits.MaxCellsPerOp
	}
	out.Meta.MaxCells = maxCells

	// Accumulate totals by group
	acc := map[string]float64{}

	err = c.Mgr.WithRead(id, func(f *excelize.File, _ int64) error {
		x1, y1, x2, y2, normalized, rerr := resolveRangeLocal(f, out.Sheet, in.Range)
		if rerr != nil {
			return rerr
		}
		out.Range = normalized
		colCount := x2 - x1 + 1
		if in.DimIndex < 1 || in.DimIndex > colCount || in.MeasureIndex < 1 || in.MeasureIndex > colCount {
			return fmt.Errorf("invalid dimension_index or measure_index; range has %d columns", colCount)
		}

		r, rerr := f.Rows(out.Sheet)
		if rerr != nil {
			return rerr
		}
		defer r.Close()

		cellsProcessed := 0
		rowIdx := 0
		for r.Next() {
			rowIdx++
			if rowIdx <= y1 { // skip header row
				continue
			}
			if rowIdx > y2 {
				break
			}
			vals, cerr := r.Columns()
			if cerr != nil {
				return cerr
			}
			cellsProcessed += minInt(len(vals), colCount)
			if cellsProcessed > maxCells {
				out.Meta.Truncated = true
				break
			}

			dimAbs := x1 + (in.DimIndex - 1) - 1
			measAbs := x1 + (in.MeasureIndex - 1) - 1
			var dimVal, measVal string
			if dimAbs >= 0 && dimAbs < len(vals) {
				dimVal = strings.TrimSpace(vals[dimAbs])
			}
			if measAbs >= 0 && measAbs < len(vals) {
				measVal = strings.TrimSpace(vals[measAbs])
			}
			if dimVal == "" {
				dimVal = "(empty)"
			}
			mv, ok := parseFloatStrict(measVal)
			if !ok {
				continue
			}
			acc[dimVal] += mv
			out.Meta.ProcessedRows++
		}
		out.Meta.ProcessedCells = cellsProcessed
		return r.Error()
	})
	if err != nil {
		return out, err
	}

	// Compute shares
	var total float64
	for _, v := range acc {
		total += v
	}
	if total == 0 {
		return out, fmt.Errorf("zero total measure; cannot compute shares")
	}

	type kv struct {
		k string
		v float64
	}
	arr := make([]kv, 0, len(acc))
	for k, v := range acc {
		arr = append(arr, kv{k: k, v: v})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].v > arr[j].v })

	keep := out.TopN
	if keep > len(arr) {
		keep = len(arr)
	}
	var topShare float64
	for i := 0; i < keep; i++ {
		sh := arr[i].v / total
		out.Groups = append(out.Groups, GroupShare{Name: arr[i].k, Share: round3(sh), Total: arr[i].v})
		topShare += sh
	}
	out.OtherShare = round3(1.0 - topShare)

	// HHI: sum of squared shares over all groups
	var hhi float64
	for _, kvp := range arr {
		sh := kvp.v / total
		hhi += sh * sh
	}
	out.HHI = round3(hhi)
	// Bands based on common antitrust thresholds
	switch {
	case hhi < 0.15:
		out.Band = "unconcentrated"
	case hhi < 0.25:
		out.Band = "moderately_concentrated"
	default:
		out.Band = "highly_concentrated"
	}
	return out, nil
}

// round3 provided in detect_tables.go; reuse within package
