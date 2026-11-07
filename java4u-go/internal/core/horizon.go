package core

// Horizon (https://github.com/horizon-rust/horizon) is an open-source rewrite of Minecraft:
// Java Edition in Rust, by other developers. Java4U doesn't run Horizon's game itself:
// Horizon ships its own launcher, which signs players in, installs the game from Mojang and
// runs it. Java4U downloads that launcher, checks it, keeps it in <data>/horizon/launcher
// and opens it.
//
// Each Horizon launcher release (tag "launcher@<version>") carries SHA256SUMS and
// SHA256SUMS.sig: base64 Ed25519 signatures over "horizon release <tag>\n" followed by
// SHA256SUMS's bytes. Java4U checks the signature against Horizon's published key, then the
// download against the list, and never installs a version older than the one it has.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// HorizonReleasesURL lists Horizon's releases; launcher releases are among them.
const HorizonReleasesURL = "https://api.github.com/repos/horizon-rust/horizon/releases?per_page=50"

// HorizonPage is where players can read about Horizon.
const HorizonPage = "https://github.com/horizon-rust/horizon"

const horizonTagPrefix = "launcher@"

// horizonKeys are the Ed25519 public keys Horizon signs its releases with, copied from
// crates/horizon-release-sign/release-keys.txt in Horizon's repository. Tests change it.
var horizonKeys = []string{"MU4oiIQcCxkZvbwSjjXs7SySCuM6ywRQ3nGaJYZG0gw="}

// ErrHorizonUnsupported means Horizon has no launcher build for this computer.
var ErrHorizonUnsupported = errors.New("Horizon has no launcher for this computer")

// Size limits for what is downloaded and unpacked.
const (
	horizonMaxArchive  = 512 << 20
	horizonMaxUnpacked = 1 << 30
)

func (d Dirs) HorizonDir() string { return filepath.Join(d.Root, "horizon") }

func (d Dirs) horizonLauncherDir() string { return filepath.Join(d.HorizonDir(), "launcher") }
func (d Dirs) horizonInfoFile() string    { return filepath.Join(d.HorizonDir(), "installed.json") }

// HorizonInstall records which Horizon launcher Java4U has installed.
type HorizonInstall struct {
	Version string `json:"version"`
	Tag     string `json:"tag"`
	Asset   string `json:"asset"`
}

// HorizonAssetName is the launcher archive for a platform.
func HorizonAssetName(goos, goarch string) (string, error) {
	names := map[string]string{
		"darwin/arm64":  "horizon-launcher-macos-arm64.zip",
		"darwin/amd64":  "horizon-launcher-macos-x64.zip",
		"windows/amd64": "horizon-launcher-windows-x64.zip",
		"windows/arm64": "horizon-launcher-windows-arm64.zip",
		"linux/amd64":   "horizon-launcher-linux-x86_64.tar.gz",
	}
	if n, ok := names[goos+"/"+goarch]; ok {
		return n, nil
	}
	return "", fmt.Errorf("%w (%s/%s)", ErrHorizonUnsupported, goos, goarch)
}

// HorizonSupported reports whether Horizon has a launcher for this computer.
func HorizonSupported() bool {
	_, err := HorizonAssetName(runtime.GOOS, runtime.GOARCH)
	return err == nil
}

// horizonVersion is a launcher release's version, or "" if tag isn't a launcher release.
func horizonVersion(tag string) string {
	v, ok := strings.CutPrefix(tag, horizonTagPrefix)
	if !ok || v == "" {
		return ""
	}
	return v
}

// LatestHorizon finds Horizon's newest published launcher release.
func LatestHorizon(ctx context.Context, c *http.Client, api string) (*Release, error) {
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub: %s", resp.Status)
	}
	var rels []Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&rels); err != nil {
		return nil, err
	}
	var best *Release
	for i := range rels {
		r := &rels[i]
		v := horizonVersion(r.Tag)
		if r.Draft || r.Pre || v == "" {
			continue
		}
		if best == nil || NewerVersion(v, horizonVersion(best.Tag)) {
			best = r
		}
	}
	if best == nil {
		return nil, errors.New("Horizon has no launcher release on GitHub")
	}
	return best, nil
}

// InstalledHorizon returns the installed Horizon launcher, or nil if there is none.
func InstalledHorizon(dirs Dirs) *HorizonInstall {
	data, err := os.ReadFile(dirs.horizonInfoFile())
	if err != nil {
		return nil
	}
	var info HorizonInstall
	if json.Unmarshal(data, &info) != nil || info.Version == "" {
		return nil
	}
	if _, err := HorizonProgram(dirs); err != nil {
		return nil
	}
	return &info
}

// HorizonProgram is the installed launcher's program (Horizon.app on macOS).
func HorizonProgram(dirs Dirs) (string, error) {
	dir := dirs.horizonLauncherDir()
	var p string
	switch runtime.GOOS {
	case "darwin":
		p = filepath.Join(dir, "Horizon.app")
	case "windows":
		p = filepath.Join(dir, "horizon-launcher.exe")
	default:
		p = filepath.Join(dir, "horizon-launcher")
	}
	if _, err := os.Stat(p); err != nil {
		return "", errors.New("Horizon isn't installed")
	}
	return p, nil
}

// verifyHorizonSums checks that one of Horizon's keys signed sums for tag.
func verifyHorizonSums(tag string, sums, sigs []byte) error {
	msg := append([]byte("horizon release "+tag+"\n"), sums...)
	for _, k := range horizonKeys {
		key, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(key) != ed25519.PublicKeySize {
			continue
		}
		for _, line := range strings.Fields(string(sigs)) {
			sig, err := base64.StdEncoding.DecodeString(line)
			if err == nil && ed25519.Verify(key, msg, sig) {
				return nil
			}
		}
	}
	return fmt.Errorf("Horizon %s isn't signed with Horizon's release key, so it was not installed", tag)
}

// InstallHorizon downloads and checks rel's launcher for this computer and puts it in
// place of any older one.
func InstallHorizon(ctx context.Context, c *http.Client, dirs Dirs, rel *Release, progress Progress) (*HorizonInstall, error) {
	version := horizonVersion(rel.Tag)
	if version == "" {
		return nil, fmt.Errorf("%s is not a Horizon launcher release", rel.Tag)
	}
	if cur := InstalledHorizon(dirs); cur != nil && NewerVersion(cur.Version, version) {
		return nil, fmt.Errorf("Horizon %s is older than the installed %s; not installing it", version, cur.Version)
	}
	name, err := HorizonAssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	archive, sums, sigs := rel.asset(name), rel.asset("SHA256SUMS"), rel.asset("SHA256SUMS.sig")
	if archive == nil {
		return nil, fmt.Errorf("Horizon %s has no download for this computer (%s)", version, name)
	}
	if sums == nil || sigs == nil {
		return nil, fmt.Errorf("Horizon %s isn't signed, so it can't be checked", version)
	}

	if err := os.MkdirAll(dirs.HorizonDir(), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(dirs.HorizonDir(), "download-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	sumsPath, sigsPath := filepath.Join(tmp, "SHA256SUMS"), filepath.Join(tmp, "SHA256SUMS.sig")
	if _, err := downloadRelease(ctx, c, sums.URL, sumsPath, 1<<20, nil); err != nil {
		return nil, fmt.Errorf("downloading Horizon's checksums: %w", err)
	}
	if _, err := downloadRelease(ctx, c, sigs.URL, sigsPath, 64<<10, nil); err != nil {
		return nil, fmt.Errorf("downloading Horizon's signature: %w", err)
	}
	sumsData, err := os.ReadFile(sumsPath)
	if err != nil {
		return nil, err
	}
	sigsData, err := os.ReadFile(sigsPath)
	if err != nil {
		return nil, err
	}
	if err := verifyHorizonSums(rel.Tag, sumsData, sigsData); err != nil {
		return nil, err
	}
	want, err := checksumFor(sumsPath, name)
	if err != nil {
		return nil, err
	}

	archivePath := filepath.Join(tmp, name)
	got, err := downloadRelease(ctx, c, archive.URL, archivePath, horizonMaxArchive, func(done int64) {
		report(progress, "Horizon", int(done>>10), int(archive.Size>>10))
	})
	if err != nil {
		return nil, fmt.Errorf("downloading Horizon: %w", err)
	}
	if !strings.EqualFold(got, want) {
		return nil, fmt.Errorf("Horizon %s failed its checksum; not installing it", version)
	}

	staged := filepath.Join(tmp, "launcher")
	if strings.HasSuffix(name, ".tar.gz") {
		err = extractTarGz(archivePath, staged)
	} else {
		err = extractZip(archivePath, staged)
	}
	if err != nil {
		return nil, fmt.Errorf("unpacking Horizon: %w", err)
	}

	// Swap the new launcher in, keeping the old one until the new one is in place.
	dest := dirs.horizonLauncherDir()
	old := dest + ".old"
	os.RemoveAll(old)
	if exists(dest) {
		if err := os.Rename(dest, old); err != nil {
			return nil, fmt.Errorf("can't replace the installed Horizon (is it still open?): %w", err)
		}
	}
	if err := os.Rename(staged, dest); err != nil {
		if exists(old) {
			os.Rename(old, dest)
		}
		return nil, err
	}
	os.RemoveAll(old)

	if _, err := HorizonProgram(dirs); err != nil {
		os.RemoveAll(dest)
		return nil, fmt.Errorf("Horizon %s's download has no launcher in it", version)
	}
	info := &HorizonInstall{Version: version, Tag: rel.Tag, Asset: name}
	data, _ := json.MarshalIndent(info, "", "  ")
	if err := os.WriteFile(dirs.horizonInfoFile(), data, 0o644); err != nil {
		return nil, err
	}
	return info, nil
}

// EnsureHorizon installs Horizon's launcher, or updates it when a newer one is out. If
// GitHub can't be reached, an installed launcher is used as it is.
func EnsureHorizon(ctx context.Context, c *http.Client, dirs Dirs, api string, progress Progress, log func(string)) (*HorizonInstall, error) {
	if !HorizonSupported() {
		_, err := HorizonAssetName(runtime.GOOS, runtime.GOARCH)
		return nil, err
	}
	cur := InstalledHorizon(dirs)
	rel, err := LatestHorizon(ctx, c, api)
	if err != nil {
		if cur != nil {
			log(fmt.Sprintf("Couldn't check for a newer Horizon (%v); using %s.", err, cur.Version))
			return cur, nil
		}
		return nil, fmt.Errorf("finding Horizon's launcher: %w", err)
	}
	latest := horizonVersion(rel.Tag)
	if cur != nil && !NewerVersion(latest, cur.Version) {
		return cur, nil
	}
	if cur == nil {
		log(fmt.Sprintf("Installing Horizon launcher %s...", latest))
	} else {
		log(fmt.Sprintf("Updating Horizon launcher %s to %s...", cur.Version, latest))
	}
	info, err := InstallHorizon(ctx, c, dirs, rel, progress)
	if err != nil {
		if cur != nil {
			log(fmt.Sprintf("Couldn't update Horizon (%v); using %s.", err, cur.Version))
			return cur, nil
		}
		return nil, err
	}
	log(fmt.Sprintf("Horizon launcher %s is installed and checked.", info.Version))
	return info, nil
}

// OpenHorizon starts the installed Horizon launcher. It runs on its own; Java4U doesn't
// wait for it.
func OpenHorizon(dirs Dirs) error {
	prog, err := HorizonProgram(dirs)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", prog)
	} else {
		cmd = exec.Command(prog)
		cmd.Dir = filepath.Dir(prog)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// unpackTarget checks an archive entry's name and returns where it goes under dir.
func unpackTarget(dir, name string) (string, error) {
	name = strings.TrimSuffix(name, "/")
	if !isSafeRel(name) || strings.Contains(name, ":") {
		return "", fmt.Errorf("unsafe path in archive: %s", name)
	}
	return filepath.Join(dir, filepath.FromSlash(name)), nil
}

// writeUnpacked copies r to path with the archive's permissions (executable or not),
// adding the bytes to *total and failing once it passes horizonMaxUnpacked.
func writeUnpacked(path string, r io.Reader, mode os.FileMode, total *int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if mode&0o111 != 0 {
		perm = 0o755
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, horizonMaxUnpacked-*total+1))
	*total += n
	if err == nil && *total > horizonMaxUnpacked {
		err = errors.New("archive unpacks to more than expected")
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// extractZip unpacks a zip's folders and regular files into dir. Links are refused.
func extractZip(zipPath, dir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	var total int64
	for _, f := range zr.File {
		mode := f.Mode()
		if strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		dest, err := unpackTarget(dir, f.Name)
		if err != nil {
			return err
		}
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case mode.IsRegular():
			r, err := f.Open()
			if err != nil {
				return err
			}
			err = writeUnpacked(dest, r, mode, &total)
			r.Close()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive has a link or special file: %s", f.Name)
		}
	}
	return nil
}

// extractTarGz unpacks a .tar.gz's folders and regular files into dir. Links are refused.
func extractTarGz(path, dir string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Name == "." || h.Name == "./" {
			continue
		}
		dest, err := unpackTarget(dir, strings.TrimPrefix(h.Name, "./"))
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeUnpacked(dest, tr, h.FileInfo().Mode(), &total); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive has a link or special file: %s", h.Name)
		}
	}
}
