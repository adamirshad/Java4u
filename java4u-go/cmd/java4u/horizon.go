package main

import (
	"fmt"

	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

// horizon installs or updates Horizon's launcher, then opens it unless --no-open is given.
func (a *app) horizon(args []string) error {
	fs := newFlags("horizon", &a.g)
	noOpen := fs.Bool("no-open", false, "")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if err := a.setup(); err != nil {
		return err
	}
	b := &bar{}
	info, err := core.EnsureHorizon(a.ctx, a.client, a.dirs, core.HorizonReleasesURL, b.update, func(msg string) {
		b.clear()
		fmt.Println(msg)
	})
	b.clear()
	if err != nil {
		return err
	}
	if *noOpen {
		fmt.Printf("Horizon launcher %s is ready.\n", info.Version)
		return nil
	}
	if err := core.OpenHorizon(a.dirs); err != nil {
		return fmt.Errorf("opening Horizon: %w", err)
	}
	fmt.Printf("Opened Horizon launcher %s. Sign in there to play.\n", info.Version)
	return nil
}
