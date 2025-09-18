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

func createConcentrationWorkbook(t *testing.T) (string, string) {
	t.Helper()
	f := excelize.NewFile()
	sh := "Conc"
	f.SetSheetName("Sheet1", sh)
	// Header: Product, Value
	require.NoError(t, f.SetSheetRow(sh, "A1", &[]string{"Product", "Value"}))
	require.NoError(t, f.SetSheetRow(sh, "A2", &[]string{"A", "80"}))
	require.NoError(t, f.SetSheetRow(sh, "A3", &[]string{"B", "20"}))

	dir := t.TempDir()
	path := filepath.Join(dir, "conc.xlsx")
	require.NoError(t, f.SaveAs(path))
	require.NoError(t, f.Close())
	return path, sh
}

func TestConcentrationMetrics_HighlyConcentrated(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	mgr := workbooks.NewManager(0, 0, nil, nil)
	c := &Concentrator{Limits: limits, Mgr: mgr}

	path, sh := createConcentrationWorkbook(t)
	in := ConcentrationMetricsInput{Path: path, Sheet: sh, Range: "A1:B3", DimIndex: 1, MeasureIndex: 2}
	out, err := c.ConcentrationMetrics(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "highly_concentrated", out.Band)
	require.InDelta(t, 0.68, out.HHI, 0.01)
}
