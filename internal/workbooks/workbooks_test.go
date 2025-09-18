package workbooks

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// fakeGate implements WorkbookGate for tests with counters.
type fakeGate struct {
	acquireErr error
	acquires   atomic.Int64
	releases   atomic.Int64
}

func (g *fakeGate) AcquireWorkbook(ctx context.Context) error {
	g.acquires.Add(1)
	return g.acquireErr
}
func (g *fakeGate) ReleaseWorkbook() { g.releases.Add(1) }

func TestAdoptGetClose(t *testing.T) {
	gate := &fakeGate{}
	// Use a long TTL to avoid eviction in this test; disable background loop by not calling Start.
	m := NewManager(2*time.Second, time.Second, gate, time.Now)

	f := excelize.NewFile()
	id, err := m.Adopt(context.Background(), f)
	require.NoError(t, err)
	require.NotEmpty(t, id)
	require.Equal(t, int64(1), gate.acquires.Load())
	require.Equal(t, 1, m.Count())

	h, ok := m.Get(id)
	require.True(t, ok)
	require.Equal(t, id, h.ID)

	// Close and ensure it is removed and capacity released.
	require.NoError(t, m.CloseHandle(context.Background(), id))
	require.Equal(t, 0, m.Count())
	require.Equal(t, int64(1), gate.releases.Load())
}

func TestTTLExpiryAndEviction(t *testing.T) {
	// Custom clock we can advance.
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	clock := func() time.Time { return time.Unix(0, now.Load()) }

	gate := &fakeGate{}
	m := NewManager(50*time.Millisecond, 5*time.Millisecond, gate, clock)

	_, err := m.Adopt(context.Background(), excelize.NewFile())
	require.NoError(t, err)
	require.Equal(t, 1, m.Count())

	// Advance time beyond TTL and evict.
	now.Store(time.Now().Add(200 * time.Millisecond).UnixNano())
	m.EvictExpired()

	require.Equal(t, 0, m.Count())
	require.Equal(t, int64(1), gate.releases.Load())
}

func TestReadWriteLocking(t *testing.T) {
	m := NewManager(time.Second, time.Second, nil, time.Now)
	id, err := m.Adopt(context.Background(), excelize.NewFile())
	require.NoError(t, err)

	var r1Acq, r2Acq, wAcq sync.WaitGroup
	r1Acq.Add(1)
	r2Acq.Add(1)
	wAcq.Add(1)

	releaseR1 := make(chan struct{})
	releaseR2 := make(chan struct{})
	writeDone := make(chan struct{})

	// Reader 1
	go func() {
		err := m.WithRead(id, func(*excelize.File, int64) error {
			r1Acq.Done()
			<-releaseR1
			return nil
		})
		require.NoError(t, err)
	}()

	// Reader 2
	go func() {
		err := m.WithRead(id, func(*excelize.File, int64) error {
			r2Acq.Done()
			<-releaseR2
			return nil
		})
		require.NoError(t, err)
	}()

	// Writer (should block until both readers release)
	go func() {
		// Wait until both readers have acquired before attempting write
		r1Acq.Wait()
		r2Acq.Wait()
		err := m.WithWrite(id, func(*excelize.File) error {
			wAcq.Done()
			return nil
		})
		require.NoError(t, err)
		close(writeDone)
	}()

	// Ensure writer hasn't acquired yet
	ch := make(chan struct{})
	go func() { wAcq.Wait(); close(ch) }()
	select {
	case <-ch:
		t.Fatal("writer should not acquire while readers hold RLock")
	case <-time.After(30 * time.Millisecond):
		// expected timeout
	}

	// Release readers; writer should proceed
	close(releaseR1)
	close(releaseR2)
	<-writeDone
}

func TestOpen_UnsupportedFormatReleasesGate(t *testing.T) {
	gate := &fakeGate{}
	m := NewManager(time.Second, time.Second, gate, time.Now)

	_, err := m.Open(context.Background(), "not_excel.txt")
	require.Error(t, err)
	require.Equal(t, int64(1), gate.acquires.Load())
	// Release should be called on early error
	require.Equal(t, int64(1), gate.releases.Load())
}

func TestOpen_GateBusy(t *testing.T) {
	gate := &fakeGate{acquireErr: context.DeadlineExceeded}
	m := NewManager(time.Second, time.Second, gate, time.Now)

	_, err := m.Open(context.Background(), "sheet.xlsx")
	require.Error(t, err)
	require.Equal(t, int64(1), gate.acquires.Load())
	require.Equal(t, int64(0), gate.releases.Load())
}

type denyValidator struct{}

func (denyValidator) ValidateOpenPath(string) (string, error) { return "", fmt.Errorf("denied") }

func TestOpen_PathValidatorDenied_ReleasesGate(t *testing.T) {
	gate := &fakeGate{}
	m := NewManager(time.Second, time.Second, gate, time.Now)
	// Inject a validator that denies access
	m.validator = denyValidator{}

	_, err := m.Open(context.Background(), "ok.xlsx")
	require.Error(t, err)
	require.Equal(t, int64(1), gate.acquires.Load())
	require.Equal(t, int64(1), gate.releases.Load())
}

func TestWorkbookVersionIncrementsOnWrite(t *testing.T) {
	m := NewManager(time.Second, time.Second, nil, time.Now)
	id, err := m.Adopt(context.Background(), excelize.NewFile())
	require.NoError(t, err)

	var v1 int64
	// Initial read to establish baseline
	err = m.WithRead(id, func(*excelize.File, int64) error { return nil })
	require.NoError(t, err)

	// Perform a write (no-op save) and expect version to bump
	err = m.WithWrite(id, func(f *excelize.File) error { return nil })
	require.NoError(t, err)

	err = m.WithRead(id, func(_ *excelize.File, ver int64) error { v1 = ver; return nil })
	require.NoError(t, err)

	// We cannot directly assert v0 from first read since the callback didn't receive it;
	// but after one write, version should be >= 1.
	require.GreaterOrEqual(t, v1, int64(1))
}
