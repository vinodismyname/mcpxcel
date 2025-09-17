package security

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
)

// Manager enforces filesystem allow-list and path validation guardrails.
// It resolves and stores canonical absolute directory paths and validates
// that requested file paths are within these roots and have supported extensions.
type Manager struct {
    allowedDirs []string
    allowedExts map[string]struct{}
}

// ErrNotAllowed indicates the requested path is outside the allow-list roots.
var ErrNotAllowed = errors.New("security: path not allowed")

// ErrUnsupportedExtension indicates the requested file extension is not supported.
var ErrUnsupportedExtension = errors.New("security: unsupported file extension")

// ErrNotFound indicates the requested file does not exist or is not accessible.
var ErrNotFound = errors.New("security: file not found")

// NewManager constructs a security manager given an allow-list of directories
// and a list of allowed file extensions (case-insensitive, with leading dot).
// Directories are canonicalized (absolute + EvalSymlinks) and validated.
func NewManager(allowDirs []string, allowedExtensions []string) (*Manager, error) {
    if len(allowedExtensions) == 0 {
        allowedExtensions = []string{".xlsx", ".xlsm", ".xltx", ".xltm"}
    }

    exts := make(map[string]struct{}, len(allowedExtensions))
    for _, e := range allowedExtensions {
        e = strings.ToLower(strings.TrimSpace(e))
        if e == "" || !strings.HasPrefix(e, ".") {
            return nil, fmt.Errorf("security: invalid extension: %q", e)
        }
        exts[e] = struct{}{}
    }

    canonical := make([]string, 0, len(allowDirs))
    for _, d := range allowDirs {
        d = strings.TrimSpace(d)
        if d == "" { // skip empties
            continue
        }
        abs, err := filepath.Abs(d)
        if err != nil {
            return nil, fmt.Errorf("security: resolve abs for %q: %w", d, err)
        }
        // EvalSymlinks so that symlinked roots cannot be used to escape later.
        real, err := filepath.EvalSymlinks(abs)
        if err != nil {
            return nil, fmt.Errorf("security: eval symlinks for %q: %w", abs, err)
        }
        info, err := os.Stat(real)
        if err != nil {
            return nil, fmt.Errorf("security: stat %q: %w", real, err)
        }
        if !info.IsDir() {
            return nil, fmt.Errorf("security: allow-list entry is not a directory: %q", real)
        }
        // Normalize with a trailing separator removed for consistent prefix checks.
        canonical = append(canonical, filepath.Clean(real))
    }

    return &Manager{allowedDirs: canonical, allowedExts: exts}, nil
}

// NewManagerFromEnv constructs a Manager from environment variable
// MCPXCEL_ALLOWED_DIRS as a path list separated by os.PathListSeparator.
// If the variable is empty, an empty allow-list is used (deny-by-default).
func NewManagerFromEnv() (*Manager, error) {
    list := os.Getenv("MCPXCEL_ALLOWED_DIRS")
    var dirs []string
    if list != "" {
        dirs = filepath.SplitList(list)
    }
    return NewManager(dirs, nil)
}

// AllowedDirectories returns the canonical allow-list roots.
func (m *Manager) AllowedDirectories() []string {
    out := make([]string, len(m.allowedDirs))
    copy(out, m.allowedDirs)
    return out
}

// ValidateConfig returns an error when no allow-list entries are configured.
// This supports fail-safe startup where file operations should be disabled
// until explicit directories are provided by the operator.
func (m *Manager) ValidateConfig() error {
    if len(m.allowedDirs) == 0 {
        return errors.New("security: no allowed directories configured")
    }
    return nil
}

// ValidateOpenPath ensures the input path refers to an existing file with an
// allowed extension inside one of the configured allow-list directories.
// It returns the canonical absolute path suitable for opening.
func (m *Manager) ValidateOpenPath(input string) (string, error) {
    if input == "" {
        return "", ErrNotAllowed
    }
    // Extension check first for quick rejection.
    ext := strings.ToLower(filepath.Ext(input))
    if _, ok := m.allowedExts[ext]; !ok {
        return "", ErrUnsupportedExtension
    }

    // Make absolute and resolve symlinks for the target file.
    abs, err := filepath.Abs(input)
    if err != nil {
        return "", fmt.Errorf("security: abs path: %w", err)
    }
    real, err := filepath.EvalSymlinks(abs)
    if err != nil {
        // If the file doesn't exist or can't resolve symlinks, return not found.
        if errors.Is(err, os.ErrNotExist) {
            return "", ErrNotFound
        }
        return "", fmt.Errorf("security: eval symlinks: %w", err)
    }

    info, err := os.Stat(real)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return "", ErrNotFound
        }
        return "", fmt.Errorf("security: stat: %w", err)
    }
    if info.IsDir() {
        return "", ErrNotAllowed
    }

    // Check containment: real path must be within one of the allow-list roots.
    for _, root := range m.allowedDirs {
        // filepath.Rel returns a path starting with ".." when outside.
        rel, err := filepath.Rel(root, real)
        if err != nil {
            continue
        }
        if rel == "." || rel == "" {
            // exact root match but file is not dir; continue to next root
            continue
        }
        // Normalize separators and check for escape attempts.
        if !strings.HasPrefix(rel, "..") && !strings.HasPrefix(filepath.Clean(rel), "..") {
            // Contained within root.
            return real, nil
        }
    }
    return "", ErrNotAllowed
}

