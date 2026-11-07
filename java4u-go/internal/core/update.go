package core

// Self-update from GitHub Releases. Each release has one zip per platform
// (java4u-<os>-<arch>.zip holding java4u and java4u-gui), checksums.txt with their SHA-256
// sums, and checksums.txt.sig signing that list (see signing.go). Updating checks the
// signature, downloads the zip, checks its sum, and swaps the binaries next to the running
// one; the old files are kept as *.old until the next start. Unsigned releases are refused.

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrNoReleases means the project has not published any release yet.
var ErrNoReleases = errors.New("no Java4U releases have been published yet")

// ReleasesURL is the GitHub API address of Java4U's newest release.
const ReleasesURL = "https://api.github.com/repos/adamirshad/Java4u/releases/latest"

// releaseScheme is the only scheme updates are downloaded over. Tests change it.
var releaseScheme = "https"

// Hosts release files may be downloaded from (GitHub redirects asset downloads).
var releaseHosts = map[string]bool{
	"github.com":                            true,
	"objects.githubusercontent.com":         true,
	"release-assets.githubusercontent.com":  true,
	"github-releases.githubusercontent.com": true,
}

type ReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type Release struct {
	Tag    string         `json:"tag_name"`
	Page   string         `json:"html_url"`
	Notes  string         `json:"body"`
	Draft  bool           `json:"draft"`
	Pre    bool           `json:"prerelease"`
	Assets []ReleaseAsset `json:"assets"`
}

// Version is the release's version without the leading "v".
func (r *Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

func (r *Release) asset(name string) *ReleaseAsset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// LatestRelease asks GitHub for the newest published release.
func LatestRelease(ctx context.Context, c *http.Client, api string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoReleases
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub: %s", resp.Status)
	}
	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.Tag == "" {
		return nil, errors.New("GitHub sent a release with no version")
	}
	return &rel, nil
}

// NewerVersion reports whether version a is newer than b. Versions look like 1.2.3 with an
// optional pre-release part (1.2.3-dev), which sorts before the plain release.
func NewerVersion(a, b string) bool {
	pa, preA := splitVersion(a)
	pb, preB := splitVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	switch {
	case preA == preB:
		return false
	case preA == "":
		return true // 1.2.3 is newer than 1.2.3-dev
	case preB == "":
		return false
	}
	return preA > preB
}

func splitVersion(v string) ([3]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out, pre
}

// UpdateAssetName is the release zip for a platform.
func UpdateAssetName(goos, goarch string) string {
	return fmt.Sprintf("java4u-%s-%s.zip", goos, goarch)
}

// binaryNames are the files a release zip may update.
func binaryNames() []string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return []string{"java4u" + ext, "java4u-gui" + ext}
}

// ApplyUpdate downloads rel for this platform, checks it and replaces the Java4U binaries
// in exeDir (the folder of the running program when empty).
func ApplyUpdate(ctx context.Context, c *http.Client, rel *Release, exeDir string, progress Progress) error {
	name := UpdateAssetName(runtime.GOOS, runtime.GOARCH)
	zipAsset, sums, sigs := rel.asset(name), rel.asset("checksums.txt"), rel.asset(ReleaseSigName)
	if zipAsset == nil {
		return fmt.Errorf("release %s has no download for this computer (%s)", rel.Tag, name)
	}
	if sums == nil || sigs == nil {
		return fmt.Errorf("release %s isn't signed, so it can't be checked; not installing it", rel.Tag)
	}
	if exeDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return err
		}
		exeDir = filepath.Dir(exe)
	}

	tmp, err := os.MkdirTemp("", "java4u-update")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	sumsPath := filepath.Join(tmp, "checksums.txt")
	if _, err := downloadRelease(ctx, c, sums.URL, sumsPath, 1<<20, nil); err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	sigsPath := filepath.Join(tmp, ReleaseSigName)
	if _, err := downloadRelease(ctx, c, sigs.URL, sigsPath, 64<<10, nil); err != nil {
		return fmt.Errorf("downloading the signature: %w", err)
	}
	sumsData, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	sigsData, err := os.ReadFile(sigsPath)
	if err != nil {
		return err
	}
	if err := VerifyJava4URelease(rel.Tag, sumsData, sigsData); err != nil {
		return err
	}
	want, err := checksumFor(sumsPath, name)
	if err != nil {
		return err
	}
	zipPath := filepath.Join(tmp, name)
	got, err := downloadRelease(ctx, c, zipAsset.URL, zipPath, 512<<20, func(done int64) {
		if progress != nil {
			progress("update", int(done>>10), int(zipAsset.Size>>10))
		}
	})
	if err != nil {
		return fmt.Errorf("downloading %s: %w", name, err)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%s failed its checksum; not installing it", name)
	}

	// Pull the binaries out of the zip, then swap them in.
	staged, err := extractBinaries(zipPath, tmp)
	if err != nil {
		return err
	}
	if len(staged) == 0 {
		return fmt.Errorf("%s has no Java4U programs in it", name)
	}
	return swapBinaries(staged, exeDir)
}

func downloadRelease(ctx context.Context, c *http.Client, src, dest string, limit int64, onProgress func(int64)) (string, error) {
	u, err := url.Parse(src)
	if err != nil || u.Scheme != releaseScheme || !releaseHosts[u.Host] {
		return "", fmt.Errorf("unexpected download address %q", src)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server said %s", resp.Status)
	}
	if final := resp.Request.URL; final.Scheme != releaseScheme || !releaseHosts[final.Host] {
		return "", fmt.Errorf("download was redirected to %s", final.Host)
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	body := io.LimitReader(resp.Body, limit+1)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return "", err
			}
			h.Write(buf[:n])
			done += int64(n)
			if done > limit {
				return "", errors.New("file is larger than expected")
			}
			if onProgress != nil {
				onProgress(done)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), f.Close()
}

// checksumFor reads a sha256sum-style file ("<hex>  <name>") and returns name's sum.
func checksumFor(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && len(fields[0]) == 64 {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

// extractBinaries copies java4u and java4u-gui out of the zip (from any folder in it)
// into dir and returns their paths by file name.
func extractBinaries(zipPath, dir string) (map[string]string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	wanted := map[string]bool{}
	for _, n := range binaryNames() {
		wanted[n] = true
	}
	out := map[string]string{}
	for _, f := range zr.File {
		base := filepath.Base(filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() || !wanted[base] {
			continue
		}
		dest := filepath.Join(dir, "new-"+base)
		if err := copyZipEntry(f, dest); err != nil {
			return nil, err
		}
		os.Chmod(dest, 0o755)
		out[base] = dest
	}
	return out, nil
}

// swapBinaries moves each staged file over its namesake in exeDir. A running program can
// be renamed but not overwritten on Windows, so the current file is renamed to *.old
// first. If anything fails, the files already swapped are put back.
func swapBinaries(staged map[string]string, exeDir string) error {
	type done struct{ target, old string }
	var swapped []done
	rollback := func() {
		for i := len(swapped) - 1; i >= 0; i-- {
			os.Remove(swapped[i].target)
			if swapped[i].old != "" {
				os.Rename(swapped[i].old, swapped[i].target)
			}
		}
	}
	for name, src := range staged {
		target := filepath.Join(exeDir, name)
		old := ""
		if exists(target) {
			old = target + ".old"
			os.Remove(old)
			if err := os.Rename(target, old); err != nil {
				rollback()
				return fmt.Errorf("can't replace %s (is Java4U installed somewhere you can't write to?): %w", name, err)
			}
		} else {
			continue // only update the programs that are already there
		}
		if err := moveFile(src, target); err != nil {
			if old != "" {
				os.Rename(old, target)
			}
			rollback()
			return err
		}
		swapped = append(swapped, done{target, old})
	}
	return nil
}

func moveFile(src, dest string) error {
	if os.Rename(src, dest) == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// CleanOldBinaries removes *.old files left by an earlier update. Call it at start-up.
func CleanOldBinaries() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	for _, n := range binaryNames() {
		os.Remove(filepath.Join(dir, n+".old"))
	}
}
