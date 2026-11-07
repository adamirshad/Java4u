package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// horizonArchive builds a launcher archive for this platform holding a fake program (and
// any extra entries), the way Horizon's releases are laid out.
func horizonArchive(t *testing.T, extra map[string]string) []byte {
	t.Helper()
	files := map[string]string{}
	switch runtime.GOOS {
	case "darwin":
		files["Horizon.app/Contents/MacOS/horizon-launcher"] = "launcher"
		files["Horizon.app/Contents/Info.plist"] = "plist"
	case "windows":
		files["horizon-launcher.exe"] = "launcher"
	default:
		files["horizon-launcher"] = "launcher"
	}
	for k, v := range extra {
		files[k] = v
	}
	var buf bytes.Buffer
	if runtime.GOOS == "linux" {
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for name, body := range files {
			tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
			tw.Write([]byte(body))
		}
		tw.Close()
		gz.Close()
		return buf.Bytes()
	}
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o755)
		w, _ := zw.CreateHeader(h)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

// fakeHorizon serves one signed Horizon launcher release and returns it.
type fakeHorizon struct {
	archive, sums, sigs []byte
	rel                 *Release
	srv                 *httptest.Server
}

func newFakeHorizon(t *testing.T, tag string, archive []byte) *fakeHorizon {
	t.Helper()
	if !HorizonSupported() {
		t.Skip("Horizon has no launcher for this platform")
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	oldKeys := horizonKeys
	horizonKeys = []string{base64.StdEncoding.EncodeToString(pub)}

	name, _ := HorizonAssetName(runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(archive)
	f := &fakeHorizon{archive: archive}
	f.sums = []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	sig := ed25519.Sign(priv, append([]byte("horizon release "+tag+"\n"), f.sums...))
	f.sigs = []byte(base64.StdEncoding.EncodeToString(sig) + "\n")

	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/archive":
			w.Write(f.archive)
		case "/sums":
			w.Write(f.sums)
		case "/sig":
			w.Write(f.sigs)
		}
	}))
	u, _ := url.Parse(f.srv.URL)
	releaseHosts[u.Host] = true
	releaseScheme = "http"
	t.Cleanup(func() {
		f.srv.Close()
		delete(releaseHosts, u.Host)
		releaseScheme = "https"
		horizonKeys = oldKeys
	})
	f.rel = &Release{Tag: tag, Assets: []ReleaseAsset{
		{Name: name, URL: f.srv.URL + "/archive", Size: int64(len(archive))},
		{Name: "SHA256SUMS", URL: f.srv.URL + "/sums"},
		{Name: "SHA256SUMS.sig", URL: f.srv.URL + "/sig"},
	}}
	return f
}

func TestHorizonKeyIsValid(t *testing.T) {
	for _, k := range horizonKeys {
		b, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(b) != ed25519.PublicKeySize {
			t.Fatalf("%q is not an Ed25519 public key", k)
		}
	}
}

func TestVerifyHorizonSums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	old := horizonKeys
	horizonKeys = []string{base64.StdEncoding.EncodeToString(pub)}
	defer func() { horizonKeys = old }()

	sums := []byte("abc  horizon-launcher-linux-x86_64.tar.gz\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, append([]byte("horizon release launcher@1.0.0\n"), sums...)))
	if err := verifyHorizonSums("launcher@1.0.0", sums, []byte("junk\n"+sig+"\n")); err != nil {
		t.Fatalf("good signature refused: %v", err)
	}
	if verifyHorizonSums("launcher@0.9.0", sums, []byte(sig)) == nil {
		t.Fatal("signature for another version accepted")
	}
	if verifyHorizonSums("launcher@1.0.0", append(sums, 'x'), []byte(sig)) == nil {
		t.Fatal("changed checksums accepted")
	}
	_, other, _ := ed25519.GenerateKey(nil)
	bad := base64.StdEncoding.EncodeToString(ed25519.Sign(other, append([]byte("horizon release launcher@1.0.0\n"), sums...)))
	if verifyHorizonSums("launcher@1.0.0", sums, []byte(bad)) == nil {
		t.Fatal("signature from another key accepted")
	}
}

func TestLatestHorizon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "nightly", "prerelease": true},
			{"tag_name": "launcher@0.3.0", "prerelease": true},
			{"tag_name": "launcher@0.2.10"},
			{"tag_name": "launcher@0.4.0", "draft": true},
			{"tag_name": "v0.9.0"},
			{"tag_name": "launcher@0.2.9"},
		})
	}))
	defer srv.Close()
	rel, err := LatestHorizon(context.Background(), srv.Client(), srv.URL)
	if err != nil || rel.Tag != "launcher@0.2.10" {
		t.Fatalf("got %+v, %v", rel, err)
	}
}

func TestInstallHorizon(t *testing.T) {
	f := newFakeHorizon(t, "launcher@1.2.0", horizonArchive(t, nil))
	dirs := NewDirs(t.TempDir())
	ctx := context.Background()

	info, err := InstallHorizon(ctx, f.srv.Client(), dirs, f.rel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "1.2.0" {
		t.Fatalf("version %q", info.Version)
	}
	if got := InstalledHorizon(dirs); got == nil || got.Version != "1.2.0" {
		t.Fatalf("installed: %+v", got)
	}
	prog, err := HorizonProgram(dirs)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		prog = filepath.Join(prog, "Contents", "MacOS", "horizon-launcher")
	}
	st, err := os.Stat(prog)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode()&0o100 == 0 {
		t.Fatalf("launcher isn't executable: %v", st.Mode())
	}

	// An older release is refused, and the installed one stays.
	older := *f.rel
	older.Tag = "launcher@1.1.0"
	if _, err := InstallHorizon(ctx, f.srv.Client(), dirs, &older, nil); err == nil || !strings.Contains(err.Error(), "older") {
		t.Fatalf("downgrade: %v", err)
	}

	// A broken signature leaves the installed launcher alone.
	f.sigs = []byte(base64.StdEncoding.EncodeToString(make([]byte, 64)) + "\n")
	if _, err := InstallHorizon(ctx, f.srv.Client(), dirs, f.rel, nil); err == nil {
		t.Fatal("bad signature accepted")
	}
	if InstalledHorizon(dirs) == nil {
		t.Fatal("failed install removed the old launcher")
	}
}

func TestInstallHorizonRefusesBadDownloads(t *testing.T) {
	dirs := NewDirs(t.TempDir())
	ctx := context.Background()

	// Checksum mismatch: the archive changes after it was signed.
	f := newFakeHorizon(t, "launcher@1.0.0", horizonArchive(t, nil))
	f.archive = horizonArchive(t, map[string]string{"extra": "x"})
	if _, err := InstallHorizon(ctx, f.srv.Client(), dirs, f.rel, nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum mismatch: %v", err)
	}

	// A path leaving the install folder, even when signed.
	f = newFakeHorizon(t, "launcher@1.0.0", horizonArchive(t, map[string]string{"../escape": "x"}))
	// (Go's zip reader may refuse such a name before Java4U's own check does.)
	if _, err := InstallHorizon(ctx, f.srv.Client(), dirs, f.rel, nil); err == nil {
		t.Fatal("unsafe path accepted")
	}
	if exists(filepath.Join(dirs.HorizonDir(), "escape")) {
		t.Fatal("file written outside the launcher folder")
	}

	// Downloads from hosts other than GitHub's.
	f = newFakeHorizon(t, "launcher@1.0.0", horizonArchive(t, nil))
	f.rel.Assets[0].URL = "https://example.com/archive"
	if _, err := InstallHorizon(ctx, f.srv.Client(), dirs, f.rel, nil); err == nil {
		t.Fatal("download from an unknown host accepted")
	}
	if InstalledHorizon(dirs) != nil {
		t.Fatal("a refused download left a launcher installed")
	}
}

func TestEnsureHorizonUsesInstalledWhenOffline(t *testing.T) {
	f := newFakeHorizon(t, "launcher@1.0.0", horizonArchive(t, nil))
	dirs := NewDirs(t.TempDir())
	if _, err := InstallHorizon(context.Background(), f.srv.Client(), dirs, f.rel, nil); err != nil {
		t.Fatal(err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	var logged []string
	info, err := EnsureHorizon(context.Background(), down.Client(), dirs, down.URL, nil, func(s string) { logged = append(logged, s) })
	if err != nil || info.Version != "1.0.0" {
		t.Fatalf("got %+v, %v", info, err)
	}
	if len(logged) == 0 {
		t.Fatal("no message about the failed check")
	}

	// With nothing installed, the failure is reported.
	if _, err := EnsureHorizon(context.Background(), down.Client(), NewDirs(t.TempDir()), down.URL, nil, func(string) {}); err == nil {
		t.Fatal("no error with nothing installed and GitHub down")
	}
}

func TestHorizonAssetName(t *testing.T) {
	if n, err := HorizonAssetName("darwin", "arm64"); err != nil || n != "horizon-launcher-macos-arm64.zip" {
		t.Fatalf("%q %v", n, err)
	}
	if _, err := HorizonAssetName("linux", "arm64"); !errors.Is(err, ErrHorizonUnsupported) {
		t.Fatalf("linux/arm64: %v", err)
	}
}
