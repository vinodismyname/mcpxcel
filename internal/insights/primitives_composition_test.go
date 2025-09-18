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

func createMixWorkbook(t *testing.T) (string, string) {
	t.Helper()
	f := excelize.NewFile()
	sh := "Mix"
	f.SetSheetName("Sheet1", sh)
	// Header: Product, Month, Revenue
	require.NoError(t, f.SetSheetRow(sh, "A1", &[]string{"Product", "Month", "Revenue"}))
	// Baseline 2024-01
	require.NoError(t, f.SetSheetRow(sh, "A2", &[]string{"A", "2024-01-01", "100"}))
	require.NoError(t, f.SetSheetRow(sh, "A3", &[]string{"B", "2024-01-01", "100"}))
	// Current 2024-02
	require.NoError(t, f.SetSheetRow(sh, "A4", &[]string{"A", "2024-02-01", "200"}))
	require.NoError(t, f.SetSheetRow(sh, "A5", &[]string{"B", "2024-02-01", "100"}))

	dir := t.TempDir()
	path := filepath.Join(dir, "mix.xlsx")
	require.NoError(t, f.SaveAs(path))
	require.NoError(t, f.Close())
	return path, sh
}

func TestCompositionShift_ComputesPPChanges(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	mgr := workbooks.NewManager(0, 0, nil, nil)
	c := &Composer{Limits: limits, Mgr: mgr}

	path, sh := createMixWorkbook(t)
	in := CompositionShiftInput{
		Path: path, Sheet: sh, Range: "A1:C5",
		DimIndex: 1, MeasureIndex: 3, TimeIndex: 2,
		TopN: 5,
	}
	out, err := c.CompositionShift(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, 2, len(out.Groups))
	// Expect A increased share (positive pp), B decreased (negative)
	var aPP, bPP float64
	for _, g := range out.Groups {
		if g.Name == "A" {
			aPP = g.PPChange
		}
		if g.Name == "B" {
			bPP = g.PPChange
		}
	}
	require.Greater(t, aPP, 0.0)
	require.Less(t, bPP, 0.0)
	// No others when TopN >= groups
	require.InDelta(t, 0.0, out.OtherBaseline, 0.001)
	require.InDelta(t, 0.0, out.OtherCurrent, 0.001)
}
