package insights

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
	"github.com/xuri/excelize/v2"
)

// ProfileSchemaInput specifies the sheet/range to profile and sampling bounds.
type ProfileSchemaInput struct {
	Path          string `json:"path" jsonschema_description:"Absolute or allowed path to an Excel workbook"`
	Sheet         string `json:"sheet" jsonschema_description:"Sheet name to analyze"`
	Range         string `json:"range" jsonschema_description:"A1-style range or defined name for the table region"`
	MaxSampleRows int    `json:"max_sample_rows,omitempty" jsonschema_description:"Max non-header rows to sample per column (default 100)"`
}

// ColumnProfile summarizes inferred role, type, and quality for one column.
type ColumnProfile struct {
	Index       int      `json:"index"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Type        string   `json:"type"`
	Sampled     int      `json:"sampled"`
	MissingPct  float64  `json:"missing_pct"`
	UniqueRatio float64  `json:"unique_ratio"`
	Flags       []string `json:"flags,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

// ProfileSchemaOutput contains per-column profiles and clarifying questions.
type ProfileSchemaOutput struct {
	Path      string          `json:"path"`
	Sheet     string          `json:"sheet"`
	Range     string          `json:"range"`
	Columns   []ColumnProfile `json:"columns"`
	Questions []string        `json:"questions,omitempty"`
	Meta      struct {
		SampledRows int  `json:"sampled_rows"`
		MaxSample   int  `json:"max_sample"`
		Truncated   bool `json:"truncated"`
	} `json:"meta"`
}

// Profiler holds dependencies/limits for schema profiling.
type Profiler struct {
	Limits runtime.Limits
	Mgr    *workbooks.Manager
}

// ProfileSchema performs role inference and data quality checks on a bounded sample.
func (p *Profiler) ProfileSchema(ctx context.Context, in ProfileSchemaInput) (ProfileSchemaOutput, error) {
	var out ProfileSchemaOutput
	out.Sheet = strings.TrimSpace(in.Sheet)

	id, canonical, err := p.Mgr.GetOrOpenByPath(ctx, in.Path)
	if err != nil {
		return out, err
	}
	out.Path = canonical

	maxSample := in.MaxSampleRows
	if maxSample <= 0 || maxSample > 100 {
		maxSample = 100
	}

	err = p.Mgr.WithRead(id, func(f *excelize.File, _ int64) error {
		// Resolve and normalize the range text
		x1, y1, x2, y2, normalized, rerr := resolveRangeLocal(f, out.Sheet, in.Range)
		if rerr != nil {
			return rerr
		}
		out.Range = normalized

		colCount := x2 - x1 + 1
		if colCount <= 0 {
			return fmt.Errorf("empty range: no columns")
		}
		// Extract header names from first row of range
		headers := make([]string, colCount)
		rowsIter, rerr := f.Rows(out.Sheet)
		if rerr != nil {
			return rerr
		}
		defer rowsIter.Close()

		rowIdx := 0
		for rowsIter.Next() {
			rowIdx++
			vals, cerr := rowsIter.Columns()
			if cerr != nil {
				return cerr
			}
			if rowIdx == y1 {
				for i := 0; i < colCount; i++ {
					absCol := x1 + i - 1 // zero-based
					if absCol >= 0 && absCol < len(vals) {
						headers[i] = strings.TrimSpace(vals[absCol])
					}
				}
				break
			}
		}
		if err := rowsIter.Error(); err != nil {
			return err
		}

		// Prepare samplers for each column
		types := make([]typeCounter, colCount)
		uniqs := make([]map[string]int, colCount) // count duplicates
		miss := make([]int, colCount)
		total := 0

		for i := range uniqs {
			uniqs[i] = make(map[string]int)
		}

		rowsIter2, rerr2 := f.Rows(out.Sheet)
		if rerr2 != nil {
			return rerr2
		}
		defer rowsIter2.Close()

		rowIdx = 0
		sampledRows := 0
		for rowsIter2.Next() {
			rowIdx++
			if rowIdx <= y1 { // skip header row
				continue
			}
			if rowIdx > y2 {
				break
			}
			vals, cerr := rowsIter2.Columns()
			if cerr != nil {
				return cerr
			}

			// stop condition: sampledRows reached cap
			if sampledRows >= maxSample {
				break
			}
			sampledRows++
			total++
			for i := 0; i < colCount; i++ {
				absCol := x1 + i - 1
				var cell string
				if absCol >= 0 && absCol < len(vals) {
					cell = strings.TrimSpace(vals[absCol])
				}
				if cell == "" {
					miss[i]++
					continue
				}
				types[i].observe(cell)
				uniqs[i][cell]++
			}
		}
		if err := rowsIter2.Error(); err != nil {
			return err
		}

		out.Meta.SampledRows = sampledRows
		out.Meta.MaxSample = maxSample
		out.Meta.Truncated = (y1+sampledRows < y2)

		// Build column profiles with role inference and quality checks
		profiles := make([]ColumnProfile, colCount)
		candidateIDs := []int{}
		candidateTimes := []int{}
		for i := 0; i < colCount; i++ {
			name := strings.TrimSpace(headers[i])
			cp := ColumnProfile{Index: i + 1, Name: name}
			nonEmpty := sampledRows - miss[i]
			if sampledRows > 0 {
				cp.MissingPct = round2(100.0 * float64(miss[i]) / float64(sampledRows))
			}
			uniqNonEmpty := 0
			for k := range uniqs[i] {
				if strings.TrimSpace(k) != "" {
					uniqNonEmpty++
				}
			}
			if nonEmpty > 0 {
				cp.UniqueRatio = round3(float64(uniqNonEmpty) / float64(nonEmpty))
			}
			// Type inference
			cp.Type = types[i].dominantType()
			cp.Sampled = sampledRows

			// Role inference rules
			role := inferRole(name, types[i], cp.UniqueRatio, nonEmpty)
			cp.Role = role
			// Candidate collections for ambiguity questions
			if role == "time" {
				candidateTimes = append(candidateTimes, i)
			}
			if role == "id" {
				candidateIDs = append(candidateIDs, i)
			}

			// Quality checks
			cp.Flags, cp.Warnings = qualityChecks(name, types[i], uniqs[i], sampledRows)

			profiles[i] = cp
		}

		// Clarifying questions for ambiguity
		var questions []string
		if len(candidateIDs) > 1 {
			cols := columnNames(candidateIDs, headers)
			questions = append(questions, fmt.Sprintf("Multiple ID-like columns detected (%s). Which one is the primary ID?", strings.Join(cols, ", ")))
		}
		if len(candidateTimes) > 1 {
			cols := columnNames(candidateTimes, headers)
			questions = append(questions, fmt.Sprintf("Multiple time-like columns detected (%s). Which column is the time dimension?", strings.Join(cols, ", ")))
		}
		// Ask for primary measure when none clearly numeric
		numericCols := 0
		for _, pr := range profiles {
			if pr.Role == "measure" || pr.Role == "target" {
				numericCols++
			}
		}
		if numericCols == 0 {
			questions = append(questions, "Which column is the primary KPI/measure to analyze?")
		}

		// Stable output ordering: preserve input order but ensure any target/id/time highlighted first in summaries
		out.Columns = profiles
		out.Questions = questions
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// Helpers and inference utilities

// resolveRangeLocal parses an A1-style or defined name range relative to a sheet.
// Returns x1,y1,x2,y2 and normalized textual range without sheet qualifier.
func resolveRangeLocal(f *excelize.File, sheet, input string) (int, int, int, int, string, error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return 0, 0, 0, 0, "", fmt.Errorf("invalid range: empty")
	}
	if strings.Contains(in, "!") {
		parts := strings.SplitN(in, "!", 2)
		if len(parts) == 2 {
			s := strings.Trim(parts[0], "'")
			if s != "" && !strings.EqualFold(s, sheet) {
				return 0, 0, 0, 0, "", fmt.Errorf("invalid range: sheet mismatch")
			}
			in = parts[1]
		}
	}
	if strings.Contains(in, ":") {
		parts := strings.Split(in, ":")
		if len(parts) != 2 {
			return 0, 0, 0, 0, "", fmt.Errorf("invalid range: %s", input)
		}
		x1, y1, err1 := excelize.CellNameToCoordinates(parts[0])
		x2, y2, err2 := excelize.CellNameToCoordinates(parts[1])
		if err1 != nil || err2 != nil {
			return 0, 0, 0, 0, "", fmt.Errorf("invalid range coordinates")
		}
		if x2 < x1 {
			x1, x2 = x2, x1
		}
		if y2 < y1 {
			y1, y2 = y2, y1
		}
		l, _ := excelize.CoordinatesToCellName(x1, y1)
		r, _ := excelize.CoordinatesToCellName(x2, y2)
		return x1, y1, x2, y2, l + ":" + r, nil
	}
	// Named range: find a match for this sheet
	names := f.GetDefinedName()
	for _, dn := range names {
		if dn.Name == input {
			ref := strings.TrimPrefix(dn.RefersTo, "=")
			if strings.Contains(ref, "!") {
				parts := strings.SplitN(ref, "!", 2)
				if len(parts) == 2 {
					s := strings.Trim(parts[0], "'")
					if s != "" && !strings.EqualFold(s, sheet) {
						continue
					}
					ref = parts[1]
				}
			}
			ref = strings.ReplaceAll(ref, "$", "")
			if strings.Contains(ref, ":") {
				p := strings.Split(ref, ":")
				if len(p) != 2 {
					continue
				}
				x1, y1, e1 := excelize.CellNameToCoordinates(p[0])
				x2, y2, e2 := excelize.CellNameToCoordinates(p[1])
				if e1 == nil && e2 == nil {
					if x2 < x1 {
						x1, x2 = x2, x1
					}
					if y2 < y1 {
						y1, y2 = y2, y1
					}
					l, _ := excelize.CoordinatesToCellName(x1, y1)
					r, _ := excelize.CoordinatesToCellName(x2, y2)
					return x1, y1, x2, y2, l + ":" + r, nil
				}
			}
		}
	}
	return 0, 0, 0, 0, "", fmt.Errorf("invalid range: %s", input)
}

// typeCounter tracks observed value categories for a column.
type typeCounter struct {
	numCount     int
	intCount     int
	percentCount int
	textCount    int
	dateCount    int
	boolCount    int
	negCount     int
	gt100Pct     int
	// total non-empty observations recorded here is sum of above except neg/gt100 which are sub-counters
}

func (t *typeCounter) observe(s string) {
	// detect boolean
	low := strings.ToLower(s)
	if low == "true" || low == "false" || low == "yes" || low == "no" {
		t.boolCount++
		return
	}
	// percent like
	if strings.HasSuffix(low, "%") {
		v := strings.TrimSpace(strings.TrimSuffix(low, "%"))
		v = strings.ReplaceAll(v, ",", "")
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			t.percentCount++
			if f > 100 {
				t.gt100Pct++
			}
			if f < 0 {
				t.negCount++
			}
			return
		}
	}
	// numeric (strip commas and $)
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
	if f, err := strconv.ParseFloat(clean, 64); err == nil {
		t.numCount++
		if math.Trunc(f) == f {
			t.intCount++
		}
		if f < 0 {
			t.negCount++
		}
		return
	}
	// date/time detection with a few common layouts
	dateLayouts := []string{
		time.RFC3339, "2006-01-02", "01/02/2006", "2006/01/02", "1/2/2006", "1/2/06", "2006-01-02 15:04:05",
	}
	for _, layout := range dateLayouts {
		if _, err := time.Parse(layout, s); err == nil {
			t.dateCount++
			return
		}
	}
	// fallback to text
	t.textCount++
}

func (t *typeCounter) dominantType() string {
	// choose the category with the highest count among non-empty
	max := 0
	typeName := "unknown"
	set := []struct {
		n int
		k string
	}{
		{t.percentCount, "percent"},
		{t.numCount, "numeric"},
		{t.intCount, "integer"},
		{t.dateCount, "date"},
		{t.boolCount, "boolean"},
		{t.textCount, "text"},
	}
	for _, s := range set {
		if s.n > max {
			max = s.n
			typeName = s.k
		}
	}
	// Mixed type detection heuristic
	counts := []int{t.percentCount, t.numCount, t.dateCount, t.boolCount, t.textCount}
	sort.Slice(counts, func(i, j int) bool { return counts[i] > counts[j] })
	if len(counts) >= 2 && counts[0] > 0 && counts[1] > 0 {
		// if second best is at least 20% of best, mark as mixed
		if float64(counts[1]) >= 0.2*float64(counts[0]) {
			return "mixed"
		}
	}
	return typeName
}

func inferRole(name string, t typeCounter, uniqueRatio float64, nonEmpty int) string {
	low := strings.ToLower(strings.TrimSpace(name))
	// name hints for time and id/target
	if reDate.MatchString(low) || strings.Contains(low, "date") || strings.Contains(low, "time") || strings.Contains(low, "month") || strings.Contains(low, "year") {
		if t.dateCount > 0 {
			return "time"
		}
	}
	if strings.Contains(low, "id") || strings.Contains(low, "uuid") || strings.Contains(low, "key") {
		if uniqueRatio >= 0.9 && nonEmpty > 0 {
			return "id"
		}
	}
	if strings.Contains(low, "target") || strings.Contains(low, "plan") || strings.Contains(low, "budget") || strings.Contains(low, "goal") || strings.Contains(low, "quota") {
		if t.percentCount > 0 || t.numCount > 0 || t.intCount > 0 {
			return "target"
		}
	}
	// time by content dominance
	if t.dateCount > 0 && t.dateCount >= t.numCount && t.dateCount >= t.textCount {
		return "time"
	}
	// id by uniqueness even without name clue
	if uniqueRatio >= 0.95 && nonEmpty > 0 && t.textCount >= t.numCount {
		return "id"
	}
	// measure vs dimension
	if (t.numCount + t.intCount + t.percentCount) > (t.textCount + t.boolCount) {
		return "measure"
	}
	return "dimension"
}

var reDate = regexp.MustCompile(`\b(ymd|y/m/d|d/m/y|m/d/y|q\d|qtr|quarter|week|wk)\b`)

func qualityChecks(name string, t typeCounter, uniq map[string]int, sampledRows int) (flags []string, warnings []string) {
	low := strings.ToLower(name)
	// Nonnegative expectations by name
	if containsAny(low, []string{"count", "qty", "quantity", "units", "views", "clicks", "impressions", "visits", "orders", "transactions", "installs"}) {
		flags = append(flags, "nonnegative_expected")
		if t.negCount > 0 {
			warnings = append(warnings, fmt.Sprintf("negative values in nonnegative field: %d", t.negCount))
		}
	}
	// Percent bounds
	if t.percentCount > 0 && t.gt100Pct > 0 {
		warnings = append(warnings, fmt.Sprintf(">100%% values in percent-like field: %d", t.gt100Pct))
	}
	// Mixed types
	// approximate: more than one strong category present
	strongCats := 0
	if t.numCount > 0 {
		strongCats++
	}
	if t.percentCount > 0 {
		strongCats++
	}
	if t.textCount > 0 {
		strongCats++
	}
	if t.dateCount > 0 {
		strongCats++
	}
	if strongCats >= 2 {
		warnings = append(warnings, "mixed types observed")
	}
	// Duplicate IDs if likely ID-like
	if strings.Contains(low, "id") || strings.Contains(low, "uuid") || strings.Contains(low, "key") {
		dups := 0
		for _, c := range uniq {
			if c > 1 {
				dups += c - 1
			}
		}
		if dups > 0 {
			warnings = append(warnings, fmt.Sprintf("duplicate IDs detected: %d", dups))
		}
	}
	return flags, warnings
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func columnNames(indices []int, headers []string) []string {
	labels := make([]string, 0, len(indices))
	for _, i := range indices {
		name := headers[i]
		if strings.TrimSpace(name) == "" {
			labels = append(labels, fmt.Sprintf("$%d", i+1))
		} else {
			labels = append(labels, fmt.Sprintf("%s($%d)", name, i+1))
		}
	}
	return labels
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }
