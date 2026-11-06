package core

// Managing the mod jars in an instance's mods folder.

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	disabledSuffix = ".disabled"
	modIndexFile   = ".java4u-mods.json"
)

// InstalledMod describes one jar in a mods folder.
type InstalledMod struct {
	File        string `json:"file"` // file name without ".disabled"
	Enabled     bool   `json:"enabled"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	ProjectID   string `json:"project_id,omitempty"` // Modrinth project, if installed from there
}

// modIndex remembers which Modrinth project each installed file came from.
type modIndex struct {
	Files map[string]indexEntry `json:"files"`
}

type indexEntry struct {
	ProjectID string `json:"project_id"`
	VersionID string `json:"version_id"`
}

func loadIndex(modsDir string) *modIndex {
	idx := &modIndex{Files: map[string]indexEntry{}}
	data, err := os.ReadFile(filepath.Join(modsDir, modIndexFile))
	if err == nil {
		_ = json.Unmarshal(data, idx)
	}
	if idx.Files == nil {
		idx.Files = map[string]indexEntry{}
	}
	return idx
}

func (x *modIndex) save(modsDir string) error {
	data, err := json.MarshalIndent(x, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(modsDir, modIndexFile), data, 0o644)
}

func (x *modIndex) projectFile(projectID string) (string, bool) {
	for f, e := range x.Files {
		if e.ProjectID == projectID {
			return f, true
		}
	}
	return "", false
}

// validModFile rejects names that could point outside the mods folder.
func validModFile(name string) bool {
	return name != "" && !strings.ContainsAny(name, `/\`) && !strings.HasPrefix(name, ".") &&
		strings.HasSuffix(name, ".jar") && filepath.Base(name) == name
}

// ListMods returns the mods in a folder, enabled and disabled, sorted by name.
func ListMods(modsDir string) ([]InstalledMod, error) {
	out := []InstalledMod{}
	entries, err := os.ReadDir(modsDir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	idx := loadIndex(modsDir)
	for _, e := range entries {
		name := e.Name()
		enabled := true
		if strings.HasSuffix(name, disabledSuffix) {
			enabled = false
			name = strings.TrimSuffix(name, disabledSuffix)
		}
		if e.IsDir() || !validModFile(name) {
			continue
		}
		m := InstalledMod{File: name, Enabled: enabled, ID: strings.TrimSuffix(name, ".jar"), Name: strings.TrimSuffix(name, ".jar")}
		m.ProjectID = idx.Files[name].ProjectID
		if meta, err := readModMeta(filepath.Join(modsDir, e.Name())); err == nil {
			m.ID, m.Version, m.Description = meta.ID, meta.Version, meta.Description
			if meta.Name != "" {
				m.Name = meta.Name
			}
		}
		out = append(out, m)
	}
	sort.Slice(out, func(a, b int) bool { return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name) })
	return out, nil
}

type modMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// readModMeta reads chunky.mod.json or fabric.mod.json from a mod jar.
func readModMeta(jar string) (*modMeta, error) {
	zr, err := zip.OpenReader(jar)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, name := range []string{"chunky.mod.json", "fabric.mod.json"} {
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(r, 1<<20))
			r.Close()
			if err != nil {
				return nil, err
			}
			var m modMeta
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			return &m, nil
		}
	}
	return nil, errors.New("not a Chunky or Fabric mod")
}

// SetModEnabled turns a mod on or off by renaming it to or from "<file>.disabled".
func SetModEnabled(modsDir, file string, enabled bool) error {
	if !validModFile(file) {
		return errors.New("invalid mod file name")
	}
	on := filepath.Join(modsDir, file)
	off := on + disabledSuffix
	if enabled {
		if exists(on) {
			return nil
		}
		return os.Rename(off, on)
	}
	if exists(off) {
		return nil
	}
	return os.Rename(on, off)
}

// RemoveMod deletes a mod (enabled or disabled) and forgets where it came from.
func RemoveMod(modsDir, file string) error {
	if !validModFile(file) {
		return errors.New("invalid mod file name")
	}
	removed := false
	for _, p := range []string{filepath.Join(modsDir, file), filepath.Join(modsDir, file+disabledSuffix)} {
		if err := os.Remove(p); err == nil {
			removed = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !removed {
		return fmt.Errorf("%s isn't installed", file)
	}
	idx := loadIndex(modsDir)
	delete(idx.Files, file)
	return idx.save(modsDir)
}
