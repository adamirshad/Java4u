package core

// Installing mods from Modrinth. Chunky runs Fabric mods, so Java4U asks Modrinth for the
// Fabric build of a mod that matches the instance's Minecraft version.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
)

const modrinthAPI = "https://api.modrinth.com/v2"

var projectRef = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type mrVersion struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"`
	GameVersions  []string `json:"game_versions"`
	Dependencies  []struct {
		VersionID      string `json:"version_id"`
		ProjectID      string `json:"project_id"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
	Files []struct {
		Hashes   map[string]string `json:"hashes"`
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Primary  bool              `json:"primary"`
		Size     int64             `json:"size"`
	} `json:"files"`
}

// InstalledFile is one jar written by InstallMod.
type InstalledFile struct {
	File      string `json:"file"`
	ProjectID string `json:"project_id"`
	Version   string `json:"version"`
	Reason    string `json:"reason"` // "requested" or "dependency"
}

// Sides a mod can be installed for.
const (
	SideClient = "client"
	SideServer = "server"
)

// InstallMod installs the newest Fabric build of a Modrinth project (by slug or id) for
// gameVersion into modsDir, plus every required dependency that isn't there yet. side is
// SideClient or SideServer; mods that don't run on that side are refused (or, for
// dependencies, skipped).
func InstallMod(ctx context.Context, c *http.Client, modsDir, project, gameVersion, side string) ([]InstalledFile, error) {
	if !projectRef.MatchString(project) {
		return nil, errors.New("invalid Modrinth project")
	}
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return nil, err
	}
	idx := loadIndex(modsDir)
	inst := &modInstaller{ctx: ctx, c: c, dir: modsDir, game: gameVersion, side: side, idx: idx, seen: map[string]bool{}}
	err := inst.install(project, "", "requested")
	if saveErr := idx.save(modsDir); err == nil {
		err = saveErr
	}
	return inst.done, err
}

type modInstaller struct {
	ctx  context.Context
	c    *http.Client
	dir  string
	game string
	side string
	idx  *modIndex
	seen map[string]bool
	done []InstalledFile
}

// runsOnSide asks Modrinth whether a project works on this installer's side.
func (m *modInstaller) runsOnSide(projectID string) (bool, string, error) {
	if m.side == "" {
		return true, "", nil
	}
	var p struct {
		Title      string `json:"title"`
		ClientSide string `json:"client_side"`
		ServerSide string `json:"server_side"`
	}
	if err := m.get("/project/"+projectID, &p); err != nil {
		return false, "", err
	}
	support := p.ClientSide
	if m.side == SideServer {
		support = p.ServerSide
	}
	return support != "unsupported", p.Title, nil
}

func (m *modInstaller) install(project, versionID, reason string) error {
	if m.seen[project+"|"+versionID] {
		return nil
	}
	m.seen[project+"|"+versionID] = true

	var v *mrVersion
	var err error
	if versionID != "" {
		v, err = m.version(versionID)
	} else {
		v, err = m.latest(project)
	}
	if err != nil {
		return err
	}
	// A dependency that's already installed (from any version) is left alone.
	if reason == "dependency" {
		if _, ok := m.idx.projectFile(v.ProjectID); ok {
			return nil
		}
	}
	ok, title, err := m.runsOnSide(v.ProjectID)
	if err != nil {
		return err
	}
	if !ok {
		if reason == "dependency" {
			return nil // not needed on this side
		}
		if m.side == SideServer {
			return fmt.Errorf("%s only runs on the client, so it can't go on a server", title)
		}
		return fmt.Errorf("%s only runs on servers", title)
	}

	file := v.Files[0]
	for _, f := range v.Files {
		if f.Primary {
			file = f
			break
		}
	}
	u, err := url.Parse(file.URL)
	if err != nil || u.Scheme != "https" || u.Host != "cdn.modrinth.com" {
		return fmt.Errorf("unexpected download address for %s", v.Name)
	}
	name := filepath.Base(file.Filename)
	if !validModFile(name) {
		return fmt.Errorf("unexpected file name %q from Modrinth", file.Filename)
	}
	sha := file.Hashes["sha1"]
	if len(sha) != 40 {
		return fmt.Errorf("Modrinth gave no SHA-1 for %s", name)
	}

	// Replace an older file from the same project (an update), if there is one.
	if old, ok := m.idx.projectFile(v.ProjectID); ok && old != name {
		_ = os.Remove(filepath.Join(m.dir, old))
		_ = os.Remove(filepath.Join(m.dir, old+disabledSuffix))
		delete(m.idx.Files, old)
	}
	task := DownloadTask{URL: file.URL, Dest: filepath.Join(m.dir, name), SHA1: sha, Size: file.Size}
	if err := DownloadAll(m.ctx, m.c, []DownloadTask{task}, name, nil, VerifyHash); err != nil {
		return fmt.Errorf("downloading %s: %w", name, err)
	}
	m.idx.Files[name] = indexEntry{ProjectID: v.ProjectID, VersionID: v.ID}
	m.done = append(m.done, InstalledFile{File: name, ProjectID: v.ProjectID, Version: v.VersionNumber, Reason: reason})

	for _, d := range v.Dependencies {
		if d.DependencyType != "required" || (d.ProjectID == "" && d.VersionID == "") {
			continue
		}
		dep := d.ProjectID
		if err := m.install(dep, d.VersionID, "dependency"); err != nil {
			return fmt.Errorf("%s needs another mod that couldn't be installed: %w", v.Name, err)
		}
	}
	return nil
}

func (m *modInstaller) get(path string, out any) error {
	req, err := http.NewRequestWithContext(m.ctx, http.MethodGet, modrinthAPI+path, nil)
	if err != nil {
		return err
	}
	resp, err := m.c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errors.New("not found on Modrinth")
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Modrinth: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (m *modInstaller) version(id string) (*mrVersion, error) {
	if !projectRef.MatchString(id) {
		return nil, errors.New("invalid Modrinth version")
	}
	var v mrVersion
	if err := m.get("/version/"+id, &v); err != nil {
		return nil, err
	}
	if len(v.Files) == 0 {
		return nil, fmt.Errorf("%s has no files", v.Name)
	}
	return &v, nil
}

// latest picks the newest release (or, failing that, the newest beta/alpha) Fabric build
// for this Minecraft version.
func (m *modInstaller) latest(project string) (*mrVersion, error) {
	if !projectRef.MatchString(project) {
		return nil, errors.New("invalid Modrinth project")
	}
	q := url.Values{
		"loaders":       {`["fabric"]`},
		"game_versions": {`["` + m.game + `"]`},
	}
	var list []mrVersion
	if err := m.get("/project/"+project+"/version?"+q.Encode(), &list); err != nil {
		return nil, fmt.Errorf("%s: %w", project, err)
	}
	var pick *mrVersion
	for i := range list {
		if len(list[i].Files) == 0 {
			continue
		}
		if list[i].VersionType == "release" {
			pick = &list[i]
			break
		}
		if pick == nil {
			pick = &list[i]
		}
	}
	if pick == nil {
		return nil, fmt.Errorf("%s has no Fabric build for Minecraft %s yet", project, m.game)
	}
	return pick, nil
}
