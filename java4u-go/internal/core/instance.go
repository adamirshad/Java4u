package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Instance struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	MemoryMB int      `json:"memory_mb"`
	JavaPath string   `json:"java_path,omitempty"`
	JVMArgs  []string `json:"jvm_args"`
	Profile  Profile  `json:"profile"`
}

const instanceFile = "instance.json"

// ValidateInstanceName allows letters, digits, space, '-', '_' and '.' (not leading).
func ValidateInstanceName(name string) error {
	ok := name != "" && len(name) <= 64 && name[0] != '.'
	for _, c := range name {
		if !(c == '-' || c == '_' || c == ' ' || c == '.' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			ok = false
		}
	}
	if !ok {
		return errors.New("instance name may only contain letters, digits, space, '-', '_' and '.'")
	}
	return nil
}

func CreateInstance(dirs Dirs, name, version string) (*Instance, error) {
	if err := ValidateInstanceName(name); err != nil {
		return nil, err
	}
	if exists(filepath.Join(dirs.InstanceDir(name), instanceFile)) {
		return nil, fmt.Errorf("instance '%s' already exists", name)
	}
	inst := &Instance{
		Name:     name,
		Version:  version,
		MemoryMB: RecommendedMemoryMB(),
		JVMArgs:  []string{},
		Profile:  ProfileBalanced,
	}
	if err := inst.Save(dirs); err != nil {
		return nil, err
	}
	return inst, nil
}

func parseInstance(data []byte) (*Instance, error) {
	inst := &Instance{MemoryMB: -1, Profile: ProfileBalanced}
	if err := json.Unmarshal(data, inst); err != nil {
		return nil, err
	}
	if inst.MemoryMB <= 0 {
		inst.MemoryMB = RecommendedMemoryMB()
	}
	if p, err := ParseProfile(string(inst.Profile)); err == nil {
		inst.Profile = p
	} else {
		inst.Profile = ProfileBalanced
	}
	if inst.JVMArgs == nil {
		inst.JVMArgs = []string{}
	}
	return inst, nil
}

func LoadInstance(dirs Dirs, name string) (*Instance, error) {
	if err := ValidateInstanceName(name); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dirs.InstanceDir(name), instanceFile))
	if err != nil {
		return nil, fmt.Errorf("no such instance: %s", name)
	}
	return parseInstance(data)
}

func (i *Instance) Save(dirs Dirs) error {
	if err := ValidateInstanceName(i.Name); err != nil {
		return err
	}
	dir := dirs.InstanceDir(i.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if i.JVMArgs == nil {
		i.JVMArgs = []string{}
	}
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, instanceFile), data, 0o644)
}

// DeleteInstance removes the instance folder, including its worlds and mods.
func DeleteInstance(dirs Dirs, name string) error {
	if err := ValidateInstanceName(name); err != nil {
		return err
	}
	return os.RemoveAll(dirs.InstanceDir(name))
}

func ListInstances(dirs Dirs) ([]*Instance, error) {
	out := []*Instance{}
	entries, err := os.ReadDir(dirs.InstancesDir())
	if err != nil {
		return out, nil
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dirs.InstancesDir(), e.Name(), instanceFile))
		if err != nil {
			continue
		}
		if inst, err := parseInstance(data); err == nil {
			out = append(out, inst)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

// EffectiveJVMArgs is the profile flags followed by the user's own arguments (which win on conflict).
func (i *Instance) EffectiveJVMArgs() []string {
	args := JVMFlags(i.Profile, i.MemoryMB, i.JVMArgs)
	return append(args, i.JVMArgs...)
}

// GameDir is Minecraft's game directory for this instance (saves, mods, options.txt...).
func (i *Instance) GameDir(dirs Dirs) string {
	return filepath.Join(dirs.InstanceDir(i.Name), "minecraft")
}
