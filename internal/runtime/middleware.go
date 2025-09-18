package runtime

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Middleware enforces runtime limits for tool calls using the Controller.
// It bounds global concurrency and applies an operation timeout to each call.
type Middleware struct {
	ctrl *Controller
}

// NewMiddleware constructs a Middleware bound to the provided Controller.
func NewMiddleware(ctrl *Controller) *Middleware {
	return &Middleware{ctrl: ctrl}
}

// ToolMiddleware implements mcp-go's tool handler middleware interface.
// It acquires a request slot, applies a timeout, and guarantees release.
func (m *Middleware) ToolMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Attempt to acquire request capacity with a bounded wait.
		acquireCtx := ctx
		if m.ctrl.limits.AcquireRequestTimeout > 0 {
			var cancel context.CancelFunc
			acquireCtx, cancel = context.WithTimeout(ctx, m.ctrl.limits.AcquireRequestTimeout)
			defer cancel()
		}

		if err := m.ctrl.AcquireRequest(acquireCtx); err != nil {
			// Return a tool-level error so the client can self-correct/retry.
			msg := fmt.Sprintf("BUSY_RESOURCE: concurrent request limit reached (max=%d). Please retry shortly.", m.ctrl.limits.MaxConcurrentRequests)
			return mcp.NewToolResultError(msg), nil
		}
		defer m.ctrl.ReleaseRequest()

		callCtx := ctx
		cancel := func() {}
		// Apply operation timeout to bound execution time.
		if m.ctrl.limits.OperationTimeout > 0 {
			callCtx, cancel = context.WithTimeout(ctx, m.ctrl.limits.OperationTimeout)
		}
		defer cancel()

		// Delegate to the next handler.
		res, err := next(callCtx, req)

		// If the underlying handler surfaced a context deadline, prefer a tool-level timeout error.
		if err == context.DeadlineExceeded || (callCtx.Err() == context.DeadlineExceeded && err == nil && res == nil) {
			return mcp.NewToolResultError("TIMEOUT: operation exceeded configured time limit"), nil
		}

		return res, err
	}
}
