package gui

// A small on-disk cache for content from the internet (news, Modrinth lists, skins), so
// the app shows the last copy straight away when it opens and refreshes it in the
// background, instead of waiting on the network every time. It also works offline.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// staleLimit is how old a saved copy may be and still be shown while a fresh one loads.
const staleLimit = 7 * 24 * time.Hour

type diskCache struct {
	dir string

	mu       sync.Mutex
	inflight map[string]*flight
}

type flight struct {
	done chan struct{}
	data []byte
	err  error
}

func newDiskCache(dir string) *diskCache {
	return &diskCache{dir: dir, inflight: map[string]*flight{}}
}

func (c *diskCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:12]))
}

func (c *diskCache) read(key string) ([]byte, time.Time, bool) {
	p := c.path(key)
	info, err := os.Stat(p)
	if err != nil {
		return nil, time.Time{}, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, time.Time{}, false
	}
	return data, info.ModTime(), true
}

func (c *diskCache) write(key string, data []byte) {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return
	}
	p := c.path(key)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, p)
	}
}

// load runs build once at a time per key and saves its result.
func (c *diskCache) load(ctx context.Context, key string, build func(context.Context) ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	if f, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-f.done:
			return f.data, f.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f := &flight{done: make(chan struct{})}
	c.inflight[key] = f
	c.mu.Unlock()

	f.data, f.err = build(ctx)
	if f.err == nil {
		c.write(key, f.data)
	}
	c.mu.Lock()
	delete(c.inflight, key)
	c.mu.Unlock()
	close(f.done)
	return f.data, f.err
}

// get returns the cached bytes for key. A copy younger than fresh is returned as is. An
// older copy (up to staleLimit) is returned straight away and refreshed in the background
// using bg as the context. With no usable copy, it waits for build.
func (c *diskCache) get(ctx, bg context.Context, key string, fresh time.Duration, build func(context.Context) ([]byte, error)) ([]byte, error) {
	data, at, ok := c.read(key)
	age := time.Since(at)
	switch {
	case ok && age < fresh:
		return data, nil
	case ok && age < staleLimit:
		go func() {
			ctx, cancel := context.WithTimeout(bg, 30*time.Second)
			defer cancel()
			c.load(ctx, key, build)
		}()
		return data, nil
	}
	got, err := c.load(ctx, key, build)
	if err != nil && ok {
		return data, nil // offline: an old copy beats nothing
	}
	return got, err
}
