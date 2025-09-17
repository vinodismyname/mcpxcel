package workbooks

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/vinoddu/mcpxcel/config"
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

// WorkbookGate coordinates capacity for open workbook handles (backed by runtime.Controller).
type WorkbookGate interface {
	AcquireWorkbook(ctx context.Context) error
	ReleaseWorkbook()
}

// Manager provides lifecycle hooks for opening and closing workbooks and a stateless handle cache.
type Manager struct {
	mu           sync.RWMutex
	handles      map[string]*Handle
	ttl          time.Duration
	cleanupEvery time.Duration
	clock        func() time.Time
	gate         WorkbookGate
	stopCh       chan struct{}
	cleanupWG    sync.WaitGroup
	validator    PathValidator
}

// NewManager constructs a lifecycle manager with TTL-bearing handle cache.
// Pass ttl or cleanupEvery <= 0 to use defaults from config.
// Gate can be nil for tests; clock defaults to time.Now when nil.
func NewManager(ttl, cleanupEvery time.Duration, gate WorkbookGate, clock func() time.Time) *Manager {
	if ttl <= 0 {
		ttl = config.DefaultWorkbookIdleTTL
	}
	if cleanupEvery <= 0 {
		cleanupEvery = config.DefaultWorkbookCleanupPeriod
	}
	if clock == nil {
		clock = time.Now
	}
	return &Manager{
		handles:      make(map[string]*Handle),
		ttl:          ttl,
		cleanupEvery: cleanupEvery,
		clock:        clock,
		gate:         gate,
		stopCh:       make(chan struct{}),
	}
}

// Start launches periodic eviction of expired handles.
func (m *Manager) Start() {
	m.cleanupWG.Add(1)
	ticker := time.NewTicker(m.cleanupEvery)
	go func() {
		defer m.cleanupWG.Done()
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.EvictExpired()
			}
		}
	}()
}

// Close stops background cleanup and closes all open handles.
func (m *Manager) Close(ctx context.Context) error {
	// Stop the cleanup loop
	close(m.stopCh)
	done := make(chan struct{})
	go func() { m.cleanupWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Close any remaining handles
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, h := range m.handles {
		// block until we can close; best-effort cleanup
		h.mu.Lock()
		_ = h.File.Close()
		h.mu.Unlock()
		delete(m.handles, id)
		if m.gate != nil {
			m.gate.ReleaseWorkbook()
		}
	}
	return nil
}

// NewHandle initializes a Handle wrapper for an excelize workbook instance.
func (m *Manager) NewHandle(id string, file *excelize.File, ttl time.Duration) (*Handle, error) {
	if file == nil {
		return nil, fmt.Errorf("workbooks: nil excelize file")
	}
	if id == "" {
		return nil, fmt.Errorf("workbooks: empty handle id")
	}
	if ttl <= 0 {
		ttl = m.ttl
	}
	loadedAt := m.clock()
	return &Handle{
		ID:        id,
		File:      file,
		LoadedAt:  loadedAt,
		ExpiresAt: loadedAt.Add(ttl),
	}, nil
}

// ErrHandleNotFound indicates an unknown or expired handle ID.
var ErrHandleNotFound = errors.New("workbooks: handle not found")

// Open opens a workbook from the given path, registers a TTL-bearing handle, and returns its ID.
// The manager enforces open-workbook capacity via the gate when provided.
func (m *Manager) Open(ctx context.Context, path string) (string, error) {
	if err := m.acquire(ctx); err != nil {
		return "", err
	}

	// Basic format validation; detailed allow-list checks are handled by the security module in later tasks.
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".xlsx", ".xlsm", ".xltx", ".xltm":
		// allowed Excel formats
	default:
		m.release()
		return "", fmt.Errorf("workbooks: unsupported format: %s", ext)
	}

	// Optional path validation via security manager when provided.
	if m.validator != nil {
		canonical, err := m.validator.ValidateOpenPath(path)
		if err != nil {
			m.release()
			return "", err
		}
		path = canonical
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		m.release()
		return "", err
	}
	id := uuid.NewString()
	h, err := m.NewHandle(id, f, m.ttl)
	if err != nil {
		_ = f.Close()
		m.release()
		return "", err
	}

	m.mu.Lock()
	m.handles[id] = h
	m.mu.Unlock()

	return id, nil
}

// Adopt registers an existing excelize.File as a managed handle. Intended for tests or advanced flows.
func (m *Manager) Adopt(ctx context.Context, f *excelize.File) (string, error) {
	if f == nil {
		return "", fmt.Errorf("workbooks: nil file")
	}
	if err := m.acquire(ctx); err != nil {
		return "", err
	}
	id := uuid.NewString()
	h, err := m.NewHandle(id, f, m.ttl)
	if err != nil {
		m.release()
		return "", err
	}
	m.mu.Lock()
	m.handles[id] = h
	m.mu.Unlock()
	return id, nil
}

// Get returns the handle when present and refreshes its TTL.
func (m *Manager) Get(id string) (*Handle, bool) {
	m.mu.RLock()
	h, ok := m.handles[id]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	// Refresh TTL on access (idle timeout semantics)
	now := m.clock()
	h.mu.Lock()
	h.ExpiresAt = now.Add(m.ttl)
	h.mu.Unlock()
	return h, true
}

// WithRead obtains a shared read lock for the handle and executes fn.
func (m *Manager) WithRead(id string, fn func(*excelize.File) error) error {
	h, ok := m.Get(id)
	if !ok {
		return ErrHandleNotFound
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return fn(h.File)
}

// WithWrite obtains an exclusive write lock for the handle and executes fn.
func (m *Manager) WithWrite(id string, fn func(*excelize.File) error) error {
	h, ok := m.Get(id)
	if !ok {
		return ErrHandleNotFound
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return fn(h.File)
}

// CloseHandle closes and removes a handle by ID, releasing capacity via the gate.
func (m *Manager) CloseHandle(ctx context.Context, id string) error {
	m.mu.Lock()
	h, ok := m.handles[id]
	if ok {
		delete(m.handles, id)
	}
	m.mu.Unlock()
	if !ok {
		return ErrHandleNotFound
	}
	// Ensure no other readers/writers are inside the workbook.
	h.mu.Lock()
	err := h.File.Close()
	h.mu.Unlock()
	m.release()
	return err
}

// EvictExpired scans for expired handles and closes them.
func (m *Manager) EvictExpired() {
	now := m.clock()
	var expired []*Handle
	var expiredIDs []string

	m.mu.RLock()
	for id, h := range m.handles {
		h.mu.RLock()
		isExpired := now.After(h.ExpiresAt)
		h.mu.RUnlock()
		if isExpired {
			expired = append(expired, h)
			expiredIDs = append(expiredIDs, id)
		}
	}
	m.mu.RUnlock()

	if len(expired) == 0 {
		return
	}

	// Close outside of read lock; remove from map under write lock.
	for i, h := range expired {
		// block until safe to close
		h.mu.Lock()
		_ = h.File.Close()
		h.mu.Unlock()

		m.mu.Lock()
		delete(m.handles, expiredIDs[i])
		m.mu.Unlock()
		m.release()
	}
}

// Count returns the current number of cached handles.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.handles)
}

func (m *Manager) acquire(ctx context.Context) error {
	if m.gate == nil {
		return nil
	}
	return m.gate.AcquireWorkbook(ctx)
}

func (m *Manager) release() {
	if m.gate == nil {
		return
	}
	m.gate.ReleaseWorkbook()
}

// Close releases the underlying excelize file resources for a single handle.
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

// PathValidator abstracts filesystem path validation. Implementations should
// return a canonical absolute path if allowed, or an error when denied.
type PathValidator interface {
	ValidateOpenPath(path string) (string, error)
}
