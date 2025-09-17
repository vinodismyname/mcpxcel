package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	"github.com/vinoddu/mcpxcel/internal/registry"
	"github.com/vinoddu/mcpxcel/internal/runtime"
	"github.com/vinoddu/mcpxcel/internal/security"
	"github.com/vinoddu/mcpxcel/pkg/version"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	var (
		useStdio        bool
		shutdownTimeout time.Duration
	)

	flag.BoolVar(&useStdio, "stdio", false, "Run server over stdio transport")
	flag.DurationVar(&shutdownTimeout, "shutdown-timeout", 5*time.Second, "Graceful shutdown timeout")
	flag.Parse()

	logger := zlog.With().Str("service", "mcpxcel-server").Logger()
	ctx := logger.WithContext(context.Background())

	// Security: validate allow-list directories on startup (fail-safe on error)
	secMgr, err := security.NewManagerFromEnv()
	if err != nil {
		logger.Error().Err(err).Msg("security: failed to initialize manager from env")
		fmt.Fprintln(os.Stderr, "invalid security configuration; set MCPXCEL_ALLOWED_DIRS")
		os.Exit(1)
	}
	if err := secMgr.ValidateConfig(); err != nil {
		logger.Error().Err(err).Msg("security: invalid allow-list configuration")
		fmt.Fprintln(os.Stderr, "no allowed directories configured; set MCPXCEL_ALLOWED_DIRS")
		os.Exit(1)
	}
	logger.Info().Strs("allowed_dirs", secMgr.AllowedDirectories()).Msg("security allow-list configured")

	limits := runtime.NewLimits(10, 4)
	runtimeController := runtime.NewController(limits)
	runtimeMW := runtime.NewMiddleware(runtimeController)

	toolRegistry := registry.New()

	writeFilter := registry.NewWriteToolFilterFromEnv()

	srv := server.NewMCPServer(
		"MCP Excel Analysis Server",
		version.Version(),
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, false),
		server.WithRecovery(),
		server.WithHooks(buildHooks(logger)),
		server.WithToolHandlerMiddleware(runtimeMW.ToolMiddleware),
		server.WithToolFilter(func(ctx context.Context, tools []mcp.Tool) []mcp.Tool { return writeFilter.FilterTools(ctx, tools) }),
	)

	// Register foundation tool schemas for discovery
	registry.RegisterFoundationTools(srv, toolRegistry, runtimeController.LimitsSnapshot())

	toolContextSize := toolRegistry.ModelContextSize("gpt-4o")

	logger.Info().
		Ctx(ctx).
		Str("version", version.Version()).
		Int("max_concurrent_requests", limits.MaxConcurrentRequests).
		Int("max_open_workbooks", limits.MaxOpenWorkbooks).
		Int("model_context_size", toolContextSize).
		Bool("stdio", useStdio).
		Msg("server bootstrap configured")

	if useStdio {
		if err := server.ServeStdio(srv); err != nil {
			// Use stderr for transport errors so clients don't misinterpret output
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// If no transport flags provided, print usage and exit non-zero
	fmt.Fprintln(os.Stderr, "no transport selected; use --stdio to run over stdio")
	os.Exit(2)
}

// buildHooks constructs mcp-go server hooks for basic telemetry.
func buildHooks(logger zerolog.Logger) *server.Hooks {
	hooks := &server.Hooks{}

	hooks.AddOnRegisterSession(func(ctx context.Context, session server.ClientSession) {
		logger.Info().Str("session_id", session.SessionID()).Msg("session registered")
	})

	hooks.AddOnUnregisterSession(func(ctx context.Context, session server.ClientSession) {
		logger.Info().Str("session_id", session.SessionID()).Msg("session unregistered")
	})

	hooks.AddAfterListTools(func(ctx context.Context, id any, req *mcp.ListToolsRequest, res *mcp.ListToolsResult) {
		// Keep it light: tool count only
		logger.Info().Int("tools", len(res.Tools)).Msg("list_tools served")
	})

	hooks.AddAfterReadResource(func(ctx context.Context, id any, req *mcp.ReadResourceRequest, res *mcp.ReadResourceResult) {
		logger.Info().Str("uri", req.Params.URI).Msg("resource read served")
	})

	hooks.AddAfterCallTool(func(ctx context.Context, id any, req *mcp.CallToolRequest, res *mcp.CallToolResult) {
		logger.Info().Str("tool", req.Params.Name).Msg("tool call served")
	})

	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		logger.Error().Str("method", string(method)).Err(err).Msg("request error")
	})

	return hooks
}
