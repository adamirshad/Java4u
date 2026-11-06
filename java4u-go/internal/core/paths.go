// Package core is everything a Minecraft launcher needs, with no UI attached.
package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Version is Java4U's own version, sent as the user agent and to the game as launcher_version.
// Release builds set it with -ldflags "-X github.com/adamirshad/Java4u/java4u-go/internal/core.Version=1.2.3".
var Version = "0.1.0-dev"

// Dirs holds all on-disk locations used by the launcher.
type Dirs struct {
	Root string
}

func NewDirs(root string) Dirs { return Dirs{Root: root} }

// DefaultDirs returns $JAVA4U_HOME if set, otherwise the platform data directory:
// %APPDATA%\Java4U, ~/Library/Application Support/Java4U or ~/.local/share/java4u.
func DefaultDirs() (Dirs, error) {
	if h := os.Getenv("JAVA4U_HOME"); h != "" {
		return NewDirs(h), nil
	}
	switch runtime.GOOS {
	case "windows", "darwin":
		base, err := os.UserConfigDir()
		if err != nil {
			return Dirs{}, errors.New("could not determine a data directory")
		}
		return NewDirs(filepath.Join(base, "Java4U")), nil
	default:
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return NewDirs(filepath.Join(x, "java4u")), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return Dirs{}, errors.New("could not determine a data directory")
		}
		return NewDirs(filepath.Join(home, ".local", "share", "java4u")), nil
	}
}

func (d Dirs) VersionsDir() string              { return filepath.Join(d.Root, "versions") }
func (d Dirs) VersionDir(id string) string      { return filepath.Join(d.VersionsDir(), id) }
func (d Dirs) VersionJSON(id string) string     { return filepath.Join(d.VersionDir(id), id+".json") }
func (d Dirs) VersionJar(id string) string      { return filepath.Join(d.VersionDir(id), id+".jar") }
func (d Dirs) LibrariesDir() string             { return filepath.Join(d.Root, "libraries") }
func (d Dirs) AssetsDir() string                { return filepath.Join(d.Root, "assets") }
func (d Dirs) AssetIndexesDir() string          { return filepath.Join(d.AssetsDir(), "indexes") }
func (d Dirs) AssetIndex(id string) string      { return filepath.Join(d.AssetIndexesDir(), id+".json") }
func (d Dirs) AssetObjectsDir() string          { return filepath.Join(d.AssetsDir(), "objects") }
func (d Dirs) NativesDir(version string) string { return filepath.Join(d.Root, "natives", version) }
func (d Dirs) AccountsFile() string             { return filepath.Join(d.Root, "accounts.json") }
func (d Dirs) RuntimesDir() string              { return filepath.Join(d.Root, "runtimes") }
func (d Dirs) WallpapersDir() string            { return filepath.Join(d.Root, "wallpapers") }
func (d Dirs) SettingsFile() string             { return filepath.Join(d.Root, "settings.json") }
func (d Dirs) InstancesDir() string             { return filepath.Join(d.Root, "instances") }
func (d Dirs) InstanceDir(name string) string   { return filepath.Join(d.InstancesDir(), name) }
