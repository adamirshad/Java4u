package core

import (
	"context"
	"net/http"
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
