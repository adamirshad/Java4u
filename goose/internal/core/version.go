package core

// Models for Mojang's per-version JSON, rule evaluation, and library resolution.

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const LibrariesBase = "https://libraries.minecraft.net/"

type Artifact struct {
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type LibArtifact struct {
	Path string `json:"path"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type AssetIndexRef struct {
	ID   string `json:"id"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type JavaVersion struct {
	Component    string `json:"component"`
	MajorVersion int    `json:"majorVersion"`
}

type Downloads struct {
	Client Artifact `json:"client"`
}

type VersionJSON struct {
	ID                 string        `json:"id"`
	Kind               string        `json:"type"`
	MainClass          string        `json:"mainClass"`
	AssetIndex         AssetIndexRef `json:"assetIndex"`
	Assets             string        `json:"assets"`
	Downloads          Downloads     `json:"downloads"`
	Libraries          []Library     `json:"libraries"`
	Arguments          *Arguments    `json:"arguments"`
	MinecraftArguments string        `json:"minecraftArguments"`
	JavaVersion        *JavaVersion  `json:"javaVersion"`
	InheritsFrom       string        `json:"inheritsFrom"`
}

func LoadVersionJSON(path string) (*VersionJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v VersionJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if v.InheritsFrom != "" {
		return nil, errors.New("versions that use `inheritsFrom` (mod loaders) are not supported yet")
	}
	return &v, nil
}

type Library struct {
	Name      string            `json:"name"`
	Downloads *LibDownloads     `json:"downloads"`
	Rules     []Rule            `json:"rules"`
	Natives   map[string]string `json:"natives"`
	Extract   *Extract          `json:"extract"`
	URL       string            `json:"url"`
}

type LibDownloads struct {
	Artifact    *LibArtifact           `json:"artifact"`
	Classifiers map[string]LibArtifact `json:"classifiers"`
}

type Extract struct {
	Exclude []string `json:"exclude"`
}

// ---------------------------------------------------------------- rules

type Rule struct {
	Action   string          `json:"action"`
	OS       *OSRule         `json:"os"`
	Features map[string]bool `json:"features"`
}

type OSRule struct {
	Name string `json:"name"`
	Arch string `json:"arch"`
}

type Features map[string]bool

// CurrentOS is the OS name Mojang's rules use.
func CurrentOS() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "osx"
	default:
		return "linux"
	}
}

// CurrentArch is the architecture name Mojang's rules use.
func CurrentArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "arm64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}

func archBits() string {
	return strconv.Itoa(strconv.IntSize)
}

func (r Rule) matches(f Features) bool {
	if r.OS != nil {
		if r.OS.Name != "" && r.OS.Name != CurrentOS() {
			return false
		}
		if r.OS.Arch != "" && r.OS.Arch != CurrentArch() {
			return false
		}
	}
	for key, want := range r.Features {
		if f[key] != want {
			return false
		}
	}
	return true
}

// RulesAllow implements Mojang semantics: no rules = allowed; otherwise start
// disallowed and the last matching rule wins.
func RulesAllow(rules []Rule, f Features) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range rules {
		if r.matches(f) {
			allowed = r.Action == "allow"
		}
	}
	return allowed
}

// ------------------------------------------------------------ arguments

type Arguments struct {
	Game []Arg `json:"game"`
	JVM  []Arg `json:"jvm"`
}

// Arg is either a plain string (no rules, one value) or a conditional entry.
type Arg struct {
	Rules  []Rule
	Values []string
}

func (a *Arg) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*a = Arg{Values: []string{s}}
		return nil
	}
	var obj struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	a.Rules = obj.Rules
	var one string
	if err := json.Unmarshal(obj.Value, &one); err == nil {
		a.Values = []string{one}
		return nil
	}
	return json.Unmarshal(obj.Value, &a.Values)
}

func ExpandArgs(args []Arg, f Features) []string {
	var out []string
	for _, a := range args {
		if RulesAllow(a.Rules, f) {
			out = append(out, a.Values...)
		}
	}
	return out
}

// --------------------------------------------------------------- assets

type AssetIndex struct {
	Objects        map[string]AssetObject `json:"objects"`
	Virtual        bool                   `json:"virtual"`
	MapToResources bool                   `json:"map_to_resources"`
}

type AssetObject struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

func LoadAssetIndex(path string) (*AssetIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx AssetIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &idx, nil
}

// objectPath is where an asset object with this hash lives.
func objectPath(objects, hash string) string {
	return filepath.Join(objects, hash[:2], hash)
}

// ------------------------------------------------------------ libraries

type NativeJar struct {
	Path    string
	Exclude []string
}

type ResolvedLibraries struct {
	Classpath []string
	Downloads []DownloadTask
	Natives   []NativeJar
}

func mavenPath(name string) (string, bool) {
	parts := strings.Split(name, ":")
	if len(parts) < 3 {
		return "", false
	}
	group := strings.ReplaceAll(parts[0], ".", "/")
	artifact, version := parts[1], parts[2]
	classifier := ""
	if len(parts) > 3 {
		classifier = "-" + parts[3]
	}
	return fmt.Sprintf("%s/%s/%s/%s-%s%s.jar", group, artifact, version, artifact, version, classifier), true
}

func ResolveLibraries(v *VersionJSON, libsDir string, f Features) ResolvedLibraries {
	var out ResolvedLibraries

	for _, lib := range v.Libraries {
		if !RulesAllow(lib.Rules, f) {
			continue
		}
		dls := lib.Downloads

		if dls != nil && dls.Artifact != nil {
			a := dls.Artifact
			dest := filepath.Join(libsDir, a.Path)
			out.Downloads = append(out.Downloads, DownloadTask{URL: a.URL, Dest: dest, SHA1: a.SHA1, Size: a.Size})
			out.Classpath = append(out.Classpath, dest)
		} else if lib.Natives == nil && dls == nil {
			// Old maven-style entry without a downloads block.
			if rel, ok := mavenPath(lib.Name); ok {
				base := lib.URL
				if base == "" {
					base = LibrariesBase
				}
				if !strings.HasSuffix(base, "/") {
					base += "/"
				}
				dest := filepath.Join(libsDir, rel)
				out.Downloads = append(out.Downloads, DownloadTask{URL: base + rel, Dest: dest, Size: UnknownSize})
				out.Classpath = append(out.Classpath, dest)
			}
		}

		// Legacy natives (pre-1.19): classifier jars extracted to a natives dir.
		if template, ok := lib.Natives[CurrentOS()]; ok && dls != nil {
			key := strings.ReplaceAll(template, "${arch}", archBits())
			if c, ok := dls.Classifiers[key]; ok {
				dest := filepath.Join(libsDir, c.Path)
				out.Downloads = append(out.Downloads, DownloadTask{URL: c.URL, Dest: dest, SHA1: c.SHA1, Size: c.Size})
				var exclude []string
				if lib.Extract != nil {
					exclude = lib.Extract.Exclude
				}
				out.Natives = append(out.Natives, NativeJar{Path: dest, Exclude: exclude})
			}
		}
	}

	seen := make(map[string]bool, len(out.Classpath))
	cp := out.Classpath[:0]
	for _, p := range out.Classpath {
		if !seen[p] {
			seen[p] = true
			cp = append(cp, p)
		}
	}
	out.Classpath = cp
	return out
}

// isSafeRel reports whether p is a relative path with no ".." parts, so joining it
// to a directory cannot escape that directory.
func isSafeRel(p string) bool {
	if p == "" || filepath.IsAbs(p) || filepath.VolumeName(p) != "" ||
		strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return false
	}
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return false
		}
	}
	return true
}

func ExtractNatives(jars []NativeJar, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, nj := range jars {
		if err := extractJar(nj, dest); err != nil {
			return fmt.Errorf("extracting %s: %w", nj.Path, err)
		}
	}
	return nil
}

func extractJar(nj NativeJar, dest string) error {
	zr, err := zip.OpenReader(nj.Path)
	if err != nil {
		return err
	}
	defer zr.Close()

entries:
	for _, e := range zr.File {
		if e.FileInfo().IsDir() {
			continue
		}
		for _, x := range nj.Exclude {
			if strings.HasPrefix(e.Name, x) {
				continue entries
			}
		}
		if !isSafeRel(e.Name) {
			continue
		}
		outPath := filepath.Join(dest, e.Name)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := copyZipEntry(e, outPath); err != nil {
			return err
		}
	}
	return nil
}

func copyZipEntry(e *zip.File, outPath string) error {
	r, err := e.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(outPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
