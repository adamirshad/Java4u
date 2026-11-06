// Package chunky installs Chunky, Java4U's own mod loader, and prepares launches with it.
//
// Chunky's jar is built from ../../chunky by tools/buildchunky and embedded here. Its
// runtime libraries (Mixin, MixinExtras and ASM) are downloaded like game libraries and
// checked against pinned SHA-1 hashes.
package chunky

import (
	"bytes"
	"context"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

// Version is the Chunky version embedded in this build.
const Version = "0.2.0"

// MainClass is Chunky's entry point.
const MainClass = "dev.java4u.chunky.Main"

//go:embed all:dist
var dist embed.FS

// Library is a runtime dependency of Chunky.
type Library struct {
	Path string // maven-style path below the libraries folder
	URL  string
	SHA1 string
	Size int64
}

// Libraries are Chunky's runtime dependencies, pinned by hash.
var Libraries = []Library{
	lib("https://maven.fabricmc.net", "net.fabricmc", "sponge-mixin", "0.17.5+mixin.0.8.7", "0cef8519a91129d0c21fa03bb9378f0e8ca810e5", 1551593),
	lib("https://repo1.maven.org/maven2", "io.github.llamalad7", "mixinextras-common", "0.5.5", "50d37e763fe461b5e9df961436b399e9b681ac35", 725859),
	lib("https://repo1.maven.org/maven2", "org.ow2.asm", "asm", "9.11.1", "edb29aa0c2da2918ce338fb7b218c21ea138dde2", 131138),
	lib("https://repo1.maven.org/maven2", "org.ow2.asm", "asm-tree", "9.11.1", "ad2b01f445d65aa84cb65900d1c848d121843986", 51957),
	lib("https://repo1.maven.org/maven2", "org.ow2.asm", "asm-commons", "9.11.1", "3707c04c3f7197c3d1d8dbc728b791a58809a59e", 77923),
	lib("https://repo1.maven.org/maven2", "org.ow2.asm", "asm-util", "9.11.1", "8694259cfbd6a5cbc65148b278b4d07af46a57d7", 98085),
	lib("https://repo1.maven.org/maven2", "org.ow2.asm", "asm-analysis", "9.11.1", "244c56fd08d71626e3c5de4af1d82826a394db77", 39657),
}

func lib(repo, group, artifact, version, sha string, size int64) Library {
	p := fmt.Sprintf("%s/%s/%s/%s-%s.jar", strings.ReplaceAll(group, ".", "/"), artifact, version, artifact, version)
	return Library{Path: p, URL: repo + "/" + p, SHA1: sha, Size: size}
}

// Jar returns the embedded Chunky jar, or false if this build doesn't include one.
func Jar() ([]byte, bool) {
	b, err := dist.ReadFile("dist/chunky.jar")
	return b, err == nil && len(b) > 0
}

// ErrNotBuilt means Java4U was built without running tools/buildchunky first.
var ErrNotBuilt = errors.New("this build of Java4U doesn't include Chunky; build it with `go run ./tools/buildchunky` and rebuild")

// SupportsVersion reports whether Chunky can run a Minecraft version. Chunky needs the
// unobfuscated releases that started with 26.1, so it never has to remap names.
func SupportsVersion(id string) bool {
	return core.ChunkySupports(id)
}

// Prepare installs Chunky and its libraries and returns what core needs to launch with it.
func Prepare(ctx context.Context, c *http.Client, dirs core.Dirs, progress core.Progress) (*core.ChunkyLaunch, error) {
	jar, ok := Jar()
	if !ok {
		return nil, ErrNotBuilt
	}
	sum := sha1.Sum(jar)
	jarPath := filepath.Join(dirs.Root, "chunky", "chunky-"+Version+"-"+hex.EncodeToString(sum[:4])+".jar")
	if existing, err := os.ReadFile(jarPath); err != nil || !bytes.Equal(existing, jar) {
		if err := os.MkdirAll(filepath.Dir(jarPath), 0o755); err != nil {
			return nil, err
		}
		tmp := jarPath + ".tmp"
		if err := os.WriteFile(tmp, jar, 0o644); err != nil {
			return nil, err
		}
		if err := os.Rename(tmp, jarPath); err != nil {
			return nil, err
		}
	}

	tasks := make([]core.DownloadTask, 0, len(Libraries))
	paths := make([]string, 0, len(Libraries))
	for _, l := range Libraries {
		dest := filepath.Join(dirs.LibrariesDir(), filepath.FromSlash(l.Path))
		tasks = append(tasks, core.DownloadTask{URL: l.URL, Dest: dest, SHA1: l.SHA1, Size: l.Size})
		paths = append(paths, dest)
	}
	if err := core.DownloadAll(ctx, c, tasks, "chunky", progress, core.VerifyHash); err != nil {
		return nil, fmt.Errorf("installing Chunky's libraries: %w", err)
	}
	return &core.ChunkyLaunch{Jar: jarPath, Libraries: paths, MainClass: MainClass}, nil
}
