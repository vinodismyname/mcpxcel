package registry

import (
	"context"
	"sort"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tmc/langchaingo/llms"
)

// ToolProvider resolves MCP tool definitions and associates runtime metadata.
type ToolProvider interface {
	Tools(context.Context) ([]mcp.Tool, error)
}

// Registry maintains tool definitions and optional LLM providers for analytical workflows.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]mcp.Tool
	model llms.Model
}

// New constructs an empty Registry ready for tool population.
func New() *Registry {
	return &Registry{
		tools: map[string]mcp.Tool{},
	}
}

// WithModel assigns the configured LLM model used for insight-generating tools.
func (r *Registry) WithModel(model llms.Model) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.model = model
}

// Register stores a tool definition for discovery.
func (r *Registry) Register(tool mcp.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[tool.Name] = tool
}

// Get returns a tool by name when present.
func (r *Registry) Get(name string) (mcp.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Tools returns a stable-sorted list of registered tool definitions.
func (r *Registry) Tools(ctx context.Context) ([]mcp.Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_ = ctx // placeholder for future context-aware filtering

	tools := make([]mcp.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	return tools, nil
}

// ModelContextSize exposes the configured model's context window when available.
func (r *Registry) ModelContextSize(modelName string) int {
	return llms.GetModelContextSize(modelName)
}
