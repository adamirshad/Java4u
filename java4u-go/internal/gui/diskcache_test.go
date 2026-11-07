package gui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

func TestDiskCache(t *testing.T) {
	c := newDiskCache(t.TempDir())
	ctx := context.Background()
	var calls atomic.Int32
	build := func(context.Context) ([]byte, error) {
		calls.Add(1)
		return []byte(`"v` + string(rune('0'+calls.Load())) + `"`), nil
	}

	// Nothing saved: waits for build and saves the result.
	got, err := c.get(ctx, ctx, "k", time.Hour, build)
	if err != nil || string(got) != `"v1"` {
		t.Fatalf("%s %v", got, err)
	}
	// Fresh copy: no new fetch.
	got, _ = c.get(ctx, ctx, "k", time.Hour, build)
	if string(got) != `"v1"` || calls.Load() != 1 {
		t.Fatalf("fresh copy refetched: %s, %d calls", got, calls.Load())
	}

	// Stale copy: returned at once, refreshed in the background.
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(c.path("k"), old, old)
	got, _ = c.get(ctx, ctx, "k", time.Hour, build)
	if string(got) != `"v1"` {
		t.Fatalf("stale copy not served: %s", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got, _, _ := c.read("k"); string(got) != `"v2"` {
		t.Fatalf("background refresh didn't save: %s", got)
	}

	// Offline with an old copy: the old copy is better than an error.
	os.Chtimes(c.path("k"), old.Add(-30*24*time.Hour), old.Add(-30*24*time.Hour))
	got, err = c.get(ctx, ctx, "k", time.Hour, func(context.Context) ([]byte, error) { return nil, errors.New("offline") })
	if err != nil || string(got) != `"v2"` {
		t.Fatalf("offline fallback: %s %v", got, err)
	}
	// Offline with nothing saved: the error.
	if _, err := c.get(ctx, ctx, "other", time.Hour, func(context.Context) ([]byte, error) { return nil, errors.New("offline") }); err == nil {
		t.Fatal("expected an error")
	}
}

func TestDiskCacheSharesRequests(t *testing.T) {
	c := newDiskCache(t.TempDir())
	ctx := context.Background()
	var calls atomic.Int32
	release := make(chan struct{})
	build := func(context.Context) ([]byte, error) {
		calls.Add(1)
		<-release
		return []byte(`1`), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.get(ctx, ctx, "same", time.Hour, build)
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("%d fetches for one key, want 1", calls.Load())
	}
}

func TestStaticETags(t *testing.T) {
	files := fstest.MapFS{"app.js": {Data: []byte("console.log(1)")}}
	h := withETags(files, http.FileServer(http.FS(files)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app.js", nil))
	tag := rec.Header().Get("ETag")
	if rec.Code != 200 || tag == "" {
		t.Fatalf("first load: %d, etag %q", rec.Code, tag)
	}
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("If-None-Match", tag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("reload: %d, want 304", rec.Code)
	}
}
