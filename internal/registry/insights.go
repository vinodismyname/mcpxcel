package registry

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/vinodismyname/mcpxcel/internal/insights"
	"github.com/vinodismyname/mcpxcel/internal/runtime"
	"github.com/vinodismyname/mcpxcel/internal/workbooks"
)

// RegisterInsightsTools wires the sequential_insights planning tool.
func RegisterInsightsTools(s *server.MCPServer, reg *Registry, limits runtime.Limits, mgr *workbooks.Manager) {
	planner := &insights.Planner{Limits: limits, Mgr: mgr}

	// Define tool with typed schemas
	tool := mcp.NewTool(
		"sequential_insights",
		mcp.WithDescription("Domain-neutral planning for stepwise analysis with recommended tools and clarifying questions"),
		mcp.WithInputSchema[insights.SequentialInsightsInput](),
		mcp.WithOutputSchema[insights.SequentialInsightsOutput](),
	)

	s.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, in insights.SequentialInsightsInput) (*mcp.CallToolResult, error) {
		out, err := planner.Plan(ctx, in)
		if err != nil {
			return mcp.NewToolResultError("PLANNING_FAILED: " + err.Error()), nil
		}
		// Attach a concise text summary for clients ignoring structured out
		summary := out.CurrentStep
		res := mcp.NewToolResultStructured(out, summary)
		res.Content = []mcp.Content{mcp.NewTextContent(summary)}
		return res, nil
	}))

	reg.Register(tool)
}
