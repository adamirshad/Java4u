package core

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	concurrency = 16
	maxAttempts = 3
	// UnknownSize marks a DownloadTask whose size is not known in advance.
	UnknownSize int64 = -1
)

// Progress is called with (stage name, completed, total). It may be called from
// several goroutines, but never concurrently.
type Progress func(stage string, done, total int)

func report(p Progress, stage string, done, total int) {
	if p != nil {
		p(stage, done, total)
	}
}

// Verify decides how an existing file is judged good enough to skip.
type Verify int

const (
	// VerifyHash compares SHA1 (slow, catches corruption).
	VerifyHash Verify = iota
	// VerifySize compares file size only (fast; used for already-verified installs).
	VerifySize
)

type DownloadTask struct {
	URL  string
	Dest string
	SHA1 string // empty when unknown
	Size int64  // UnknownSize when unknown
}

// HashMismatchError means a downloaded file did not have the expected SHA1.
type HashMismatchError struct {
	Path, Expected, Actual string
}

func (e *HashMismatchError) Error() string {
	return fmt.Sprintf("hash mismatch for %s: expected %s, got %s", e.Path, e.Expected, e.Actual)
}

type uaTransport struct{ base http.RoundTripper }

func (t uaTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("User-Agent") == "" {
		r = r.Clone(r.Context())
		r.Header.Set("User-Agent", "java4u/"+Version)
	}
	return t.base.RoundTrip(r)
}

// NewClient returns the HTTP client used for all of Java4U's network work.
func NewClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	tr.MaxIdleConnsPerHost = concurrency
	return &http.Client{Transport: uaTransport{tr}}
}

func getJSON(ctx context.Context, c *http.Client, url string, out any) error {
	body, err := getBytes(ctx, c, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func getBytes(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isValid reports whether the destination exists and matches the expected hash (or size).
func isValid(t DownloadTask, verify Verify) bool {
	st, err := os.Stat(t.Dest)
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	if verify == VerifySize {
		return t.Size == UnknownSize || st.Size() == t.Size
	}
	if t.SHA1 != "" {
		actual, err := sha1File(t.Dest)
		return err == nil && strings.EqualFold(actual, t.SHA1)
	}
	if t.Size != UnknownSize {
		return st.Size() == t.Size
	}
	return true
}

// fetch downloads into a .part file, checks the hash, then renames into place.
func fetch(ctx context.Context, c *http.Client, t DownloadTask) error {
	if err := os.MkdirAll(filepath.Dir(t.Dest), 0o755); err != nil {
		return err
	}
	part := t.Dest + ".part"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET %s: %s", t.URL, resp.Status)
	}

	f, err := os.Create(part)
	if err != nil {
		return err
	}
	h := sha1.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(part)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}

	if t.SHA1 != "" {
		actual := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(actual, t.SHA1) {
			os.Remove(part)
			return &HashMismatchError{Path: t.Dest, Expected: t.SHA1, Actual: actual}
		}
	}
	return os.Rename(part, t.Dest)
}

// Ensure downloads one file unless it is already valid. Retries with backoff.
func Ensure(ctx context.Context, c *http.Client, t DownloadTask, verify Verify) error {
	if isValid(t, verify) {
		return nil
	}
	for attempt := 1; ; attempt++ {
		err := fetch(ctx, c, t)
		if err == nil {
			return nil
		}
		if attempt >= maxAttempts || ctx.Err() != nil {
			return err
		}
		log.Printf("download of %s failed (attempt %d): %v", t.URL, attempt, err)
		select {
		case <-time.After(time.Duration(500*attempt) * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// DownloadAll downloads many files concurrently, skipping ones that are already valid.
// The first failure cancels the rest.
func DownloadAll(ctx context.Context, c *http.Client, tasks []DownloadTask, stage string, progress Progress, verify Verify) error {
	seen := make(map[string]bool, len(tasks))
	uniq := make([]DownloadTask, 0, len(tasks))
	for _, t := range tasks {
		if !seen[t.Dest] {
			seen[t.Dest] = true
			uniq = append(uniq, t)
		}
	}
	total := len(uniq)
	report(progress, stage, 0, total)
	if total == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		firstErr error
		done     int
		wg       sync.WaitGroup
	)
	jobs := make(chan DownloadTask)
	for i := 0; i < min(concurrency, total); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				err := Ensure(ctx, c, t, verify)
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
						cancel()
					}
				} else {
					done++
					report(progress, stage, done, total)
				}
				mu.Unlock()
			}
		}()
	}

feed:
	for _, t := range uniq {
		select {
		case jobs <- t:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
