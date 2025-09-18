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

func createWorkbookWithTwoTables(t *testing.T) string {
	t.Helper()
	f := excelize.NewFile()
	sh := "Sheet1"
	// Table 1 at A1:C4
	require.NoError(t, f.SetSheetRow(sh, "A1", &[]string{"Name", "Value", "Date"}))
	require.NoError(t, f.SetSheetRow(sh, "A2", &[]string{"A", "10", "2024-01-01"}))
	require.NoError(t, f.SetSheetRow(sh, "A3", &[]string{"B", "20", "2024-01-02"}))
	require.NoError(t, f.SetSheetRow(sh, "A4", &[]string{"C", "30", "2024-01-03"}))

	// Gap rows/cols then Table 2 at E6:G8
	require.NoError(t, f.SetSheetRow(sh, "E6", &[]string{"Prod", "Qty", "When"}))
	require.NoError(t, f.SetSheetRow(sh, "E7", &[]string{"X", "5", "2024-01-01"}))
	require.NoError(t, f.SetSheetRow(sh, "E8", &[]string{"Y", "7", "2024-01-02"}))

	dir := t.TempDir()
	path := filepath.Join(dir, "two_tables.xlsx")
	require.NoError(t, f.SaveAs(path))
	require.NoError(t, f.Close())
	return path
}

func TestDetectTables_FindsMultipleCandidates(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	mgr := workbooks.NewManager(0, 0, nil, nil)
	d := &Detector{Limits: limits, Mgr: mgr}

	path := createWorkbookWithTwoTables(t)
	out, err := d.DetectTables(context.Background(), DetectTablesInput{Path: path, Sheet: "Sheet1", MaxTables: 5})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(out.Candidates), 2)
	// Expect ranges for both candidates to be present
	found1 := false
	found2 := false
	for _, c := range out.Candidates {
		if c.Range == "A1:C4" {
			found1 = true
		}
		if c.Range == "E6:G8" {
			found2 = true
		}
		// header confidence should be within [0,1]
		require.GreaterOrEqual(t, c.Confidence, 0.0)
		require.LessOrEqual(t, c.Confidence, 1.0)
	}
	require.True(t, found1, "expected A1:C4 candidate")
	require.True(t, found2, "expected E6:G8 candidate")
}
