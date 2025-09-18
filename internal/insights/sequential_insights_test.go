package insights

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
	"github.com/xuri/excelize/v2"
)

// helper creates a small workbook on disk with two sheets and simple headers.
func createTestWorkbook(t *testing.T) string {
	t.Helper()
	f := excelize.NewFile()
	// Default sheet is Sheet1; ensure a second sheet exists
	_, err := f.NewSheet("Second")
	require.NoError(t, err)
	// Populate headers in both sheets
	require.NoError(t, f.SetSheetRow("Sheet1", "A1", &[]string{"col1", "col2", "col3"}))
	require.NoError(t, f.SetSheetRow("Second", "A1", &[]string{"a", "b"}))

	dir := t.TempDir()
	path := filepath.Join(dir, "wb.xlsx")
	require.NoError(t, f.SaveAs(path))
	require.NoError(t, f.Close())
	return path
}

func TestPlanner_EmptyObjective_AsksForObjective(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	mgr := workbooks.NewManager(0, 0, nil, nil)
	p := &Planner{Limits: limits, Mgr: mgr}

	out, err := p.Plan(context.Background(), SequentialInsightsInput{})
	require.NoError(t, err)
	require.Contains(t, out.CurrentStep, "clarify-objective")
	require.NotEmpty(t, out.Questions)
	// list_structure should be recommended first when objective unknown
	require.NotEmpty(t, out.RecommendedTools)
	require.Equal(t, "list_structure", out.RecommendedTools[0].ToolName)
}

func TestPlanner_SearchIntent_RecommendsSearch(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	mgr := workbooks.NewManager(0, 0, nil, nil)
	p := &Planner{Limits: limits, Mgr: mgr}

	out, err := p.Plan(context.Background(), SequentialInsightsInput{Objective: "search for value in sheet"})
	require.NoError(t, err)
	// Ensure search_data appears among recommendations
	found := false
	for _, r := range out.RecommendedTools {
		if r.ToolName == "search_data" {
			found = true
			break
		}
	}
	require.True(t, found, "expected search_data recommendation")
}

func TestPlanner_InsightIntent_AsksClarifiers(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	mgr := workbooks.NewManager(0, 0, nil, nil)
	p := &Planner{Limits: limits, Mgr: mgr}

	out, err := p.Plan(context.Background(), SequentialInsightsInput{Objective: "identify drivers of change and insights"})
	require.NoError(t, err)
	// Should ask for date_col and measure
	joined := strings.Join(out.Questions, " ")
	require.Contains(t, joined, "time dimension")
	require.Contains(t, joined, "primary KPI")
}

func TestPlanner_WithWorkbook_MultipleSheets_AsksWhichSheet(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	mgr := workbooks.NewManager(0, 0, nil, nil)
	p := &Planner{Limits: limits, Mgr: mgr}

	path := createTestWorkbook(t)
	// Sanity ensure file exists
	_, err := os.Stat(path)
	require.NoError(t, err)

	out, err := p.Plan(context.Background(), SequentialInsightsInput{Objective: "preview first rows", Path: path})
	require.NoError(t, err)
	// Because multiple sheets exist and no hint, expect a sheet question
	hasSheetQ := false
	for _, q := range out.Questions {
		if strings.Contains(q, "Which sheet") {
			hasSheetQ = true
			break
		}
	}
	require.True(t, hasSheetQ, "expected question asking which sheet to analyze")
}
