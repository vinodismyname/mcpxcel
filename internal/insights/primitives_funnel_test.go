package insights

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
	"github.com/xuri/excelize/v2"
)

func createFunnelWorkbook(t *testing.T) (string, string) {
	t.Helper()
	f := excelize.NewFile()
	sh := "Funnel"
	f.SetSheetName("Sheet1", sh)
	// Header detected by pattern: Impressions, Clicks, Orders
	require.NoError(t, f.SetSheetRow(sh, "A1", &[]string{"Impressions", "Clicks", "Orders"}))
	require.NoError(t, f.SetSheetRow(sh, "A2", &[]string{"1000", "100", "10"}))
	require.NoError(t, f.SetSheetRow(sh, "A3", &[]string{"500", "50", "5"}))

	dir := t.TempDir()
	path := filepath.Join(dir, "funnel.xlsx")
	require.NoError(t, f.SaveAs(path))
	require.NoError(t, f.Close())
	return path, sh
}

func TestFunnelAnalysis_DetectsStagesAndConversions(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	mgr := workbooks.NewManager(0, 0, nil, nil)
	f := &Funneler{Limits: limits, Mgr: mgr}

	path, sh := createFunnelWorkbook(t)
	out, err := f.FunnelAnalysis(context.Background(), FunnelAnalysisInput{Path: path, Sheet: sh, Range: "A1:C3"})
	require.NoError(t, err)
	require.Equal(t, []string{"Impressions", "Clicks", "Orders"}, out.StageNames)
	require.Equal(t, 3, len(out.Stages))
	// Step conversions should be roughly 0.1 from Impressions->Clicks and 0.1 Clicks->Orders
	require.InDelta(t, 0.1, out.Stages[1].StepConversion, 0.01)
	require.InDelta(t, 0.1, out.Stages[2].StepConversion, 0.01)
	// Bottleneck is the smallest step; with equal steps either Clicks or Orders is acceptable
	if out.Bottleneck != "Clicks" && out.Bottleneck != "Orders" {
		t.Fatalf("unexpected bottleneck: %s", out.Bottleneck)
	}
}
