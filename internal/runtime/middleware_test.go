package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func TestMiddleware_AllowsWhenCapacity(t *testing.T) {
	limits := NewLimits(1, 1)
	limits.OperationTimeout = 200 * time.Millisecond
	limits.AcquireRequestTimeout = 50 * time.Millisecond

	ctrl := NewController(limits)
	mw := NewMiddleware(ctrl)

	next := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	}

	wrapped := mw.ToolMiddleware(server.ToolHandlerFunc(next))

	res, err := wrapped(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.False(t, res.IsError)
}

func TestMiddleware_BusyWhenSaturated(t *testing.T) {
	limits := NewLimits(1, 1)
	limits.AcquireRequestTimeout = 10 * time.Millisecond

	ctrl := NewController(limits)
	// Saturate the request semaphore.
	require.NoError(t, ctrl.AcquireRequest(context.Background()))
	defer ctrl.ReleaseRequest()

	mw := NewMiddleware(ctrl)

	next := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t.Fatal("next should not be called when saturated")
		return nil, nil
	}

	wrapped := mw.ToolMiddleware(server.ToolHandlerFunc(next))

	res, err := wrapped(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, res.IsError)
}

func TestMiddleware_TimeoutApplied(t *testing.T) {
	limits := NewLimits(1, 1)
	limits.OperationTimeout = 20 * time.Millisecond
	limits.AcquireRequestTimeout = 20 * time.Millisecond

	ctrl := NewController(limits)
	mw := NewMiddleware(ctrl)

	// This handler only returns when the context is done.
	next := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	wrapped := mw.ToolMiddleware(server.ToolHandlerFunc(next))

	res, err := wrapped(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, res.IsError)
}
