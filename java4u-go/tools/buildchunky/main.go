// Command buildchunky compiles Chunky (../../chunky) into internal/chunky/dist/chunky.jar,
// which Java4U embeds. It needs a JDK 21 or newer: javac from JAVA_HOME or PATH, or any
// java that includes the jdk.compiler module (Mojang's own runtime does). Run it from the
// java4u-go folder:
//
//	go run ./tools/buildchunky
package main

import (
	"archive/zip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/adamirshad/Java4u/java4u-go/internal/chunky"
	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

func main() {
	src := flag.String("src", "chunky", "Chunky source folder (with src/ and resources/)")
	out := flag.String("out", filepath.Join("internal", "chunky", "dist", "chunky.jar"), "jar to write")
	java := flag.String("java", "", "javac, or a java that has the jdk.compiler module (default: JAVA_HOME, then PATH)")
	flag.Parse()
	if err := build(*src, *out, *java); err != nil {
		fmt.Fprintln(os.Stderr, "buildchunky:", err)
		os.Exit(1)
	}
}

func build(src, out, javaFlag string) error {
	compiler, err := findCompiler(javaFlag)
	if err != nil {
		return err
	}

	// Chunky compiles against the same pinned libraries it uses at runtime.
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	libsDir := filepath.Join(cache, "java4u-build", "libraries")
	var tasks []core.DownloadTask
	var classpath []string
	for _, l := range chunky.Libraries {
		dest := filepath.Join(libsDir, filepath.FromSlash(l.Path))
		tasks = append(tasks, core.DownloadTask{URL: l.URL, Dest: dest, SHA1: l.SHA1, Size: l.Size})
		classpath = append(classpath, dest)
	}
	fmt.Println("checking Chunky's libraries...")
	if err := core.DownloadAll(context.Background(), core.NewClient(), tasks, "libraries", nil, core.VerifyHash); err != nil {
		return err
	}

	var sources []string
	err = filepath.WalkDir(filepath.Join(src, "src"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".java") {
			sources = append(sources, p)
		}
		return err
	})
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return fmt.Errorf("no Java sources under %s", filepath.Join(src, "src"))
	}

	tmp, err := os.MkdirTemp("", "chunky-build")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	classes := filepath.Join(tmp, "classes")
	argfile := filepath.Join(tmp, "sources.txt")
	var quoted []string
	for _, s := range sources {
		abs, _ := filepath.Abs(s)
		quoted = append(quoted, `"`+strings.ReplaceAll(abs, `\`, `\\`)+`"`)
	}
	if err := os.WriteFile(argfile, []byte(strings.Join(quoted, "\n")), 0o644); err != nil {
		return err
	}

	args := append(append([]string{}, compiler[1:]...),
		"--release", "21", "-encoding", "UTF-8", "-nowarn",
		"-cp", strings.Join(classpath, string(os.PathListSeparator)),
		"-d", classes, "@"+argfile)
	fmt.Printf("compiling %d Java files...\n", len(sources))
	cmd := exec.Command(compiler[0], args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("javac failed: %w", err)
	}

	if err := writeJar(out, classes, filepath.Join(src, "resources")); err != nil {
		return err
	}
	st, _ := os.Stat(out)
	fmt.Printf("wrote %s (%d KB)\n", out, st.Size()/1024)
	return nil
}

// findCompiler returns the command (and leading arguments) that runs javac.
func findCompiler(flagValue string) ([]string, error) {
	exe := func(name string) string {
		if runtime.GOOS == "windows" {
			return name + ".exe"
		}
		return name
	}
	candidates := []string{}
	if flagValue != "" {
		candidates = append(candidates, flagValue)
	}
	if home := os.Getenv("JAVA_HOME"); home != "" {
		candidates = append(candidates, filepath.Join(home, "bin", exe("javac")), filepath.Join(home, "bin", exe("java")))
	}
	if p, err := exec.LookPath("javac"); err == nil {
		candidates = append(candidates, p)
	}
	if p, err := exec.LookPath("java"); err == nil {
		candidates = append(candidates, p)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err != nil {
			if _, err := exec.LookPath(c); err != nil {
				continue
			}
		}
		base := strings.TrimSuffix(strings.ToLower(filepath.Base(c)), ".exe")
		if base == "javac" {
			return []string{c}, nil
		}
		// A plain java works if it carries the compiler module.
		if err := exec.Command(c, "--describe-module", "jdk.compiler").Run(); err == nil {
			return []string{c, "-m", "jdk.compiler/com.sun.tools.javac.Main"}, nil
		}
	}
	return nil, errors.New("no Java compiler found; install a JDK 21+ or pass -java <path to javac or java>")
}

// writeJar packs classes and resources in a stable order with fixed timestamps, so the
// same sources always produce the same jar.
func writeJar(out, classes, resources string) error {
	files := map[string]string{} // entry name -> file on disk
	for _, root := range []string{classes, resources} {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			files[filepath.ToSlash(rel)] = p
			return nil
		})
		if err != nil {
			return err
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	manifest := "Manifest-Version: 1.0\r\nMain-Class: " + chunky.MainClass +
		"\r\nImplementation-Title: Chunky\r\nImplementation-Version: " + chunky.Version + "\r\n\r\n"
	if err := addEntry(zw, "META-INF/MANIFEST.MF", strings.NewReader(manifest), stamp); err != nil {
		return err
	}
	for _, n := range names {
		if n == "META-INF/MANIFEST.MF" {
			continue
		}
		in, err := os.Open(files[n])
		if err != nil {
			return err
		}
		err = addEntry(zw, n, in, stamp)
		in.Close()
		if err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func addEntry(zw *zip.Writer, name string, r io.Reader, stamp time.Time) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: stamp})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}
