package pdf

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testCache(t *testing.T, workers, queue int, budget int64) *Cache {
	t.Helper()
	c, err := NewCache(Config{Workers: workers, QueueSize: queue, CacheBytes: budget, TTL: 15 * time.Minute, Timeout: time.Second, CacheDir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}
func TestCacheDeduplicatesAndLeases(t *testing.T) {
	c := testCache(t, 2, 8, 100)
	var calls atomic.Int32
	start := make(chan struct{})
	gate := make(chan struct{})
	build := func(_ context.Context, p string) error {
		if calls.Add(1) == 1 {
			close(start)
		}
		<-gate
		return os.WriteFile(p, []byte("private"), 0600)
	}
	var wg sync.WaitGroup
	artifacts := make(chan Artifact, 2)
	for range 2 {
		wg.Go(func() { a, err := c.Do(context.Background(), "same", build); require.NoError(t, err); artifacts <- a })
	}
	<-start
	time.Sleep(20 * time.Millisecond)
	close(gate)
	wg.Wait()
	require.Equal(t, int32(1), calls.Load())
	a, b := <-artifacts, <-artifacts
	require.Equal(t, a.Path, b.Path)
	require.NoError(t, c.Close())
	_, err := os.Stat(a.Path)
	require.NoError(t, err)
	a.Release()
	b.Release()
	_, err = os.Stat(a.Path)
	require.True(t, os.IsNotExist(err))
}
func TestCacheTTLAndBudget(t *testing.T) {
	c := testCache(t, 2, 8, 6)
	now := time.Now()
	c.now = func() time.Time { return now }
	build := func(_ context.Context, p string) error { return os.WriteFile(p, []byte("data"), 0600) }
	a, err := c.Do(context.Background(), "a", build)
	require.NoError(t, err)
	a.Release()
	b, err := c.Do(context.Background(), "b", build)
	require.NoError(t, err)
	b.Release()
	_, err = os.Stat(a.Path)
	require.True(t, os.IsNotExist(err))
	now = now.Add(15 * time.Minute)
	b2, err := c.Do(context.Background(), "b", build)
	require.NoError(t, err)
	require.NotEqual(t, b.Path, b2.Path)
	b2.Release()
}
func TestCacheQueueAndCancellation(t *testing.T) {
	c := testCache(t, 1, 1, 100)
	started := make(chan struct{})
	gate := make(chan struct{})
	build := func(ctx context.Context, p string) error {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-gate:
			return os.WriteFile(p, []byte("ok"), 0600)
		}
	}
	done := make(chan struct{})
	go func() {
		a, e := c.Do(context.Background(), "a", build)
		if e == nil {
			a.Release()
		}
		close(done)
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error)
	go func() { _, e := c.Do(ctx, "b", build); queued <- e }()
	time.Sleep(20 * time.Millisecond)
	_, e := c.Do(context.Background(), "c", build)
	require.ErrorIs(t, e, ErrBusy)
	cancel()
	require.ErrorIs(t, <-queued, context.Canceled)
	close(gate)
	<-done
}
func TestCacheErrorAndOversizedCleanup(t *testing.T) {
	c := testCache(t, 2, 8, 2)
	var partial string
	_, e := c.Do(context.Background(), "bad", func(_ context.Context, p string) error {
		partial = p
		_ = os.WriteFile(p, []byte("bad"), 0600)
		return ErrRender
	})
	require.ErrorIs(t, e, ErrRender)
	_, e = os.Stat(partial)
	require.True(t, os.IsNotExist(e))
	a, e := c.Do(context.Background(), "big", func(_ context.Context, p string) error { return os.WriteFile(p, []byte("large"), 0600) })
	require.NoError(t, e)
	_, e = os.Stat(a.Path)
	require.NoError(t, e)
	a.Release()
	_, e = os.Stat(a.Path)
	require.True(t, os.IsNotExist(e))
}

func TestCacheOneCanceledWaiter(t *testing.T) {
	c := testCache(t, 1, 1, 100)
	ctx, cancel := context.WithCancel(context.Background())
	start := make(chan struct{})
	gate := make(chan struct{})
	canceled := make(chan error)
	var calls atomic.Int32
	build := func(ctx context.Context, p string) error {
		calls.Add(1)
		close(start)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-gate:
			return os.WriteFile(p, []byte("ok"), 0600)
		}
	}
	go func() { _, e := c.Do(ctx, "shared", build); canceled <- e }()
	<-start
	result := make(chan Artifact)
	go func() { a, e := c.Do(context.Background(), "shared", build); require.NoError(t, e); result <- a }()
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.jobs["shared"] != nil && c.jobs["shared"].waiters == 2
	}, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-canceled, context.Canceled)
	close(gate)
	a := <-result
	require.Equal(t, int32(1), calls.Load())
	a.Release()
}
func TestCacheNonCancellableKeepsSlot(t *testing.T) {
	c := testCache(t, 1, 1, 100)
	ctx, cancel := context.WithCancel(context.Background())
	start := make(chan struct{})
	gate := make(chan struct{})
	done := make(chan error)
	go func() {
		_, e := c.Do(ctx, "slow", func(_ context.Context, p string) error {
			close(start)
			<-gate
			return os.WriteFile(p, []byte("ok"), 0600)
		})
		done <- e
	}()
	<-start
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	c.mu.Lock()
	require.Len(t, c.slots, 1)
	c.mu.Unlock()
	close(gate)
}

func TestCacheDeadlineDoesNotReleaseActiveWorker(t *testing.T) {
	c := testCache(t, 1, 1, 100)
	c.cfg.Timeout = 30 * time.Millisecond
	gate := make(chan struct{})
	start := make(chan struct{})
	done := make(chan error)
	go func() {
		_, err := c.Do(context.Background(), "uncancellable", func(_ context.Context, p string) error {
			close(start)
			<-gate
			return os.WriteFile(p, []byte("ok"), 0600)
		})
		done <- err
	}()
	<-start
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrTimeout)
		require.Len(t, c.slots, 1)
		close(gate)
	case <-time.After(200 * time.Millisecond):
		close(gate)
		<-done
		t.Fatal("deadline must finish the waiting request while the worker remains occupied")
	}
}
