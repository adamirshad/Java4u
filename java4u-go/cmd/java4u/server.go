package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adamirshad/Java4u/java4u-go/internal/chunky"
	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

func (a *app) server(args []string) error {
	if len(args) == 0 {
		return errors.New("missing server command: create, list, set, mods or run")
	}
	sub, args := args[0], args[1:]
	if sub == "mods" {
		return a.serverMods(args)
	}
	fs := newFlags(sub, &a.g)
	memory := fs.Int("memory", 0, "")
	loader := fs.String("loader", "", "")
	acceptEULA := fs.Bool("accept-eula", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if err := a.setup(); err != nil {
		return err
	}

	switch sub {
	case "create":
		if err := need(pos, 2, "name and version"); err != nil {
			return err
		}
		id, err := a.resolve(pos[1])
		if err != nil {
			return err
		}
		s, err := core.CreateServer(a.dirs, pos[0], id)
		if err != nil {
			return err
		}
		fmt.Printf("created server '%s' (Minecraft %s) in %s\n", s.Name, s.Version, s.Dir(a.dirs))
		return nil

	case "list":
		for _, s := range core.ListServers(a.dirs) {
			fmt.Printf("%-24s %-10s %d MB, %s\n", s.Name, s.Version, s.MemoryMB, loaderLabel(s.Loader))
		}
		return nil

	case "set":
		if err := need(pos, 1, "server name"); err != nil {
			return err
		}
		s, err := core.LoadServer(a.dirs, pos[0])
		if err != nil {
			return err
		}
		if *memory != 0 {
			if *memory < 512 {
				return errors.New("memory must be at least 512 MB")
			}
			s.MemoryMB = *memory
		}
		if err := setLoader(&s.Loader, *loader, s.Version); err != nil {
			return err
		}
		if err := s.Save(a.dirs); err != nil {
			return err
		}
		fmt.Printf("saved: %d MB, %s\n", s.MemoryMB, loaderLabel(s.Loader))
		return nil

	case "run":
		if err := need(pos, 1, "server name"); err != nil {
			return err
		}
		return a.runServer(pos[0], *acceptEULA)
	}
	return fmt.Errorf("unknown server command %q", sub)
}

func (a *app) modpack(args []string) error {
	const usage = "usage: java4u modpack install <project> [--name NAME] | import <file.mrpack> [--name NAME] | export <instance> <file.mrpack>"
	if len(args) == 0 {
		return errors.New(usage)
	}
	sub := args[0]
	fs := newFlags(sub, &a.g)
	name := fs.String("name", "", "")
	pos, err := parse(fs, args[1:])
	if err != nil {
		return err
	}
	switch sub {
	case "install", "import":
		what := "Modrinth modpack (slug or id)"
		if sub == "import" {
			what = ".mrpack file"
		}
		if err := need(pos, 1, what); err != nil {
			return err
		}
		if err := a.setup(); err != nil {
			return err
		}
		b := &bar{}
		var inst *core.Instance
		if sub == "install" {
			inst, err = core.InstallModpack(a.ctx, a.client, a.dirs, pos[0], *name, b.update)
		} else {
			n := *name
			if n == "" {
				n = strings.TrimSuffix(filepath.Base(pos[0]), filepath.Ext(pos[0]))
			}
			inst, err = core.InstallMrpack(a.ctx, a.client, a.dirs, pos[0], n, "", b.update)
		}
		b.clear()
		if err != nil {
			return err
		}
		mods, _ := core.ListMods(inst.ModsDir(a.dirs))
		fmt.Printf("installed %s as '%s' (Minecraft %s, Chunky, %d mods). Play it with: java4u launch \"%s\"\n",
			inst.Modpack, inst.Name, inst.Version, len(mods), inst.Name)
		return nil
	case "export":
		if err := need(pos, 2, "instance and output file"); err != nil {
			return err
		}
		if err := a.setup(); err != nil {
			return err
		}
		inst, err := core.LoadInstance(a.dirs, pos[0])
		if err != nil {
			return err
		}
		linked, bundled, err := core.ExportMrpack(a.ctx, a.client, a.dirs, inst, pos[1])
		if err != nil {
			return err
		}
		fmt.Printf("wrote %s: %d mods linked to Modrinth, %d bundled in the pack\n", pos[1], linked, bundled)
		return nil
	}
	return errors.New(usage)
}
func loaderLabel(loader string) string {
	if loader == core.LoaderChunky {
		return "Chunky"
	}
	return "vanilla"
}

func setLoader(field *string, value, version string) error {
	switch value {
	case "":
	case "vanilla":
		*field = ""
	case core.LoaderChunky:
		if !chunky.SupportsVersion(version) {
			return fmt.Errorf("Chunky supports Minecraft 26.1 and newer, not %s", version)
		}
		*field = core.LoaderChunky
	default:
		return fmt.Errorf("unknown loader %q (use chunky or vanilla)", value)
	}
	return nil
}

func (a *app) serverMods(args []string) error {
	if len(args) == 0 {
		return errors.New("missing command: list, install, enable, disable or remove")
	}
	sub, args := args[0], args[1:]
	pos, err := parse(newFlags(sub, &a.g), args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return errors.New("missing server name")
	}
	if err := a.setup(); err != nil {
		return err
	}
	s, err := core.LoadServer(a.dirs, pos[0])
	if err != nil {
		return err
	}
	dir := s.ModsDir(a.dirs)
	switch sub {
	case "list":
		mods, err := core.ListMods(dir)
		if err != nil {
			return err
		}
		fmt.Printf("%s (Minecraft %s, %s): %d mod(s)\n", s.Name, s.Version, loaderLabel(s.Loader), len(mods))
		for _, m := range mods {
			state := ""
			if !m.Enabled {
				state = "  [disabled]"
			}
			fmt.Printf("  %-28s %-16s %s%s\n", m.Name, m.Version, m.File, state)
		}
		return nil
	case "install":
		if err := need(pos, 2, "server and Modrinth project"); err != nil {
			return err
		}
		if s.Loader != core.LoaderChunky {
			if err := setLoader(&s.Loader, core.LoaderChunky, s.Version); err != nil {
				return fmt.Errorf("mods need Chunky: %w", err)
			}
			if err := s.Save(a.dirs); err != nil {
				return err
			}
			fmt.Printf("switched %s to Chunky so it can run mods\n", s.Name)
		}
		files, err := core.InstallMod(a.ctx, a.client, dir, pos[1], s.Version, core.SideServer)
		for _, f := range files {
			fmt.Printf("installed %s %s (%s)\n", f.File, f.Version, f.Reason)
		}
		return err
	case "enable", "disable":
		if err := need(pos, 2, "server and mod file"); err != nil {
			return err
		}
		return core.SetModEnabled(dir, pos[1], sub == "enable")
	case "remove":
		if err := need(pos, 2, "server and mod file"); err != nil {
			return err
		}
		return core.RemoveMod(dir, pos[1])
	}
	return fmt.Errorf("unknown server mods command %q", sub)
}

func (a *app) runServer(name string, acceptEULA bool) error {
	s, err := core.LoadServer(a.dirs, name)
	if err != nil {
		return err
	}
	dir := s.Dir(a.dirs)
	if !core.EULAAccepted(dir) {
		if !acceptEULA {
			return errors.New("a Minecraft server needs you to accept Mojang's EULA (https://aka.ms/MinecraftEULA).\n" +
				"If you agree, run this again with --accept-eula")
		}
		if err := core.AcceptEULA(dir); err != nil {
			return err
		}
	}

	b := &bar{}
	v, files, err := core.InstallServer(a.ctx, a.client, a.dirs, s.Version, b.update)
	if err != nil {
		b.clear()
		return err
	}
	java, err := core.ResolveJava(a.ctx, a.client, a.dirs, v, s.JavaPath, b.update)
	if err != nil {
		b.clear()
		return err
	}
	var ch *core.ChunkyLaunch
	if s.Loader == core.LoaderChunky {
		if ch, err = chunky.Prepare(a.ctx, a.client, a.dirs, b.update); err != nil {
			b.clear()
			return err
		}
	}
	b.clear()

	cmd, err := core.BuildServerCommand(a.dirs, v, s, files, java, ch)
	if err != nil {
		return err
	}
	fmt.Printf("starting %s (Minecraft %s, %s). Type \"stop\" to shut it down.\n", s.Name, s.Version, loaderLabel(s.Loader))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Java: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("the server exited with %w", err)
	}
	return nil
}

func (a *app) update(args []string) error {
	fs := newFlags("update", &a.g)
	check := fs.Bool("check", false, "")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if err := a.setup(); err != nil {
		return err
	}
	rel, err := core.LatestRelease(a.ctx, a.client, core.ReleasesURL)
	if err != nil {
		return err
	}
	if !core.NewerVersion(rel.Version(), core.Version) {
		fmt.Printf("Java4U %s is the newest version.\n", core.Version)
		return nil
	}
	fmt.Printf("Java4U %s is available (you have %s): %s\n", rel.Version(), core.Version, rel.Page)
	if *check {
		return nil
	}
	b := &bar{}
	err = core.ApplyUpdate(a.ctx, a.client, rel, "", b.update)
	b.clear()
	if err != nil {
		return err
	}
	fmt.Printf("updated to %s\n", rel.Version())
	return nil
}
