package insights

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
	"github.com/xuri/excelize/v2"
)

// CompositionShiftInput computes share-of-total by group for two periods
// and highlights mix shifts in percentage points.
type CompositionShiftInput struct {
	Path           string  `json:"path" jsonschema_description:"Canonical Excel file path (allowed directories enforced)"`
	Sheet          string  `json:"sheet" jsonschema_description:"Sheet name"`
	Range          string  `json:"range" jsonschema_description:"A1-style range or defined name covering header + data"`
	DimIndex       int     `json:"dimension_index" jsonschema_description:"1-based column index within the range for the grouping dimension"`
	MeasureIndex   int     `json:"measure_index" jsonschema_description:"1-based column index within the range for the numeric measure"`
	TimeIndex      int     `json:"time_index,omitempty" jsonschema_description:"Optional 1-based column index within the range for the period/time column"`
	PeriodBaseline string  `json:"period_baseline,omitempty" jsonschema_description:"Optional baseline period value; if omitted, detected as earlier of last two periods"`
	PeriodCurrent  string  `json:"period_current,omitempty" jsonschema_description:"Optional current period value; if omitted, detected as latest of last two periods"`
	TopN           int     `json:"top_n,omitempty" jsonschema_description:"Top-N groups to return explicitly; remaining combined into 'Other' (default 5)"`
	MixThresholdPP float64 `json:"mix_threshold_pp,omitempty" jsonschema_description:"Highlight threshold in percentage points for mix shift (default 5)"`
	MaxCells       int     `json:"max_cells,omitempty" jsonschema_description:"Max cells to process (bounded by global limits)"`
}

type GroupMix struct {
	Name          string  `json:"name"`
	ShareBaseline float64 `json:"share_baseline"`
	ShareCurrent  float64 `json:"share_current"`
	PPChange      float64 `json:"pp_change"`
}

// CompositionShiftOutput reports period shares and Top-N movers.
type CompositionShiftOutput struct {
	Path           string     `json:"path"`
	Sheet          string     `json:"sheet"`
	Range          string     `json:"range"`
	PeriodBaseline string     `json:"period_baseline"`
	PeriodCurrent  string     `json:"period_current"`
	TopN           int        `json:"top_n"`
	MixThresholdPP float64    `json:"mix_threshold_pp"`
	Groups         []GroupMix `json:"groups"`
	OtherBaseline  float64    `json:"other_share_baseline"`
	OtherCurrent   float64    `json:"other_share_current"`
	Meta           struct {
		ProcessedRows  int  `json:"processed_rows"`
		ProcessedCells int  `json:"processed_cells"`
		MaxCells       int  `json:"max_cells"`
		Truncated      bool `json:"truncated"`
	} `json:"meta"`
}

// Composer executes composition/mix shift analysis using streaming reads.
type Composer struct {
	Limits runtime.Limits
	Mgr    *workbooks.Manager
}

func parseFloatStrict(s string) (float64, bool) {
	if strings.TrimSpace(s) == "" {
		return 0, false
	}
	// Strip common formatting
	clean := strings.Map(func(r rune) rune {
		switch r {
		case ',':
			return -1
		case '$':
			return -1
		default:
			return r
		}
	}, s)
	if strings.HasSuffix(strings.TrimSpace(clean), "%") {
		v := strings.TrimSpace(strings.TrimSuffix(clean, "%"))
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f / 100.0, true
		}
		return 0, false
	}
	if f, err := strconv.ParseFloat(clean, 64); err == nil {
		return f, true
	}
	return 0, false
}

func tryParseTime(s string) (time.Time, bool) {
	layouts := []string{time.RFC3339, "2006-01-02", "2006/01/02", "01/02/2006", "1/2/2006", "1/2/06", "2006-01-02 15:04:05"}
	for _, l := range layouts {
		if t, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// CompositionShift computes mix shift across two periods.
func (c *Composer) CompositionShift(ctx context.Context, in CompositionShiftInput) (CompositionShiftOutput, error) {
	var out CompositionShiftOutput
	out.Sheet = strings.TrimSpace(in.Sheet)
	out.TopN = in.TopN
	if out.TopN <= 0 || out.TopN > 10 {
		out.TopN = 5
	}
	out.MixThresholdPP = in.MixThresholdPP
	if out.MixThresholdPP <= 0 {
		out.MixThresholdPP = 5.0
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

	// Accumulators: period -> group -> sum
	acc := map[string]map[string]float64{}
	periodsSeen := map[string]struct{}{}

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
		if in.TimeIndex != 0 && (in.TimeIndex < 1 || in.TimeIndex > colCount) {
			return fmt.Errorf("invalid time_index; range has %d columns", colCount)
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

			// Extract fields within range
			dimAbs := x1 + (in.DimIndex - 1) - 1
			measAbs := x1 + (in.MeasureIndex - 1) - 1
			var timeAbs int
			if in.TimeIndex > 0 {
				timeAbs = x1 + (in.TimeIndex - 1) - 1
			}
			var dimVal string
			var measVal string
			var perVal string
			if dimAbs >= 0 && dimAbs < len(vals) {
				dimVal = strings.TrimSpace(vals[dimAbs])
			}
			if measAbs >= 0 && measAbs < len(vals) {
				measVal = strings.TrimSpace(vals[measAbs])
			}
			if timeAbs >= 0 && timeAbs < len(vals) {
				perVal = strings.TrimSpace(vals[timeAbs])
			}

			if dimVal == "" {
				dimVal = "(empty)"
			}
			mv, ok := parseFloatStrict(measVal)
			if !ok {
				continue
			}
			periodKey := "all"
			if in.TimeIndex > 0 {
				periodKey = perVal
				if periodKey == "" {
					periodKey = "(empty)"
				}
				periodsSeen[periodKey] = struct{}{}
			}
			m, ok := acc[periodKey]
			if !ok {
				m = map[string]float64{}
				acc[periodKey] = m
			}
			m[dimVal] += mv
			out.Meta.ProcessedRows++
		}
		out.Meta.ProcessedCells = cellsProcessed
		return r.Error()
	})
	if err != nil {
		return out, err
	}

	// Determine baseline/current periods
	if in.TimeIndex <= 0 {
		return out, fmt.Errorf("time_index is required to compute composition shift")
	}
	perBaseline := strings.TrimSpace(in.PeriodBaseline)
	perCurrent := strings.TrimSpace(in.PeriodCurrent)
	if perBaseline == "" || perCurrent == "" {
		// choose last two periods by time parse or lex order
		var keys []string
		for k := range periodsSeen {
			keys = append(keys, k)
		}
		if len(keys) < 2 {
			return out, fmt.Errorf("not enough distinct periods; need at least 2, found %d", len(keys))
		}
		sort.Slice(keys, func(i, j int) bool {
			ti, okI := tryParseTime(keys[i])
			tj, okJ := tryParseTime(keys[j])
			if okI && okJ {
				return ti.Before(tj)
			}
			// fallback lexicographic
			return keys[i] < keys[j]
		})
		perBaseline = keys[len(keys)-2]
		perCurrent = keys[len(keys)-1]
	}
	out.PeriodBaseline = perBaseline
	out.PeriodCurrent = perCurrent

	base := acc[perBaseline]
	curr := acc[perCurrent]
	if base == nil || curr == nil {
		return out, fmt.Errorf("missing period aggregates for baseline or current")
	}
	// Totals
	var totBase, totCurr float64
	for _, v := range base {
		totBase += v
	}
	for _, v := range curr {
		totCurr += v
	}
	if totBase == 0 || totCurr == 0 {
		return out, fmt.Errorf("zero totals for baseline or current period")
	}

	// Union of groups
	uniq := map[string]struct{}{}
	for k := range base {
		uniq[k] = struct{}{}
	}
	for k := range curr {
		uniq[k] = struct{}{}
	}
	rows := make([]GroupMix, 0, len(uniq))
	for g := range uniq {
		b := base[g] / totBase
		c := curr[g] / totCurr
		pp := (c - b) * 100.0
		rows = append(rows, GroupMix{Name: g, ShareBaseline: round3(b), ShareCurrent: round3(c), PPChange: round2(pp)})
	}
	// Sort by absolute pp change desc
	sort.Slice(rows, func(i, j int) bool {
		ai := math.Abs(rows[i].PPChange)
		aj := math.Abs(rows[j].PPChange)
		if ai == aj {
			return rows[i].Name < rows[j].Name
		}
		return ai > aj
	})

	keep := out.TopN
	if keep > len(rows) {
		keep = len(rows)
	}
	selected := rows[:keep]
	var selBase, selCurr float64
	for _, r := range selected {
		selBase += r.ShareBaseline
		selCurr += r.ShareCurrent
	}
	out.Groups = selected
	out.OtherBaseline = round3(1.0 - selBase)
	out.OtherCurrent = round3(1.0 - selCurr)
	return out, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
