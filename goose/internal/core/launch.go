package core

// Turn an installed version + account + settings into a runnable `java` command.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type LaunchOptions struct {
	Java         string
	GameDir      string
	Account      Account
	MemoryMB     int
	ExtraJVMArgs []string
}

// syncAssets copies hashed asset objects to human-readable paths (pre-1.7 "virtual"
// assets and pre-1.6 "map_to_resources").
func syncAssets(index *AssetIndex, objects, target string) error {
	for name, obj := range index.Objects {
		if !isSafeRel(name) || len(obj.Hash) < 2 {
			continue
		}
		dst := filepath.Join(target, name)
		if exists(dst) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyFile(objectPath(objects, obj.Hash), dst); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func BuildCommand(dirs Dirs, v *VersionJSON, opts LaunchOptions) (*exec.Cmd, error) {
	features := Features{}

	// Classpath + natives
	resolved := ResolveLibraries(v, dirs.LibrariesDir(), features)
	cp := append(append([]string{}, resolved.Classpath...), dirs.VersionJar(v.ID))
	sep := string(os.PathListSeparator)
	classpath := strings.Join(cp, sep)
	nativesDir := dirs.NativesDir(v.ID)
	if err := ExtractNatives(resolved.Natives, nativesDir); err != nil {
		return nil, err
	}

	// Assets
	index, err := LoadAssetIndex(dirs.AssetIndex(v.AssetIndex.ID))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.GameDir, 0o755); err != nil {
		return nil, err
	}
	assetsRoot := dirs.AssetsDir()
	gameAssets := assetsRoot
	if index.Virtual {
		target := filepath.Join(assetsRoot, "virtual", v.AssetIndex.ID)
		if err := syncAssets(index, dirs.AssetObjectsDir(), target); err != nil {
			return nil, err
		}
		gameAssets = target
	}
	if index.MapToResources {
		if err := syncAssets(index, dirs.AssetObjectsDir(), filepath.Join(opts.GameDir, "resources")); err != nil {
			return nil, err
		}
	}

	assetsName := v.Assets
	if assetsName == "" {
		assetsName = v.AssetIndex.ID
	}
	acct := opts.Account
	vars := map[string]string{
		"auth_player_name":    acct.Username,
		"auth_uuid":           acct.UUID,
		"auth_access_token":   acct.AccessToken,
		"auth_session":        fmt.Sprintf("token:%s:%s", acct.AccessToken, acct.UUID),
		"user_type":           acct.UserType,
		"user_properties":     "{}",
		"clientid":            "",
		"auth_xuid":           "",
		"version_name":        v.ID,
		"version_type":        v.Kind,
		"game_directory":      opts.GameDir,
		"assets_root":         assetsRoot,
		"game_assets":         gameAssets,
		"assets_index_name":   assetsName,
		"natives_directory":   nativesDir,
		"library_directory":   dirs.LibrariesDir(),
		"classpath_separator": sep,
		"classpath":           classpath,
		"launcher_name":       "goose",
		"launcher_version":    Version,
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, val := range vars {
		pairs = append(pairs, "${"+k+"}", val)
	}
	sub := strings.NewReplacer(pairs...)

	// JVM + game argument templates (modern `arguments` or legacy string)
	var jvmTpl, gameTpl []string
	if v.Arguments != nil {
		jvmTpl = ExpandArgs(v.Arguments.JVM, features)
		gameTpl = ExpandArgs(v.Arguments.Game, features)
	} else {
		jvmTpl = []string{"-Djava.library.path=${natives_directory}", "-cp", "${classpath}"}
		gameTpl = strings.Fields(v.MinecraftArguments)
	}

	args := []string{fmt.Sprintf("-Xmx%dM", opts.MemoryMB)}
	args = append(args, opts.ExtraJVMArgs...)
	for _, a := range jvmTpl {
		args = append(args, sub.Replace(a))
	}
	args = append(args, v.MainClass)
	for _, a := range gameTpl {
		args = append(args, sub.Replace(a))
	}

	cmd := exec.Command(opts.Java, args...)
	cmd.Dir = opts.GameDir
	return cmd, nil
}

// DescribeCommand renders a command for display with the access token hidden.
func DescribeCommand(cmd *exec.Cmd, secret string) string {
	parts := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		if secret != "" && secret != "0" {
			a = strings.ReplaceAll(a, secret, "<access token>")
		}
		if a == "" || strings.ContainsAny(a, " \t\"") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}
