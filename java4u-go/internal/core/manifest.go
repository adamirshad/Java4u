package core

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

const ManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type Manifest struct {
	Latest   Latest         `json:"latest"`
	Versions []VersionEntry `json:"versions"`
}

type Latest struct {
	Release  string `json:"release"`
	Snapshot string `json:"snapshot"`
}

type VersionEntry struct {
	ID          string `json:"id"`
	Kind        string `json:"type"`
	URL         string `json:"url"`
	SHA1        string `json:"sha1"`
	ReleaseTime string `json:"releaseTime"`
}

func FetchManifest(ctx context.Context, c *http.Client) (*Manifest, error) {
	var m Manifest
	if err := getJSON(ctx, c, ManifestURL, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) Find(id string) *VersionEntry {
	for i := range m.Versions {
		if m.Versions[i].ID == id {
			return &m.Versions[i]
		}
	}
	return nil
}

// ResolveAlias turns "latest"/"release"/"snapshot" into a concrete version id.
func (m *Manifest) ResolveAlias(id string) string {
	switch id {
	case "latest", "release":
		return m.Latest.Release
	case "snapshot":
		return m.Latest.Snapshot
	}
	return id
}

// IsAlias reports whether id is one of the names ResolveAlias understands.
func IsAlias(id string) bool {
	return id == "latest" || id == "release" || id == "snapshot"
}

// ManifestCacheFile is where the last downloaded version list is kept, so the app can
// show it before (or without) a network connection.
func (d Dirs) ManifestCacheFile() string {
	return filepath.Join(d.Root, "cache", "version_manifest_v2.json")
}

// LoadCachedManifest reads the version list saved by SaveCachedManifest.
func LoadCachedManifest(dirs Dirs) (*Manifest, error) {
	data, err := os.ReadFile(dirs.ManifestCacheFile())
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// SaveCachedManifest keeps a copy of the version list for next time.
func SaveCachedManifest(dirs Dirs, m *Manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writePrivate(dirs.ManifestCacheFile(), data)
}
