package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.0", "0.2.0", false},
		{"v1.0.0", "0.9.9", true},
		{"0.1.0", "0.1.0-dev", true},
		{"0.1.0-dev", "0.1.0", false},
		{"0.1.0", "0.1.0", false},
		{"0.10.0", "0.9.0", true},
		{"0.1.1-rc.2", "0.1.1-rc.1", true},
	}
	for _, c := range cases {
		if got := NewerVersion(c.a, c.b); got != c.want {
			t.Errorf("NewerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func TestApplyUpdate(t *testing.T) {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	asset := UpdateAssetName(runtime.GOOS, runtime.GOARCH)
	folder := fmt.Sprintf("java4u-%s-%s/", runtime.GOOS, runtime.GOARCH)
	zipData := makeZip(t, map[string]string{folder + "java4u" + ext: "new cli", folder + "java4u-gui" + ext: "new gui", folder + "README.md": "hi"})
	sum := sha256.Sum256(zipData)
	good := hex.EncodeToString(sum[:]) + "  " + asset + "\n"

	pub, priv, _ := ed25519.GenerateKey(nil)
	oldKeys := ReleaseKeys
	ReleaseKeys = []string{base64.StdEncoding.EncodeToString(pub)}
	defer func() { ReleaseKeys = oldKeys }()
	signFor := func(tag, list string) string { return SignJava4URelease(priv, tag, []byte(list)) + "\n" }

	sums := good
	sig := signFor("v9.9.9", good)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zip":
			w.Write(zipData)
		case "/sums":
			w.Write([]byte(sums))
		case "/sig":
			w.Write([]byte(sig))
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	releaseHosts[u.Host] = true
	releaseScheme = "http"
	defer func() { delete(releaseHosts, u.Host); releaseScheme = "https" }()

	rel := &Release{Tag: "v9.9.9", Assets: []ReleaseAsset{{Name: asset, URL: srv.URL + "/zip"}, {Name: "checksums.txt", URL: srv.URL + "/sums"}, {Name: ReleaseSigName, URL: srv.URL + "/sig"}}}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "java4u-gui"+ext), []byte("old gui"), 0o755)

	if err := ApplyUpdate(context.Background(), srv.Client(), rel, dir, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "java4u-gui"+ext)); string(b) != "new gui" {
		t.Fatalf("gui not replaced: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "java4u-gui"+ext+".old")); string(b) != "old gui" {
		t.Fatalf("old gui not kept: %q", b)
	}
	if exists(filepath.Join(dir, "java4u"+ext)) {
		t.Fatal("CLI was added though it wasn't installed")
	}

	// A bad checksum must leave everything alone, even when the list is signed.
	sums = "0000000000000000000000000000000000000000000000000000000000000000  " + asset + "\n"
	sig = signFor("v9.9.9", sums)
	if err := ApplyUpdate(context.Background(), srv.Client(), rel, dir, nil); err == nil {
		t.Fatal("bad checksum accepted")
	}

	// Signatures for another version, by another key, or missing are refused.
	sums = good
	_, other, _ := ed25519.GenerateKey(nil)
	for name, s := range map[string]string{
		"other version": signFor("v9.9.8", good),
		"other key":     SignJava4URelease(other, "v9.9.9", []byte(good)),
		"garbage":       "not a signature",
	} {
		sig = s
		if err := ApplyUpdate(context.Background(), srv.Client(), rel, dir, nil); err == nil || !strings.Contains(err.Error(), "signed") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	unsigned := &Release{Tag: rel.Tag, Assets: rel.Assets[:2]}
	if err := ApplyUpdate(context.Background(), srv.Client(), unsigned, dir, nil); err == nil || !strings.Contains(err.Error(), "signed") {
		t.Fatalf("unsigned release: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "java4u-gui"+ext)); string(b) != "new gui" {
		t.Fatalf("file changed after a failed update: %q", b)
	}

	// Downloads from other hosts are refused.
	rel.Assets[0].URL = "https://example.com/zip"
	sums, sig = good, signFor("v9.9.9", good)
	if err := ApplyUpdate(context.Background(), srv.Client(), rel, dir, nil); err == nil {
		t.Fatal("download from an unknown host accepted")
	}
}

func TestLatestRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.3.0", "html_url": "https://github.com/x", "assets": []any{}})
	}))
	defer srv.Close()
	rel, err := LatestRelease(context.Background(), srv.Client(), srv.URL)
	if err != nil || rel.Version() != "0.3.0" {
		t.Fatalf("%+v %v", rel, err)
	}
}
