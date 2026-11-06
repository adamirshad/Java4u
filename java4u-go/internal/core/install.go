package core

// Download everything a version needs: client jar, libraries, asset index, assets.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

const (
	resourcesBase = "https://resources.download.minecraft.net"
	okMarker      = ".java4u-ok"
)

// InstallVersion installs (or quickly re-checks) a concrete version id such as "1.21.1".
//
// After one fully verified install a marker is written, and later runs only compare file
// sizes (milliseconds instead of re-hashing thousands of assets). fullVerify forces SHA1
// checks again, for repairing corrupted files.
func InstallVersion(ctx context.Context, c *http.Client, dirs Dirs, id string, fullVerify bool, progress Progress) (*VersionJSON, error) {
	marker := filepath.Join(dirs.VersionDir(id), okMarker)
	verify := VerifySize
	if fullVerify || !exists(marker) {
		verify = VerifyHash
	}

	v, err := ensureVersionJSON(ctx, c, dirs, id, progress)
	if err != nil {
		return nil, err
	}

	cl := v.Downloads.Client
	jar := DownloadTask{URL: cl.URL, Dest: dirs.VersionJar(id), SHA1: cl.SHA1, Size: cl.Size}
	if err := DownloadAll(ctx, c, []DownloadTask{jar}, "client", progress, verify); err != nil {
		return nil, err
	}

	resolved := ResolveLibraries(v, dirs.LibrariesDir(), Features{})
	if err := DownloadAll(ctx, c, resolved.Downloads, "libraries", progress, verify); err != nil {
		return nil, err
	}

	idx := v.AssetIndex
	idxTask := DownloadTask{URL: idx.URL, Dest: dirs.AssetIndex(idx.ID), SHA1: idx.SHA1, Size: idx.Size}
	if err := DownloadAll(ctx, c, []DownloadTask{idxTask}, "asset index", progress, verify); err != nil {
		return nil, err
	}

	index, err := LoadAssetIndex(dirs.AssetIndex(idx.ID))
	if err != nil {
		return nil, err
	}
	objects := dirs.AssetObjectsDir()
	tasks := make([]DownloadTask, 0, len(index.Objects))
	for _, o := range index.Objects {
		if len(o.Hash) < 2 {
			continue
		}
		tasks = append(tasks, DownloadTask{
			URL:  fmt.Sprintf("%s/%s/%s", resourcesBase, o.Hash[:2], o.Hash),
			Dest: objectPath(objects, o.Hash),
			SHA1: o.Hash,
			Size: o.Size,
		})
	}
	if err := DownloadAll(ctx, c, tasks, "assets", progress, verify); err != nil {
		return nil, err
	}

	if err := os.WriteFile(marker, []byte(Version), 0o644); err != nil {
		return nil, err
	}
	return v, nil
}

// ensureVersionJSON downloads a version's JSON from Mojang's manifest the first time.
func ensureVersionJSON(ctx context.Context, c *http.Client, dirs Dirs, id string, progress Progress) (*VersionJSON, error) {
	jsonPath := dirs.VersionJSON(id)
	if !exists(jsonPath) {
		m, err := FetchManifest(ctx, c)
		if err != nil {
			return nil, err
		}
		entry := m.Find(id)
		if entry == nil {
			return nil, fmt.Errorf("unknown Minecraft version: %s", id)
		}
		task := DownloadTask{URL: entry.URL, Dest: jsonPath, SHA1: entry.SHA1, Size: UnknownSize}
		if err := DownloadAll(ctx, c, []DownloadTask{task}, "version", progress, VerifyHash); err != nil {
			return nil, err
		}
	}
	return LoadVersionJSON(jsonPath)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
