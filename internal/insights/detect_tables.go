package insights

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
	"github.com/xuri/excelize/v2"
)

// DetectTablesInput controls multi-table detection within a sheet.
type DetectTablesInput struct {
    Path             string `json:"path" validate:"required,filepath_ext" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
    Sheet            string `json:"sheet" validate:"required" jsonschema_description:"Sheet name to scan"`
    MaxTables        int    `json:"max_tables,omitempty" validate:"omitempty,min=1,max=10" jsonschema_description:"Max number of table candidates to return (Top-K)"`
    MaxScanRows      int    `json:"max_scan_rows,omitempty" jsonschema_description:"Max number of rows to scan (bounded)"`
    MaxScanCols      int    `json:"max_scan_cols,omitempty" jsonschema_description:"Max number of columns to scan (bounded)"`
    HeaderRow        int    `json:"header_row,omitempty" jsonschema_description:"Optional 1-based header row hint; defaults to first non-empty row of each block"`
    HeaderSampleRows int    `json:"header_sample_rows,omitempty" validate:"omitempty,min=1,max=5" jsonschema_description:"Include top-N rows of each candidate for header sampling (default 2, max 5)"`
    HeaderSampleCols int    `json:"header_sample_cols,omitempty" validate:"omitempty,min=1,max=32" jsonschema_description:"Include leftmost N columns of header sample (default 12, max 32)"`
}

// TableCandidate describes a detected rectangular region that likely forms a table.
type TableCandidate struct {
	Range            string     `json:"range"`
	Header           []string   `json:"header,omitempty"`
	Confidence       float64    `json:"confidence"`
	Rows             int        `json:"rows"`
	Cols             int        `json:"cols"`
	HeaderSample     [][]string `json:"header_sample,omitempty"`
	HeaderSampleCols int        `json:"header_sample_cols_effective,omitempty"`
}

// DetectTablesOutput carries ranked candidates with basic scan metadata.
type DetectTablesOutput struct {
	Path       string           `json:"path"`
	Sheet      string           `json:"sheet"`
	Candidates []TableCandidate `json:"candidates"`
	Meta       struct {
		ScannedRows int  `json:"scanned_rows"`
		ScannedCols int  `json:"scanned_cols"`
		Truncated   bool `json:"truncated"`
	} `json:"meta"`
}

// Detector owns dependencies and effective limits for detection.
type Detector struct {
	Limits runtime.Limits
	Mgr    *workbooks.Manager
}

// DetectTables scans a sheet for multiple rectangular table regions using
// streaming reads and simple heuristics for header detection and block growth.
func (d *Detector) DetectTables(ctx context.Context, in DetectTablesInput) (DetectTablesOutput, error) {
	var out DetectTablesOutput
	out.Sheet = strings.TrimSpace(in.Sheet)

	id, canonical, err := d.Mgr.GetOrOpenByPath(ctx, in.Path)
	if err != nil {
		return out, err
	}
	out.Path = canonical

	maxTables := in.MaxTables
	if maxTables <= 0 || maxTables > 10 {
		maxTables = 5
	}

	// Establish scan bounds using used range and global limits.
	type grid struct {
		rows int
		cols int
		data [][]bool
		vals [][]string // optional values for header preview/type hints
	}

	var g grid

	err = d.Mgr.WithRead(id, func(f *excelize.File, _ int64) error {
		// Resolve sheet used range to cap scanning to active cells
		usedRows, usedCols := 0, 0
		if dim, derr := f.GetSheetDimension(out.Sheet); derr == nil && dim != "" {
			parts := strings.Split(dim, ":")
			if len(parts) == 2 {
				x1, y1, e1 := excelize.CellNameToCoordinates(parts[0])
				x2, y2, e2 := excelize.CellNameToCoordinates(parts[1])
				if e1 == nil && e2 == nil && x2 >= x1 && y2 >= y1 {
					usedCols = x2
					usedRows = y2
				}
			}
		}
		// Fallback for unknown dimensions
		if usedCols <= 0 {
			usedCols = 256
		}
		if usedRows <= 0 {
			usedRows = 200
		}

		scanRows := in.MaxScanRows
		if scanRows <= 0 || scanRows > usedRows {
			scanRows = usedRows
		}
		scanCols := in.MaxScanCols
		if scanCols <= 0 || scanCols > usedCols {
			// limit columns to a practical bound
			if usedCols > 256 {
				scanCols = 256
			} else {
				scanCols = usedCols
			}
		}
		// Respect global cell budget (≤ MaxCellsPerOp)
		budget := d.Limits.MaxCellsPerOp
		if budget <= 0 {
			budget = 10000
		}
		for scanRows*scanCols > budget {
			if scanRows > scanCols {
				scanRows--
			} else {
				scanCols--
			}
		}

		g.rows, g.cols = scanRows, scanCols
		g.data = make([][]bool, scanRows)
		g.vals = make([][]string, scanRows)
		for i := 0; i < scanRows; i++ {
			g.data[i] = make([]bool, scanCols)
			g.vals[i] = make([]string, scanCols)
		}

		r, rerr := f.Rows(out.Sheet)
		if rerr != nil {
			return rerr
		}
		defer r.Close()

		rowIdx := 0
		for r.Next() {
			rowIdx++
			if rowIdx > scanRows {
				break
			}
			rowVals, cerr := r.Columns()
			if cerr != nil {
				return cerr
			}
			// Fill presence up to scanCols
			for c := 0; c < scanCols && c < len(rowVals); c++ {
				v := strings.TrimSpace(rowVals[c])
				if v != "" {
					g.data[rowIdx-1][c] = true
					g.vals[rowIdx-1][c] = v
				}
			}
		}
		return r.Error()
	})
	if err != nil {
		return out, err
	}

	out.Meta.ScannedRows = g.rows
	out.Meta.ScannedCols = g.cols

	// Find connected components of non-empty cells (4-directional adjacency).
	type rect struct{ r1, c1, r2, c2 int }
	visited := make([][]bool, g.rows)
	for i := 0; i < g.rows; i++ {
		visited[i] = make([]bool, g.cols)
	}

	comps := make([]rect, 0, 8)
	var queue [][2]int
	enqueue := func(r, c int) { queue = append(queue, [2]int{r, c}) }
	dequeue := func() (int, int) { p := queue[0]; queue = queue[1:]; return p[0], p[1] }

	for r := 0; r < g.rows; r++ {
		for c := 0; c < g.cols; c++ {
			if !g.data[r][c] || visited[r][c] {
				continue
			}
			// BFS for this component
			visited[r][c] = true
			queue = queue[:0]
			enqueue(r, c)
			rr1, cc1, rr2, cc2 := r, c, r, c
			for len(queue) > 0 {
				cr, cc := dequeue()
				// Update bounds
				if cr < rr1 {
					rr1 = cr
				}
				if cr > rr2 {
					rr2 = cr
				}
				if cc < cc1 {
					cc1 = cc
				}
				if cc > cc2 {
					cc2 = cc
				}
				// 4-neighbors
				if cr > 0 && g.data[cr-1][cc] && !visited[cr-1][cc] {
					visited[cr-1][cc] = true
					enqueue(cr-1, cc)
				}
				if cr+1 < g.rows && g.data[cr+1][cc] && !visited[cr+1][cc] {
					visited[cr+1][cc] = true
					enqueue(cr+1, cc)
				}
				if cc > 0 && g.data[cr][cc-1] && !visited[cr][cc-1] {
					visited[cr][cc-1] = true
					enqueue(cr, cc-1)
				}
				if cc+1 < g.cols && g.data[cr][cc+1] && !visited[cr][cc+1] {
					visited[cr][cc+1] = true
					enqueue(cr, cc+1)
				}
			}
			// Reject tiny blobs (< 2x2)
			if (rr2-rr1+1) >= 2 && (cc2-cc1+1) >= 2 {
				comps = append(comps, rect{r1: rr1, c1: cc1, r2: rr2, c2: cc2})
			}
		}
	}

	// Build candidates with header heuristic and confidence ranking
	cands := make([]TableCandidate, 0, len(comps))
	// Bound header sample rows
	hsr := in.HeaderSampleRows
	if hsr <= 0 || hsr > 5 {
		hsr = 2
	}
	// Bound header sample columns
	hsc := in.HeaderSampleCols
	if hsc <= 0 || hsc > 32 {
		hsc = 12
	}
	for _, rc := range comps {
		// Header row: use rc.r1 or explicit hint if within bounds
		hdrRow := rc.r1
		if in.HeaderRow > 0 {
			if in.HeaderRow-1 >= rc.r1 && in.HeaderRow-1 <= rc.r2 {
				hdrRow = in.HeaderRow - 1
			}
		}
		// Extract header values
		header := make([]string, 0, rc.c2-rc.c1+1)
		for c := rc.c1; c <= rc.c2; c++ {
			header = append(header, g.vals[hdrRow][c])
		}
		hconf := headerConfidence(header)
		// Size confidence: prefer moderate-to-large coherent regions without dominating
		area := float64((rc.r2 - rc.r1 + 1) * (rc.c2 - rc.c1 + 1))
		maxArea := float64(g.rows * g.cols)
		sconf := 0.0
		if area > 1 && maxArea > 1 {
			sconf = math.Log2(area) / math.Log2(maxArea)
			if sconf < 0 {
				sconf = 0
			}
			if sconf > 1 {
				sconf = 1
			}
		}
		conf := 0.6*hconf + 0.4*sconf
		// Coordinates are 1-based; rc indices are 0-based rows/cols
		tl, _ := excelize.CoordinatesToCellName(rc.c1+1, rc.r1+1)
		br, _ := excelize.CoordinatesToCellName(rc.c2+1, rc.r2+1)
		// Build header sample from the top-left of the candidate block
		sampleRows := hsr
		if sampleRows > (rc.r2 - rc.r1 + 1) {
			sampleRows = (rc.r2 - rc.r1 + 1)
		}
		// Effective sample columns (pre-trim)
		maxCols := rc.c2 - rc.c1 + 1
		effCols := hsc
		if effCols > maxCols {
			effCols = maxCols
		}
		sample := make([][]string, 0, sampleRows)
		for rr := 0; rr < sampleRows; rr++ {
			rowVals := make([]string, 0, effCols)
			rIdx := rc.r1 + rr
			for cc := rc.c1; cc < rc.c1+effCols; cc++ {
				rowVals = append(rowVals, g.vals[rIdx][cc])
			}
			sample = append(sample, trimTrailingEmpties(rowVals))
		}

		cands = append(cands, TableCandidate{
			Range:            tl + ":" + br,
			Header:           trimTrailingEmpties(header),
			Confidence:       round3(conf),
			Rows:             rc.r2 - rc.r1 + 1,
			Cols:             rc.c2 - rc.c1 + 1,
			HeaderSample:     sample,
			HeaderSampleCols: effCols,
		})
	}

	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Confidence > cands[j].Confidence })
	if len(cands) > maxTables {
		out.Meta.Truncated = true
		out.Candidates = cands[:maxTables]
	} else {
		out.Candidates = cands
	}
	return out, nil
}

func headerConfidence(hdr []string) float64 {
	nonEmpty := 0
	numeric := 0
	uniq := map[string]struct{}{}
	for _, v := range hdr {
		s := strings.TrimSpace(v)
		if s == "" {
			continue
		}
		nonEmpty++
		if _, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64); err == nil {
			numeric++
		}
		key := strings.ToLower(s)
		uniq[key] = struct{}{}
	}
	if nonEmpty == 0 {
		return 0
	}
	uniqRatio := float64(len(uniq)) / float64(nonEmpty)
	numericRatio := float64(numeric) / float64(nonEmpty)
	// favor unique, mostly text-like headers
	return clamp01(0.5*uniqRatio + 0.5*(1.0-numericRatio))
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func round3(x float64) float64 {
	return math.Round(x*1000) / 1000
}

func trimTrailingEmpties(xs []string) []string {
	i := len(xs)
	for i > 0 {
		if strings.TrimSpace(xs[i-1]) != "" {
			break
		}
		i--
	}
	return xs[:i]
}
