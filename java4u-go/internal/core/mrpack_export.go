package core

// Exporting an instance as a Modrinth modpack (.mrpack) that other launchers can import.
// Enabled mods that Modrinth knows (matched by file hash) are listed as downloads; any
// other mod, and the instance's config, resource packs and shader packs, go in "overrides".

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const fabricMetaLoaders = "https://meta.fabricmc.net/v2/versions/loader"

// Folders of the game directory copied into a pack's overrides.
var exportFolders = []string{"config", "resourcepacks", "shaderpacks", "kubejs"}

type exportSources struct{ modrinth, fabricMeta string }

// ExportMrpack writes inst as a .mrpack file at dest. It returns how many mods were
// linked to Modrinth and how many were bundled in the pack.
func ExportMrpack(ctx context.Context, c *http.Client, dirs Dirs, inst *Instance, dest string) (linked, bundled int, err error) {
	return exportMrpack(ctx, c, dirs, inst, dest, exportSources{modrinthAPI, fabricMetaLoaders})
}

type packFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

func exportMrpack(ctx context.Context, c *http.Client, dirs Dirs, inst *Instance, dest string, src exportSources) (int, int, error) {
	gameDir := inst.GameDir(dirs)
	modsDir := inst.ModsDir(dirs)
	mods, err := ListMods(modsDir)
	if err != nil {
		return 0, 0, err
	}

	// Hash every enabled mod and ask Modrinth which ones it hosts.
	type local struct {
		file       string
		sha1, s512 string
		size       int64
	}
	var jars []local
	for _, m := range mods {
		if !m.Enabled {
			continue
		}
		p := filepath.Join(modsDir, m.File)
		data, err := os.ReadFile(p)
		if err != nil {
			return 0, 0, err
		}
		h1, h5 := sha1.Sum(data), sha512.Sum512(data)
		jars = append(jars, local{m.File, hex.EncodeToString(h1[:]), hex.EncodeToString(h5[:]), int64(len(data))})
	}
	known := map[string]string{} // sha1 -> download URL
	if len(jars) > 0 {
		hashes := make([]string, len(jars))
		for i, j := range jars {
			hashes[i] = j.sha1
		}
		known, err = modrinthURLsByHash(ctx, c, src.modrinth, hashes)
		if err != nil {
			known = map[string]string{} // offline: bundle everything instead
		}
	}

	title := inst.Modpack
	if title == "" {
		title = inst.Name
	}
	idx := map[string]any{
		"formatVersion": 1,
		"game":          "minecraft",
		"versionId":     "1.0.0",
		"name":          title,
		"summary":       "Exported from Java4U",
	}
	deps := map[string]string{"minecraft": inst.Version}
	if v := latestFabricLoader(ctx, c, src.fabricMeta); v != "" {
		deps["fabric-loader"] = v // so other launchers know to use Fabric
	}
	idx["dependencies"] = deps

	files := []packFile{}
	var bundle []string // paths (relative to gameDir) to put in overrides
	for _, j := range jars {
		if u, ok := known[j.sha1]; ok {
			files = append(files, packFile{
				Path:      "mods/" + j.file,
				Hashes:    map[string]string{"sha1": j.sha1, "sha512": j.s512},
				Downloads: []string{u},
				FileSize:  j.size,
			})
		} else {
			bundle = append(bundle, filepath.Join("mods", j.file))
		}
	}
	idx["files"] = files
	bundledMods := len(bundle)

	for _, folder := range exportFolders {
		root := filepath.Join(gameDir, folder)
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(gameDir, p)
			bundle = append(bundle, rel)
			return nil
		})
	}
	if exists(filepath.Join(gameDir, "options.txt")) {
		bundle = append(bundle, "options.txt")
	}

	// Write to a temp file first so a failed export never leaves half a pack.
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, 0, err
	}
	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return 0, 0, err
	}
	zw := zip.NewWriter(out)
	writeErr := func() error {
		w, err := zw.Create("modrinth.index.json")
		if err != nil {
			return err
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(idx); err != nil {
			return err
		}
		for _, rel := range bundle {
			if err := addZipFile(zw, filepath.Join(gameDir, rel), "overrides/"+filepath.ToSlash(rel)); err != nil {
				return err
			}
		}
		return zw.Close()
	}()
	if cerr := out.Close(); writeErr == nil {
		writeErr = cerr
	}
	if writeErr != nil {
		os.Remove(tmp)
		return 0, 0, writeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return 0, 0, err
	}
	return len(files), bundledMods, nil
}

func addZipFile(zw *zip.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// modrinthURLsByHash looks up many files on Modrinth at once by SHA-1 and returns the
// download address of each one it hosts.
func modrinthURLsByHash(ctx context.Context, c *http.Client, api string, hashes []string) (map[string]string, error) {
	body, _ := json.Marshal(map[string]any{"hashes": hashes, "algorithm": "sha1"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"/version_files", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("Modrinth: %s", resp.Status)
	}
	var found map[string]mrVersion
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&found); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for hash, v := range found {
		for _, f := range v.Files {
			if strings.EqualFold(f.Hashes["sha1"], hash) && strings.HasPrefix(f.URL, "https://cdn.modrinth.com/") {
				out[strings.ToLower(hash)] = f.URL
			}
		}
	}
	return out, nil
}

// latestFabricLoader returns the newest stable Fabric Loader version, or "" if unknown.
func latestFabricLoader(ctx context.Context, c *http.Client, api string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return ""
	}
	resp, err := c.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var loaders []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&loaders) != nil {
		return ""
	}
	for _, l := range loaders {
		if l.Stable {
			return l.Version
		}
	}
	return ""
}
