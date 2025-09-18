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

func createSchemaWorkbook(t *testing.T) (string, string) {
	t.Helper()
	f := excelize.NewFile()
	sh := "Sheet1"
	// Header: id, date, product, revenue, plan%
	require.NoError(t, f.SetSheetRow(sh, "A1", &[]string{"id", "date", "product", "revenue", "plan%"}))
	require.NoError(t, f.SetSheetRow(sh, "A2", &[]string{"1", "2024-01-01", "X", "100", "10%"}))
	require.NoError(t, f.SetSheetRow(sh, "A3", &[]string{"2", "2024-01-02", "Y", "200", "15%"}))
	require.NoError(t, f.SetSheetRow(sh, "A4", &[]string{"3", "2024-01-03", "X", "300", "20%"}))
	require.NoError(t, f.SetSheetRow(sh, "A5", &[]string{"4", "2024-01-03", "Z", "400", "25%"}))

	dir := t.TempDir()
	path := filepath.Join(dir, "schema.xlsx")
	require.NoError(t, f.SaveAs(path))
	require.NoError(t, f.Close())
	return path, sh
}

func TestProfileSchema_InferenceAndQuality(t *testing.T) {
	limits := runtime.NewLimits(8, 8)
	mgr := workbooks.NewManager(0, 0, nil, nil)
	p := &Profiler{Limits: limits, Mgr: mgr}

	path, sh := createSchemaWorkbook(t)
	in := ProfileSchemaInput{Path: path, Sheet: sh, Range: "A1:E5", MaxSampleRows: 10}
	out, err := p.ProfileSchema(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, 5, len(out.Columns))

	// id
	require.Equal(t, "id", out.Columns[0].Name)
	require.Equal(t, "id", out.Columns[0].Role)
	// date
	require.Equal(t, "date", out.Columns[1].Type)
	// product
	require.Equal(t, "dimension", out.Columns[2].Role)
	// revenue
	require.Equal(t, "measure", out.Columns[3].Role)
	// plan% detected as target due to name and percent type
	require.Equal(t, "target", out.Columns[4].Role)
}
