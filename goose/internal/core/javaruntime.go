package core

// Download and manage the Java runtimes Mojang publishes for each Minecraft version.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const runtimeIndexURL = "https://launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

type runtimeIndex map[string]map[string][]struct {
	Manifest Artifact `json:"manifest"`
}

type runtimeManifest struct {
	Files map[string]struct {
		Kind      string `json:"type"`
		Downloads *struct {
			Raw *Artifact `json:"raw"`
		} `json:"downloads"`
		Executable bool   `json:"executable"`
		Target     string `json:"target"`
	} `json:"files"`
}

func platformKey() (string, bool) {
	keys := map[string]string{
		"linux/amd64":   "linux",
		"linux/386":     "linux-i386",
		"darwin/amd64":  "mac-os",
		"darwin/arm64":  "mac-os-arm64",
		"windows/amd64": "windows-x64",
		"windows/arm64": "windows-arm64",
		"windows/386":   "windows-x86",
	}
	k, ok := keys[runtime.GOOS+"/"+runtime.GOARCH]
	return k, ok
}

func javaBinary(root string) (string, bool) {
	exe := javaExe()
	for _, p := range []string{
		filepath.Join(root, "bin", exe),
		filepath.Join(root, "jre.bundle", "Contents", "Home", "bin", exe),
		filepath.Join(root, "Contents", "Home", "bin", exe),
	} {
		if exists(p) {
			return p, true
		}
	}
	return "", false
}

// EnsureRuntime makes sure the named runtime component (e.g. "java-runtime-delta") is
// installed and returns the path to its java executable. Once installed it does no network I/O.
func EnsureRuntime(ctx context.Context, c *http.Client, dirs Dirs, component string, progress Progress) (string, error) {
	platform, ok := platformKey()
	if !ok {
		return "", fmt.Errorf("Mojang publishes no Java runtime for this platform; install Java yourself")
	}
	if !isSafeRel(component) {
		return "", fmt.Errorf("invalid runtime component name: %s", component)
	}
	root := filepath.Join(dirs.RuntimesDir(), component, platform)
	marker := filepath.Join(root, okMarker)

	if exists(marker) {
		if java, ok := javaBinary(root); ok {
			return java, nil
		}
	}

	var index runtimeIndex
	if err := getJSON(ctx, c, runtimeIndexURL, &index); err != nil {
		return "", err
	}
	entries := index[platform][component]
	if len(entries) == 0 {
		return "", fmt.Errorf("Mojang has no runtime '%s' for %s", component, platform)
	}
	entry := entries[0]

	data, err := getBytes(ctx, c, entry.Manifest.URL)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum(data)
	if actual := hex.EncodeToString(sum[:]); !strings.EqualFold(actual, entry.Manifest.SHA1) {
		return "", &HashMismatchError{Path: "runtime manifest", Expected: entry.Manifest.SHA1, Actual: actual}
	}
	var manifest runtimeManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}

	var tasks []DownloadTask
	var executables []string
	type link struct{ path, target string }
	var links []link
	for path, file := range manifest.Files {
		if !isSafeRel(path) {
			return "", fmt.Errorf("unsafe path in runtime manifest: %s", path)
		}
		dest := filepath.Join(root, path)
		switch file.Kind {
		case "directory":
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return "", err
			}
		case "file":
			if file.Downloads != nil && file.Downloads.Raw != nil {
				raw := file.Downloads.Raw
				tasks = append(tasks, DownloadTask{URL: raw.URL, Dest: dest, SHA1: raw.SHA1, Size: raw.Size})
				if file.Executable {
					executables = append(executables, dest)
				}
			}
		case "link":
			if file.Target != "" {
				links = append(links, link{dest, file.Target})
			}
		}
	}

	if err := DownloadAll(ctx, c, tasks, "java runtime", progress, VerifyHash); err != nil {
		return "", err
	}

	if runtime.GOOS != "windows" {
		for _, p := range executables {
			if err := os.Chmod(p, 0o755); err != nil {
				return "", err
			}
		}
		for _, l := range links {
			if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
				return "", err
			}
			os.Remove(l.path)
			if err := os.Symlink(l.target, l.path); err != nil {
				return "", err
			}
		}
	}

	java, ok := javaBinary(root)
	if !ok {
		return "", fmt.Errorf("downloaded runtime has no java executable")
	}
	if err := os.WriteFile(marker, []byte(entry.Manifest.SHA1), 0o644); err != nil {
		return "", err
	}
	return java, nil
}

// ResolveJava picks the Java to run a version with. Order: explicit override, Mojang's
// runtime for that version (downloaded on first use), then a suitable system Java.
func ResolveJava(ctx context.Context, c *http.Client, dirs Dirs, v *VersionJSON, override string, progress Progress) (string, error) {
	component, major := "jre-legacy", 8
	if v.JavaVersion != nil {
		component, major = v.JavaVersion.Component, v.JavaVersion.MajorVersion
	}

	if override != "" {
		if err := CheckJava(override, major); err != nil {
			return "", err
		}
		return override, nil
	}

	java, err := EnsureRuntime(ctx, c, dirs, component, progress)
	if err == nil {
		return java, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	log.Printf("could not set up Mojang's Java runtime: %v", err)
	system := FindJava()
	if CheckJava(system, major) != nil {
		return "", fmt.Errorf("could not set up Java %d (%v) and no suitable system Java was found", major, err)
	}
	return system, nil
}
