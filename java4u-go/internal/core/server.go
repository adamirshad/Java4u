package core

// Dedicated servers: install Mojang's server, keep each server in its own folder, and
// build the command that runs it (optionally through Chunky).

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const serverFile = "java4u-server.json"

// Server is a dedicated server. Its folder holds the world, server.properties and mods.
type Server struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	MemoryMB int      `json:"memory_mb"`
	JavaPath string   `json:"java_path,omitempty"`
	JVMArgs  []string `json:"jvm_args"`
	Profile  Profile  `json:"profile"`
	Loader   string   `json:"loader,omitempty"`
}

func (d Dirs) ServersDir() string           { return filepath.Join(d.Root, "servers") }
func (d Dirs) ServerDir(name string) string { return filepath.Join(d.ServersDir(), name) }

// Dir is the server's own folder (its game directory).
func (s *Server) Dir(dirs Dirs) string { return dirs.ServerDir(s.Name) }

// ModsDir is where the server's mods live.
func (s *Server) ModsDir(dirs Dirs) string { return filepath.Join(s.Dir(dirs), "mods") }

// EffectiveJVMArgs is the profile flags followed by the user's own arguments.
func (s *Server) EffectiveJVMArgs() []string {
	return append(JVMFlags(s.Profile, s.MemoryMB, s.JVMArgs), s.JVMArgs...)
}

func CreateServer(dirs Dirs, name, version string) (*Server, error) {
	if err := ValidateInstanceName(name); err != nil {
		return nil, err
	}
	if exists(filepath.Join(dirs.ServerDir(name), serverFile)) {
		return nil, fmt.Errorf("server '%s' already exists", name)
	}
	s := &Server{Name: name, Version: version, MemoryMB: 2048, JVMArgs: []string{}, Profile: ProfileBalanced}
	if err := s.Save(dirs); err != nil {
		return nil, err
	}
	return s, nil
}

func LoadServer(dirs Dirs, name string) (*Server, error) {
	if err := ValidateInstanceName(name); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dirs.ServerDir(name), serverFile))
	if err != nil {
		return nil, fmt.Errorf("no such server: %s", name)
	}
	s := &Server{MemoryMB: 2048, Profile: ProfileBalanced}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	if p, err := ParseProfile(string(s.Profile)); err == nil {
		s.Profile = p
	}
	if s.JVMArgs == nil {
		s.JVMArgs = []string{}
	}
	return s, nil
}

func (s *Server) Save(dirs Dirs) error {
	if err := ValidateInstanceName(s.Name); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir(dirs), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir(dirs), serverFile), data, 0o644)
}

func ListServers(dirs Dirs) []*Server {
	out := []*Server{}
	entries, _ := os.ReadDir(dirs.ServersDir())
	for _, e := range entries {
		if s, err := LoadServer(dirs, e.Name()); err == nil {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// DeleteServer removes the server folder, including its world.
func DeleteServer(dirs Dirs, name string) error {
	if err := ValidateInstanceName(name); err != nil {
		return err
	}
	return os.RemoveAll(dirs.ServerDir(name))
}

// ---------------------------------------------------------------- EULA

// EULAAccepted reports whether eula.txt in dir says eula=true.
func EULAAccepted(dir string) bool {
	f, err := os.Open(filepath.Join(dir, "eula.txt"))
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "eula" {
			return strings.EqualFold(strings.TrimSpace(v), "true")
		}
	}
	return false
}

// AcceptEULA records that the server owner agreed to Minecraft's EULA. Only call this
// after the person running the server has said they agree.
func AcceptEULA(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	text := "# Accepted in Java4U by the server owner. See https://aka.ms/MinecraftEULA\neula=true\n"
	return os.WriteFile(filepath.Join(dir, "eula.txt"), []byte(text), 0o644)
}

// ---------------------------------------------------------------- installing the server

// ServerFiles is an unpacked server: the real server jar and its libraries.
type ServerFiles struct {
	Jar       string
	Libraries []string
	MainClass string
}

type bundled struct{ sha256, path string }

// InstallServer downloads Mojang's server jar for a version and unpacks it. Since 1.18 the
// download is a "bundler" that carries the real server jar and its libraries inside it;
// Java4U takes them out so the server (and Chunky) can load them directly.
func InstallServer(ctx context.Context, c *http.Client, dirs Dirs, id string, progress Progress) (*VersionJSON, *ServerFiles, error) {
	v, err := ensureVersionJSON(ctx, c, dirs, id, progress)
	if err != nil {
		return nil, nil, err
	}
	if v.Downloads.Server == nil {
		return nil, nil, fmt.Errorf("Mojang publishes no server for Minecraft %s", id)
	}
	art := v.Downloads.Server
	bundler := filepath.Join(dirs.VersionDir(id), id+"-server-bundler.jar")
	task := DownloadTask{URL: art.URL, Dest: bundler, SHA1: art.SHA1, Size: art.Size}
	if err := DownloadAll(ctx, c, []DownloadTask{task}, "server", progress, VerifySize); err != nil {
		return nil, nil, err
	}
	files, err := unpackServer(dirs, id, bundler, art.SHA1, progress)
	if err != nil {
		return nil, nil, fmt.Errorf("unpacking the server: %w", err)
	}
	return v, files, nil
}

func unpackServer(dirs Dirs, id, bundler, bundlerSHA string, progress Progress) (*ServerFiles, error) {
	zr, err := zip.OpenReader(bundler)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	readText := func(name string) (string, error) {
		f := entries[name]
		if f == nil {
			return "", fmt.Errorf("%s is missing; this server jar isn't a bundler", name)
		}
		r, err := f.Open()
		if err != nil {
			return "", err
		}
		defer r.Close()
		b, err := io.ReadAll(io.LimitReader(r, 1<<20))
		return string(b), err
	}
	parseList := func(name string) ([]bundled, error) {
		text, err := readText(name)
		if err != nil {
			return nil, err
		}
		var out []bundled
		for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
			parts := strings.Split(strings.TrimSpace(line), "\t")
			if len(parts) != 3 || !isSafeRel(parts[2]) || len(parts[0]) != 64 {
				return nil, fmt.Errorf("bad line in %s: %q", name, line)
			}
			out = append(out, bundled{sha256: parts[0], path: parts[2]})
		}
		return out, nil
	}

	mainClass, err := readText("META-INF/main-class")
	if err != nil {
		return nil, err
	}
	versions, err := parseList("META-INF/versions.list")
	if err != nil {
		return nil, err
	}
	libs, err := parseList("META-INF/libraries.list")
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, errors.New("the bundler lists no server jar")
	}

	serverDir := filepath.Join(dirs.VersionDir(id), "server")
	marker := filepath.Join(serverDir, okMarker)
	verified := false
	if b, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(b)) == bundlerSHA {
		verified = true // unpacked from this exact bundler before
	}

	extract := func(entry string, b bundled, dest string) error {
		if verified && exists(dest) {
			return nil
		}
		if sum, err := sha256File(dest); err == nil && sum == b.sha256 {
			return nil
		}
		f := entries[entry]
		if f == nil {
			return fmt.Errorf("%s is missing from the bundler", entry)
		}
		return copyVerified(f, dest, b.sha256)
	}

	files := &ServerFiles{MainClass: strings.TrimSpace(mainClass)}
	total := len(versions) + len(libs)
	done := 0
	report(progress, "server files", 0, total)
	for _, b := range versions {
		dest := filepath.Join(serverDir, filepath.FromSlash(b.path))
		if err := extract("META-INF/versions/"+b.path, b, dest); err != nil {
			return nil, err
		}
		files.Jar = dest
		done++
		report(progress, "server files", done, total)
	}
	for _, b := range libs {
		dest := filepath.Join(dirs.LibrariesDir(), filepath.FromSlash(b.path))
		if err := extract("META-INF/libraries/"+b.path, b, dest); err != nil {
			return nil, err
		}
		files.Libraries = append(files.Libraries, dest)
		done++
		report(progress, "server files", done, total)
	}
	if err := os.WriteFile(marker, []byte(bundlerSHA), 0o644); err != nil {
		return nil, err
	}
	return files, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyVerified writes a zip entry to dest through a temp file, checking its SHA-256.
func copyVerified(f *zip.File, dest, want string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	tmp := dest + ".part"
	w, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(w, h), r)
	closeErr := w.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmp)
		return errors.Join(copyErr, closeErr)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		os.Remove(tmp)
		return &HashMismatchError{Path: dest, Expected: want, Actual: got}
	}
	return os.Rename(tmp, dest)
}

// ---------------------------------------------------------------- running

// BuildServerCommand returns the command that runs a server without its own window ("nogui").
func BuildServerCommand(dirs Dirs, v *VersionJSON, s *Server, files *ServerFiles, java string, ch *ChunkyLaunch) (*exec.Cmd, error) {
	dir := s.Dir(dirs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	gameCP := append([]string{files.Jar}, files.Libraries...)
	sep := string(os.PathListSeparator)

	args := []string{fmt.Sprintf("-Xmx%dM", s.MemoryMB)}
	args = append(args, s.EffectiveJVMArgs()...)
	if ch == nil {
		args = append(args, "-cp", strings.Join(gameCP, sep), files.MainClass)
	} else {
		listFile := filepath.Join(dir, ".chunky", "classpath.txt")
		if err := os.MkdirAll(filepath.Dir(listFile), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(listFile, []byte(strings.Join(gameCP, "\n")+"\n"), 0o644); err != nil {
			return nil, err
		}
		args = append(args,
			"-cp", strings.Join(append([]string{ch.Jar}, ch.Libraries...), sep),
			"-Dchunky.side=server",
			"-Dchunky.mainClass="+files.MainClass,
			"-Dchunky.gameVersion="+v.ID,
			"-Dchunky.gameDir="+dir,
			"-Dchunky.gameJar="+files.Jar,
			"-Dchunky.classpathFile="+listFile,
			ch.MainClass)
	}
	args = append(args, "nogui")
	cmd := exec.Command(java, args...)
	cmd.Dir = dir
	return cmd, nil
}
