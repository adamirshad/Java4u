package core

// Installing Modrinth modpacks (.mrpack). A pack is a zip with modrinth.index.json, which
// lists files to download (with hashes), plus "overrides" folders copied into the game
// directory. Java4U makes a new Chunky instance for each pack.

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Hosts the Modrinth pack format allows files to come from.
var packHosts = map[string]bool{
	"cdn.modrinth.com":          true,
	"github.com":                true,
	"raw.githubusercontent.com": true,
	"gitlab.com":                true,
}

type packIndex struct {
	FormatVersion int    `json:"formatVersion"`
	Game          string `json:"game"`
	Name          string `json:"name"`
	VersionID     string `json:"versionId"`
	Files         []struct {
		Path      string            `json:"path"`
		Hashes    map[string]string `json:"hashes"`
		Env       map[string]string `json:"env"`
		Downloads []string          `json:"downloads"`
		FileSize  int64             `json:"fileSize"`
	} `json:"files"`
	Dependencies map[string]string `json:"dependencies"`
}

// InstallModpack installs the newest Fabric build of a Modrinth modpack (slug or id) that
// runs on a Minecraft version Chunky supports, as a new instance named name (or the pack's
// own name). It returns the new instance.
func InstallModpack(ctx context.Context, c *http.Client, dirs Dirs, project, name string, progress Progress) (*Instance, error) {
	if !projectRef.MatchString(project) {
		return nil, errors.New("invalid Modrinth project")
	}
	m := &modInstaller{ctx: ctx, c: c}

	var pack struct {
		Title string `json:"title"`
	}
	if err := m.get("/project/"+project, &pack); err != nil {
		return nil, fmt.Errorf("%s: %w", project, err)
	}
	var versions []mrVersion
	q := url.Values{"loaders": {`["fabric"]`}}
	if err := m.get("/project/"+project+"/version?"+q.Encode(), &versions); err != nil {
		return nil, err
	}
	var pick *mrVersion
	for i := range versions {
		if len(versions[i].Files) > 0 && packSupported(versions[i].GameVersions) {
			pick = &versions[i]
			break
		}
	}
	if pick == nil {
		return nil, fmt.Errorf("%s has no Fabric version for Minecraft 26.1 or newer yet, which Chunky needs", pack.Title)
	}
	file := pick.Files[0]
	for _, f := range pick.Files {
		if f.Primary {
			file = f
		}
	}
	u, err := url.Parse(file.URL)
	if err != nil || u.Scheme != "https" || u.Host != "cdn.modrinth.com" || !strings.HasSuffix(file.Filename, ".mrpack") {
		return nil, fmt.Errorf("%s's download isn't a Modrinth pack", pack.Title)
	}

	tmp, err := os.MkdirTemp("", "java4u-pack")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	mrpack := filepath.Join(tmp, "pack.mrpack")
	task := DownloadTask{URL: file.URL, Dest: mrpack, SHA1: file.Hashes["sha1"], Size: file.Size}
	if err := DownloadAll(ctx, c, []DownloadTask{task}, "modpack", progress, VerifyHash); err != nil {
		return nil, err
	}
	if name == "" {
		name = pack.Title
	}
	return InstallMrpack(ctx, c, dirs, mrpack, name, pack.Title, progress)
}

func packSupported(gameVersions []string) bool {
	for _, v := range gameVersions {
		if ChunkySupports(v) {
			return true
		}
	}
	return false
}

// InstallMrpack installs a .mrpack file from disk as a new Chunky instance.
func InstallMrpack(ctx context.Context, c *http.Client, dirs Dirs, mrpack, name, packTitle string, progress Progress) (*Instance, error) {
	zr, err := zip.OpenReader(mrpack)
	if err != nil {
		return nil, fmt.Errorf("not a modpack: %w", err)
	}
	defer zr.Close()

	var idx packIndex
	if err := readZipJSON(&zr.Reader, "modrinth.index.json", &idx); err != nil {
		return nil, err
	}
	if idx.FormatVersion != 1 || idx.Game != "minecraft" {
		return nil, errors.New("unsupported modpack format")
	}
	game := idx.Dependencies["minecraft"]
	if !ChunkySupports(game) {
		return nil, fmt.Errorf("this pack is for Minecraft %s; Chunky runs 26.1 and newer", game)
	}
	for _, other := range []string{"forge", "neoforge", "quilt-loader"} {
		if _, ok := idx.Dependencies[other]; ok {
			return nil, fmt.Errorf("this pack needs %s; Chunky runs Fabric packs", other)
		}
	}
	if packTitle == "" {
		packTitle = idx.Name
	}

	inst, err := CreateInstance(dirs, uniqueInstanceName(dirs, name), game)
	if err != nil {
		return nil, err
	}
	inst.Loader = LoaderChunky
	inst.Modpack = packTitle
	if err := inst.Save(dirs); err != nil {
		return nil, err
	}
	fail := func(err error) (*Instance, error) {
		_ = DeleteInstance(dirs, inst.Name) // don't leave a half-installed pack behind
		return nil, err
	}
	gameDir := inst.GameDir(dirs)

	var tasks []DownloadTask
	for _, f := range idx.Files {
		if f.Env["client"] == "unsupported" {
			continue
		}
		if !isSafeRel(f.Path) || strings.Contains(f.Path, ":") {
			return fail(fmt.Errorf("pack lists an unsafe path: %s", f.Path))
		}
		src := ""
		for _, d := range f.Downloads {
			if u, err := url.Parse(d); err == nil && u.Scheme == "https" && packHosts[u.Host] {
				src = d
				break
			}
		}
		if src == "" {
			return fail(fmt.Errorf("%s has no download from an allowed site", f.Path))
		}
		sha := f.Hashes["sha1"]
		if len(sha) != 40 {
			return fail(fmt.Errorf("%s has no SHA-1 hash", f.Path))
		}
		tasks = append(tasks, DownloadTask{URL: src, Dest: filepath.Join(gameDir, filepath.FromSlash(f.Path)), SHA1: sha, Size: f.FileSize})
	}
	if err := DownloadAll(ctx, c, tasks, "pack files", progress, VerifyHash); err != nil {
		return fail(err)
	}

	// Overrides: client-overrides win over overrides.
	for _, prefix := range []string{"overrides/", "client-overrides/"} {
		if err := extractPrefix(&zr.Reader, prefix, gameDir); err != nil {
			return fail(err)
		}
	}
	return inst, nil
}

func readZipJSON(zr *zip.Reader, name string, out any) error {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		return json.NewDecoder(io.LimitReader(r, 16<<20)).Decode(out)
	}
	return fmt.Errorf("%s is missing; this isn't a Modrinth pack", name)
}

// extractPrefix copies every file under prefix in the zip into dir, refusing unsafe paths.
func extractPrefix(zr *zip.Reader, prefix, dir string) error {
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) || f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		if rel == "" {
			continue
		}
		if !isSafeRel(rel) || strings.Contains(rel, ":") {
			return fmt.Errorf("pack has an unsafe file: %s", f.Name)
		}
		dest := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyZipEntry(f, dest); err != nil {
			return err
		}
	}
	return nil
}

// uniqueInstanceName turns a pack title into a valid, unused instance name.
func uniqueInstanceName(dirs Dirs, title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == ' ', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	base := strings.Join(strings.Fields(b.String()), " ")
	base = strings.TrimLeft(base, ".")
	if len(base) > 56 {
		base = strings.TrimSpace(base[:56])
	}
	if base == "" {
		base = "Modpack"
	}
	existing, _ := ListInstances(dirs)
	taken := map[string]bool{}
	for _, i := range existing {
		taken[strings.ToLower(i.Name)] = true
	}
	name := base
	for n := 2; taken[strings.ToLower(name)] || exists(dirs.InstanceDir(name)); n++ {
		name = fmt.Sprintf("%s %d", base, n)
	}
	return name
}
