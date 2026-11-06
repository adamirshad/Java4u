package core

// Worlds and screenshots inside an instance's game directory.

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type World struct {
	Folder     string `json:"folder"`
	LastPlayed int64  `json:"last_played"` // Unix seconds
	SizeBytes  int64  `json:"size_bytes"`
	HasIcon    bool   `json:"has_icon"`
}

type Screenshot struct {
	File      string `json:"file"`
	Taken     int64  `json:"taken"` // Unix seconds
	SizeBytes int64  `json:"size_bytes"`
}

func (i *Instance) SavesDir(dirs Dirs) string { return filepath.Join(i.GameDir(dirs), "saves") }
func (i *Instance) ScreenshotsDir(dirs Dirs) string {
	return filepath.Join(i.GameDir(dirs), "screenshots")
}

// ListWorlds lists the instance's worlds, most recently played first.
func ListWorlds(dirs Dirs, inst *Instance) ([]World, error) {
	root := inst.SavesDir(dirs)
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return []World{}, nil
	}
	if err != nil {
		return nil, err
	}
	worlds := []World{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		level, err := os.Stat(filepath.Join(dir, "level.dat"))
		if err != nil {
			continue // not a world
		}
		w := World{Folder: e.Name(), LastPlayed: level.ModTime().Unix(), HasIcon: exists(filepath.Join(dir, "icon.png"))}
		filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if info, err := d.Info(); err == nil {
					w.SizeBytes += info.Size()
				}
			}
			return nil
		})
		worlds = append(worlds, w)
	}
	sort.Slice(worlds, func(a, b int) bool { return worlds[a].LastPlayed > worlds[b].LastPlayed })
	return worlds, nil
}

// ListScreenshots lists the instance's screenshots, newest first.
func ListScreenshots(dirs Dirs, inst *Instance) ([]Screenshot, error) {
	entries, err := os.ReadDir(inst.ScreenshotsDir(dirs))
	if errors.Is(err, fs.ErrNotExist) {
		return []Screenshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	shots := []Screenshot{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		shots = append(shots, Screenshot{File: e.Name(), Taken: info.ModTime().Unix(), SizeBytes: info.Size()})
	}
	sort.Slice(shots, func(a, b int) bool { return shots[a].Taken > shots[b].Taken })
	return shots, nil
}

// childPath joins a single path element under root, refusing anything that would escape it.
func childPath(root, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\:`) || filepath.Base(name) != name {
		return "", errors.New("invalid name")
	}
	return filepath.Join(root, name), nil
}

// ScreenshotPath returns the full path of one screenshot.
func ScreenshotPath(dirs Dirs, inst *Instance, file string) (string, error) {
	if !strings.EqualFold(filepath.Ext(file), ".png") {
		return "", errors.New("not a screenshot")
	}
	return childPath(inst.ScreenshotsDir(dirs), file)
}

// WorldIconPath returns the full path of a world's icon.png.
func WorldIconPath(dirs Dirs, inst *Instance, folder string) (string, error) {
	dir, err := childPath(inst.SavesDir(dirs), folder)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "icon.png"), nil
}

// OpenFolder shows a folder in the system file manager, creating it first if needed.
func OpenFolder(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
