package registry

import (
    "context"
    "os"
    "strings"

    "github.com/mark3labs/mcp-go/mcp"
)

// WriteToolFilter conditionally hides write/transform tools unless explicitly enabled.
// Enable by setting environment variable MCPXCEL_ENABLE_WRITES=true.
type WriteToolFilter struct {
    allowWrites bool
}

// NewWriteToolFilterFromEnv constructs a filter using MCPXCEL_ENABLE_WRITES.
func NewWriteToolFilterFromEnv() *WriteToolFilter {
    v := strings.ToLower(strings.TrimSpace(os.Getenv("MCPXCEL_ENABLE_WRITES")))
    allow := v == "1" || v == "true" || v == "yes"
    return &WriteToolFilter{allowWrites: allow}
}

// FilterTools implements server tool filtering semantics.
// When writes are disabled, tools with prefixes commonly used for writes
// are excluded from discovery: write_, update_, transform_.
func (f *WriteToolFilter) FilterTools(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
    if f.allowWrites {
        return tools
    }
    out := make([]mcp.Tool, 0, len(tools))
    for _, t := range tools {
        name := strings.ToLower(t.Name)
        if strings.HasPrefix(name, "write_") || strings.HasPrefix(name, "update_") || strings.HasPrefix(name, "transform_") {
            continue
        }
        out = append(out, t)
    }
    return out
}

