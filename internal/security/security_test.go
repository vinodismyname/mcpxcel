package security

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func mustTempDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	// Ensure real path (EvalSymlinks on macOS can change /var -> /private/var)
	real, err := filepath.EvalSymlinks(d)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	return real
}

func TestNewManager_ValidateConfig(t *testing.T) {
	dir := mustTempDir(t)
	m, err := NewManager([]string{dir}, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := m.ValidateConfig(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if got := len(m.AllowedDirectories()); got != 1 {
		t.Fatalf("allowed dirs len = %d, want 1", got)
	}
}

func TestValidateOpenPath_AllowsWithinRoot(t *testing.T) {
	root := mustTempDir(t)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create a valid Excel file path (empty file is enough for path validation)
	fpath := filepath.Join(sub, "ok.xlsx")
	if err := os.WriteFile(fpath, []byte("test"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	m, err := NewManager([]string{root}, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	got, err := m.ValidateOpenPath(fpath)
	if err != nil {
		t.Fatalf("validate path: %v", err)
	}
	// Path returned should be canonical absolute
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}

func TestValidateOpenPath_DeniesOutsideRoot(t *testing.T) {
	root := mustTempDir(t)
	outsideDir := mustTempDir(t)
	outside := filepath.Join(outsideDir, "escape.xlsx")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	m, err := NewManager([]string{root}, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if _, err := m.ValidateOpenPath(outside); err == nil {
		t.Fatalf("expected error for outside path")
	}
}

func TestValidateOpenPath_SymlinkEscapeDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on Windows")
	}
	root := mustTempDir(t)
	outsideDir := mustTempDir(t)
	target := filepath.Join(outsideDir, "target.xlsx")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(root, "link.xlsx")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	m, err := NewManager([]string{root}, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if _, err := m.ValidateOpenPath(link); err == nil {
		t.Fatalf("expected error for symlink escape")
	}
}

func TestValidateOpenPath_UnsupportedExt(t *testing.T) {
	root := mustTempDir(t)
	fp := filepath.Join(root, "bad.txt")
	if err := os.WriteFile(fp, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	m, err := NewManager([]string{root}, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if _, err := m.ValidateOpenPath(fp); err == nil {
		t.Fatalf("expected unsupported extension error")
	}
}
