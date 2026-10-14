package pdf

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type cacheEntry struct {
	path, key string
	size      int64
	expires   time.Time
	used      uint64
	refs      int
	cached    bool
}
type flight struct {
	ctx     context.Context
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	entry   *cacheEntry
	err     error
}
type Cache struct {
	mu      sync.Mutex
	cfg     Config
	dir     string
	entries map[string]*cacheEntry
	jobs    map[string]*flight
	slots   chan struct{}
	bytes   int64
	seq     uint64
	closed  bool
	now     func() time.Time
	wg      sync.WaitGroup
}

func NewCache(cfg Config) (*Cache, error) {
	if cfg.Workers <= 0 || cfg.QueueSize < 0 || cfg.CacheBytes <= 0 || cfg.TTL <= 0 || cfg.Timeout <= 0 {
		return nil, fmt.Errorf("invalid PDF cache configuration")
	}
	if cfg.CacheDir != "" {
		if e := os.MkdirAll(cfg.CacheDir, 0700); e != nil {
			return nil, e
		}
	}
	dir, e := os.MkdirTemp(cfg.CacheDir, "portal-pdf-")
	if e != nil {
		return nil, e
	}
	return &Cache{cfg: cfg, dir: dir, entries: map[string]*cacheEntry{}, jobs: map[string]*flight{}, slots: make(chan struct{}, cfg.Workers), now: time.Now}, nil
}
func (c *Cache) drop(e *cacheEntry) {
	if e.cached {
		delete(c.entries, e.key)
		c.bytes -= e.size
		e.cached = false
	}
	if e.refs == 0 {
		_ = os.Remove(e.path)
	}
}
func (c *Cache) prune() {
	now := c.now()
	for _, e := range c.entries {
		if !now.Before(e.expires) {
			c.drop(e)
		}
	}
	for c.bytes > c.cfg.CacheBytes {
		var oldest *cacheEntry
		for _, e := range c.entries {
			if oldest == nil || e.used < oldest.used {
				oldest = e
			}
		}
		if oldest == nil {
			break
		}
		c.drop(oldest)
	}
}
func (c *Cache) lease(e *cacheEntry) Artifact {
	e.refs++
	c.seq++
	e.used = c.seq
	var once sync.Once
	return Artifact{Path: e.path, Size: e.size, Key: e.key, Release: func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			e.refs--
			if !e.cached && e.refs == 0 {
				_ = os.Remove(e.path)
			}
			if c.closed {
				_ = os.Remove(c.dir)
			}
		})
	}, retain: func() Artifact { c.mu.Lock(); defer c.mu.Unlock(); return c.lease(e) }}
}
func (c *Cache) Do(ctx context.Context, key string, build func(context.Context, string) error) (Artifact, error) {
	return c.do(ctx, key, build, nil)
}

// do transfers cleanup ownership only to a newly admitted job, including cancellation.
func (c *Cache) do(ctx context.Context, key string, build func(context.Context, string) error, cleanup func()) (Artifact, error) {
	transferred := false
	defer func() {
		if !transferred && cleanup != nil {
			cleanup()
		}
	}()
	if e := ctx.Err(); e != nil {
		return Artifact{}, e
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Artifact{}, ErrBusy
	}
	c.prune()
	if e := c.entries[key]; e != nil {
		a := c.lease(e)
		c.mu.Unlock()
		return a, nil
	}
	j := c.jobs[key]
	if j == nil {
		if len(c.jobs) >= c.cfg.Workers+c.cfg.QueueSize {
			c.mu.Unlock()
			return Artifact{}, ErrBusy
		}
		jobctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.Timeout)
		j = &flight{done: make(chan struct{}), cancel: cancel, ctx: jobctx}
		c.jobs[key] = j
		c.wg.Add(1)
		transferred = true
		go c.run(jobctx, key, j, build, cleanup)
	}
	j.waiters++
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		c.leave(key, j)
		return Artifact{}, ctx.Err()
	case <-j.ctx.Done():
		// A completed job cancels its context too; prefer its published result.
		select {
		case <-j.done:
		default:
			c.leave(key, j)
			if j.ctx.Err() == context.DeadlineExceeded {
				return Artifact{}, ErrTimeout
			}
			return Artifact{}, j.ctx.Err()
		}
	case <-j.done:
	}
	c.mu.Lock()
	var a Artifact
	if j.err == nil && ctx.Err() == nil {
		a = c.lease(j.entry)
	}
	err := j.err
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	c.leaveLocked(key, j)
	c.mu.Unlock()
	return a, err
}
func (c *Cache) leave(key string, j *flight) { c.mu.Lock(); defer c.mu.Unlock(); c.leaveLocked(key, j) }
func (c *Cache) leaveLocked(key string, j *flight) {
	j.waiters--
	if j.waiters == 0 {
		j.cancel()
		if j.entry != nil {
			j.entry.refs--
			if !j.entry.cached && j.entry.refs == 0 {
				_ = os.Remove(j.entry.path)
			}
		}
	}
}
func (c *Cache) run(ctx context.Context, key string, j *flight, build func(context.Context, string) error, cleanup func()) {
	defer c.wg.Done()
	if cleanup != nil {
		defer cleanup()
	}
	var err error
	var path string
	var size int64
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err == nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		} else {
			var f *os.File
			f, err = os.CreateTemp(c.dir, "build-")
			if err == nil {
				path = f.Name()
				err = f.Close()
			}
			if err == nil {
				err = build(ctx, path)
			}
			if err == nil {
				err = ctx.Err()
			}
			if err == nil {
				var info os.FileInfo
				info, err = os.Stat(path)
				if err == nil {
					size = info.Size()
					if size == 0 {
						err = ErrRender
					}
				}
			}
		}
	}
	if err == context.DeadlineExceeded {
		err = ErrTimeout
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	j.err = err
	delete(c.jobs, key)
	if err == nil {
		sum := sha256.Sum256([]byte(key))
		published := filepath.Join(c.dir, fmt.Sprintf("%x-%s", sum[:8], filepath.Base(path)))
		if err = os.Rename(path, published); err == nil {
			e := &cacheEntry{path: published, key: key, size: size, expires: c.now().Add(c.cfg.TTL), used: c.seq + 1, refs: 1}
			c.seq++
			j.entry = e
			if !c.closed && j.waiters > 0 && size <= c.cfg.CacheBytes {
				e.cached = true
				c.entries[key] = e
				c.bytes += size
				c.prune()
			}
			if j.waiters == 0 {
				e.refs--
				c.drop(e)
			}
		} else {
			j.err = err
		}
	}
	if j.err != nil && path != "" {
		_ = os.Remove(path)
	}
	close(j.done)
	j.cancel()
}
func (c *Cache) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	for _, j := range c.jobs {
		j.cancel()
	}
	for _, e := range c.entries {
		c.drop(e)
	}
	c.mu.Unlock()
	c.wg.Wait()
	_ = os.Remove(c.dir)
	return nil
}
