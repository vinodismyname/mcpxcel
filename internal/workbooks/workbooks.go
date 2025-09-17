package workbooks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"
)

// Handle represents an in-memory workbook reference paired with metadata for TTL eviction.
type Handle struct {
	ID        string
	File      *excelize.File
	LoadedAt  time.Time
	ExpiresAt time.Time
	mu        sync.RWMutex
}

// Manager provides lifecycle hooks for opening and closing workbooks.
type Manager struct {
	clock func() time.Time
}

// NewManager builds a Manager with the provided clock for easy testing.
func NewManager(clock func() time.Time) *Manager {
	if clock == nil {
		clock = time.Now
	}
	return &Manager{clock: clock}
}

// NewHandle initializes a Handle wrapper for an excelize workbook instance.
func (m *Manager) NewHandle(id string, file *excelize.File, ttl time.Duration) (*Handle, error) {
	if file == nil {
		return nil, fmt.Errorf("workbooks: nil excelize file")
	}
	if id == "" {
		return nil, fmt.Errorf("workbooks: empty handle id")
	}
	loadedAt := m.clock()
	return &Handle{
		ID:        id,
		File:      file,
		LoadedAt:  loadedAt,
		ExpiresAt: loadedAt.Add(ttl),
	}, nil
}

// Close releases the underlying excelize file resources.
func (h *Handle) Close(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return h.File.Close()
}

// Expired reports whether the handle has reached its TTL.
func (h *Handle) Expired(now time.Time) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return now.After(h.ExpiresAt)
}
