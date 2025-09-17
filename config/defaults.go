package config

import "time"

// Default runtime limits and guardrails for the MCP Excel Analysis Server.
// These values are conservative and can be overridden by future configuration
// mechanisms (env, CLI, or files). They are referenced by internal/runtime.

const (
	// Concurrency
	DefaultMaxConcurrentRequests = 10
	DefaultMaxOpenWorkbooks      = 4

	// Payload and row limits
	DefaultMaxPayloadBytes = 128 * 1024 // 128KB
	DefaultMaxCellsPerOp   = 10_000
	DefaultPreviewRowLimit = 10 // First 10 rows by default
)

const (
	// Timeouts
	DefaultOperationTimeout      = 30 * time.Second
	DefaultAcquireRequestTimeout = 2 * time.Second
)
