package runtime

import (
	"context"
	"time"

	"github.com/vinodismyname/mcpxcel/config"
	"golang.org/x/sync/semaphore"
)

// Limits captures the concurrency and workbook guardrails configured for the server.
type Limits struct {
	// Concurrency caps
	MaxConcurrentRequests int
	MaxOpenWorkbooks      int

	// Payload and row bounds
	MaxPayloadBytes int
	MaxCellsPerOp   int
	PreviewRowLimit int

	// Timeouts
	OperationTimeout      time.Duration
	AcquireRequestTimeout time.Duration
}

// NewLimits initializes Limits with sensible fallbacks when values are unset.
func NewLimits(maxConcurrentRequests, maxOpenWorkbooks int) Limits {
	if maxConcurrentRequests <= 0 {
		maxConcurrentRequests = config.DefaultMaxConcurrentRequests
	}
	if maxOpenWorkbooks <= 0 {
		maxOpenWorkbooks = config.DefaultMaxOpenWorkbooks
	}

	return Limits{
		MaxConcurrentRequests: maxConcurrentRequests,
		MaxOpenWorkbooks:      maxOpenWorkbooks,
		MaxPayloadBytes:       config.DefaultMaxPayloadBytes,
		MaxCellsPerOp:         config.DefaultMaxCellsPerOp,
		PreviewRowLimit:       config.DefaultPreviewRowLimit,
		OperationTimeout:      config.DefaultOperationTimeout,
		AcquireRequestTimeout: config.DefaultAcquireRequestTimeout,
	}
}

// Controller coordinates runtime semaphores for request and workbook guardrails.
type Controller struct {
	limits            Limits
	requestSemaphore  *semaphore.Weighted
	workbookSemaphore *semaphore.Weighted
}

// NewController constructs a Controller backed by weighted semaphores.
func NewController(limits Limits) *Controller {
	return &Controller{
		limits:            limits,
		requestSemaphore:  semaphore.NewWeighted(int64(limits.MaxConcurrentRequests)),
		workbookSemaphore: semaphore.NewWeighted(int64(limits.MaxOpenWorkbooks)),
	}
}

// AcquireRequest reserves capacity for an incoming request.
func (c *Controller) AcquireRequest(ctx context.Context) error {
	return c.requestSemaphore.Acquire(ctx, 1)
}

// ReleaseRequest frees previously-acquired request capacity.
func (c *Controller) ReleaseRequest() {
	c.requestSemaphore.Release(1)
}

// AcquireWorkbook reserves an open workbook slot.
func (c *Controller) AcquireWorkbook(ctx context.Context) error {
	return c.workbookSemaphore.Acquire(ctx, 1)
}

// ReleaseWorkbook frees an open workbook slot.
func (c *Controller) ReleaseWorkbook() {
	c.workbookSemaphore.Release(1)
}

// LimitsSnapshot exposes the configured guardrails for telemetry and discovery.
func (c *Controller) LimitsSnapshot() Limits {
	return c.limits
}
