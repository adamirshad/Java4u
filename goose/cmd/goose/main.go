// Command goose is the command-line front-end of the Goose Minecraft launcher.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/adamirshad/Java4u/goose/internal/core"
)

const usage = `goose - a Minecraft launcher written in Go

Usage:
  goose list [--snapshots | --all]            list Minecraft versions
  goose install <version> [--verify]          download client, libraries, assets ("latest" works)
  goose instance create <name> <version>      create an instance
  goose instance list                         list instances
  goose instance set <name> [--memory MB | --auto-memory] [--profile off|balanced|performance] [--java PATH]
  goose account login                         sign in with Microsoft (needs --client-id the first time)
  goose account list                          list saved accounts
  goose account remove <username>             forget a saved account
  goose launch <instance> [--account NAME] [--offline NAME] [--java PATH] [--dry-run]
  goose version                               print the Goose version

Global flags (anywhere on the line):
  --data-dir DIR     where Goose stores everything (or set GOOSE_HOME)
  --client-id ID     Azure application id for Microsoft sign-in (or set GOOSE_CLIENT_ID)
  -v                 show download retries and other warnings
`

type globals struct {
	dataDir  string
	clientID string
	verbose  bool
}

// newFlags makes a flag set that also understands the global flags.
func newFlags(name string, g *globals) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&g.dataDir, "data-dir", g.dataDir, "")
	fs.StringVar(&g.clientID, "client-id", g.clientID, "")
	fs.BoolVar(&g.verbose, "v", g.verbose, "")
	return fs
}

// parse lets flags and positional arguments appear in any order.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func need(pos []string, n int, what string) error {
	if len(pos) < n {
		return fmt.Errorf("missing %s (see `goose help`)", what)
	}
	if len(pos) > n {
		return fmt.Errorf("unexpected argument %q", pos[n])
	}
	return nil
}

// bar is a one-line progress display on stderr.
type bar struct {
	mu    sync.Mutex
	shown bool
}

func (b *bar) update(stage string, done, total int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const width = 40
	filled := width
	if total > 0 {
		filled = done * width / total
	}
	fmt.Fprintf(os.Stderr, "\r%12s [%s%s] %d/%d  ", stage, strings.Repeat("=", filled), strings.Repeat(" ", width-filled), done, total)
	b.shown = true
}

func (b *bar) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shown {
		fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 80))
		b.shown = false
	}
}

type app struct {
	ctx    context.Context
	g      globals
	dirs   core.Dirs
	client *http.Client
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print(usage)
			return
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	g := globals{dataDir: os.Getenv("GOOSE_HOME"), clientID: os.Getenv("GOOSE_CLIENT_ID")}
	top := newFlags("goose", &g)
	if err := top.Parse(args); err != nil {
		return err
	}
	args = top.Args()
	if len(args) == 0 || args[0] == "help" {
		fmt.Print(usage)
		return nil
	}
	cmd, rest := args[0], args[1:]

	a := &app{ctx: ctx, g: g, client: core.NewClient()}
	switch cmd {
	case "version":
		fmt.Println("goose", core.Version)
		return nil
	case "list":
		return a.list(rest)
	case "install":
		return a.install(rest)
	case "instance":
		return a.instance(rest)
	case "account":
		return a.account(rest)
	case "launch":
		return a.launch(rest)
	}
	return fmt.Errorf("unknown command %q (see `goose help`)", cmd)
}

// setup applies the global flags once the subcommand's flags have been parsed.
func (a *app) setup() error {
	if !a.g.verbose {
		log.SetOutput(io.Discard)
	}
	if a.g.dataDir != "" {
		a.dirs = core.NewDirs(a.g.dataDir)
		return nil
	}
	d, err := core.DefaultDirs()
	a.dirs = d
	return err
}

func (a *app) resolve(id string) (string, error) {
	if !core.IsAlias(id) {
		return id, nil
	}
	m, err := core.FetchManifest(a.ctx, a.client)
	if err != nil {
		return "", err
	}
	return m.ResolveAlias(id), nil
}

func (a *app) list(args []string) error {
	fs := newFlags("list", &a.g)
	snapshots := fs.Bool("snapshots", false, "")
	all := fs.Bool("all", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 0, ""); err != nil {
		return err
	}
	if err := a.setup(); err != nil {
		return err
	}
	m, err := core.FetchManifest(a.ctx, a.client)
	if err != nil {
		return err
	}
	fmt.Printf("latest release: %s   latest snapshot: %s\n\n", m.Latest.Release, m.Latest.Snapshot)
	for _, v := range m.Versions {
		if *all || v.Kind == "release" || (*snapshots && v.Kind == "snapshot") {
			fmt.Printf("%-24s %s\n", v.ID, v.Kind)
		}
	}
	return nil
}

func (a *app) install(args []string) error {
	fs := newFlags("install", &a.g)
	verify := fs.Bool("verify", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "version"); err != nil {
		return err
	}
	if err := a.setup(); err != nil {
		return err
	}
	id, err := a.resolve(pos[0])
	if err != nil {
		return err
	}
	b := &bar{}
	_, err = core.InstallVersion(a.ctx, a.client, a.dirs, id, *verify, b.update)
	b.clear()
	if err != nil {
		return err
	}
	fmt.Println("installed", id)
	return nil
}

func (a *app) instance(args []string) error {
	if len(args) == 0 {
		return errors.New("missing instance command: create, list or set")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "create":
		pos, err := parse(newFlags("create", &a.g), args)
		if err != nil {
			return err
		}
		if err := need(pos, 2, "name and version"); err != nil {
			return err
		}
		if err := a.setup(); err != nil {
			return err
		}
		id, err := a.resolve(pos[1])
		if err != nil {
			return err
		}
		inst, err := core.CreateInstance(a.dirs, pos[0], id)
		if err != nil {
			return err
		}
		fmt.Printf("created instance '%s' (%s)\n", inst.Name, inst.Version)
		return nil

	case "list":
		pos, err := parse(newFlags("list", &a.g), args)
		if err != nil {
			return err
		}
		if err := need(pos, 0, ""); err != nil {
			return err
		}
		if err := a.setup(); err != nil {
			return err
		}
		all, err := core.ListInstances(a.dirs)
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Println("no instances yet; try: goose instance create main latest")
		}
		for _, i := range all {
			fmt.Printf("%-24s %-10s %d MB, %s profile\n", i.Name, i.Version, i.MemoryMB, i.Profile)
		}
		return nil

	case "set":
		fs := newFlags("set", &a.g)
		memory := fs.Int("memory", 0, "")
		autoMemory := fs.Bool("auto-memory", false, "")
		profile := fs.String("profile", "", "")
		java := fs.String("java", "", "")
		pos, err := parse(fs, args)
		if err != nil {
			return err
		}
		if err := need(pos, 1, "instance name"); err != nil {
			return err
		}
		if *memory != 0 && *autoMemory {
			return errors.New("use either --memory or --auto-memory, not both")
		}
		if err := a.setup(); err != nil {
			return err
		}
		inst, err := core.LoadInstance(a.dirs, pos[0])
		if err != nil {
			return err
		}
		if *autoMemory {
			inst.MemoryMB = core.RecommendedMemoryMB()
		}
		if *memory != 0 {
			if *memory < 512 {
				return errors.New("memory must be at least 512 MB")
			}
			inst.MemoryMB = *memory
		}
		if *profile != "" {
			p, err := core.ParseProfile(*profile)
			if err != nil {
				return err
			}
			inst.Profile = p
		}
		if *java != "" {
			inst.JavaPath = *java
		}
		if err := inst.Save(a.dirs); err != nil {
			return err
		}
		fmt.Printf("saved: %d MB, %s profile\n", inst.MemoryMB, inst.Profile)
		return nil
	}
	return fmt.Errorf("unknown instance command %q", sub)
}

func (a *app) account(args []string) error {
	if len(args) == 0 {
		return errors.New("missing account command: login, list or remove")
	}
	sub, args := args[0], args[1:]
	pos, err := parse(newFlags(sub, &a.g), args)
	if err != nil {
		return err
	}
	if err := a.setup(); err != nil {
		return err
	}
	switch sub {
	case "login":
		if err := need(pos, 0, ""); err != nil {
			return err
		}
		id := a.g.clientID
		if id == "" {
			store, err := core.LoadAccounts(a.dirs)
			if err != nil {
				return err
			}
			id = store.ClientID
		}
		if id == "" {
			return errors.New("pass your Azure application id with --client-id or GOOSE_CLIENT_ID (see README)")
		}
		acct, err := core.Login(a.ctx, a.client, a.dirs, id, func(dc *core.DeviceCode) {
			fmt.Printf("To sign in, open %s and enter the code: %s\n", dc.VerificationURI, dc.UserCode)
			fmt.Println("Waiting for you to finish...")
		})
		if err != nil {
			return err
		}
		fmt.Printf("Signed in as %s (%s)\n", acct.Username, acct.UUID)
		return nil

	case "list":
		if err := need(pos, 0, ""); err != nil {
			return err
		}
		store, err := core.LoadAccounts(a.dirs)
		if err != nil {
			return err
		}
		if len(store.Accounts) == 0 {
			fmt.Println("no accounts; run: goose account login")
		}
		for _, acc := range store.Accounts {
			fmt.Printf("%-18s %s\n", acc.Username, acc.UUID)
		}
		return nil

	case "remove":
		if err := need(pos, 1, "username"); err != nil {
			return err
		}
		store, err := core.LoadAccounts(a.dirs)
		if err != nil {
			return err
		}
		if !store.Remove(pos[0]) {
			return fmt.Errorf("no saved account named %s", pos[0])
		}
		if err := store.Save(a.dirs); err != nil {
			return err
		}
		fmt.Println("removed", pos[0])
		return nil
	}
	return fmt.Errorf("unknown account command %q", sub)
}

func (a *app) launch(args []string) error {
	fs := newFlags("launch", &a.g)
	account := fs.String("account", "", "")
	offline := fs.String("offline", "", "")
	java := fs.String("java", "", "")
	dryRun := fs.Bool("dry-run", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if err := need(pos, 1, "instance name"); err != nil {
		return err
	}
	if err := a.setup(); err != nil {
		return err
	}
	inst, err := core.LoadInstance(a.dirs, pos[0])
	if err != nil {
		return err
	}

	// Sort out the account first, so a missing login fails before any big download.
	var acct core.Account
	if *offline != "" {
		fmt.Fprintln(os.Stderr, "warning: offline mode is for development and testing only")
		acct, err = core.OfflineAccount(*offline)
	} else {
		acct, err = core.AccountForLaunch(a.ctx, a.client, a.dirs, *account, a.g.clientID)
	}
	if err != nil {
		return err
	}

	b := &bar{}
	v, err := core.InstallVersion(a.ctx, a.client, a.dirs, inst.Version, false, b.update)
	if err != nil {
		b.clear()
		return err
	}
	javaOverride := *java
	if javaOverride == "" {
		javaOverride = inst.JavaPath
	}
	javaPath, err := core.ResolveJava(a.ctx, a.client, a.dirs, v, javaOverride, b.update)
	b.clear()
	if err != nil {
		return err
	}

	cmd, err := core.BuildCommand(a.dirs, v, core.LaunchOptions{
		Java:         javaPath,
		GameDir:      inst.GameDir(a.dirs),
		Account:      acct,
		MemoryMB:     inst.MemoryMB,
		ExtraJVMArgs: inst.EffectiveJVMArgs(),
	})
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Println(core.DescribeCommand(cmd, acct.AccessToken))
		return nil
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Java: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("Minecraft exited with %w", err)
	}
	return nil
}
