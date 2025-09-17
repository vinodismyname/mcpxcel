package main

import (
	"context"

	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	"github.com/vinoddu/mcpxcel/internal/registry"
	"github.com/vinoddu/mcpxcel/internal/runtime"
	"github.com/vinoddu/mcpxcel/pkg/version"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	logger := zlog.With().Str("service", "mcpxcel-server").Logger()
	ctx := logger.WithContext(context.Background())

	limits := runtime.NewLimits(10, 4)
	runtimeController := runtime.NewController(limits)
	toolRegistry := registry.New()

	srv := server.NewMCPServer(
		"MCP Excel Analysis Server",
		version.Version(),
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, false),
	)

	toolContextSize := toolRegistry.ModelContextSize("gpt-4o")

	logger.Info().
		Ctx(ctx).
		Str("version", version.Version()).
		Int("max_concurrent_requests", limits.MaxConcurrentRequests).
		Int("max_open_workbooks", limits.MaxOpenWorkbooks).
		Int("model_context_size", toolContextSize).
		Bool("server_initialized", srv != nil).
		Msg("initialized server scaffolding")

	_ = runtimeController
}
